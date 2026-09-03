package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/dmitriy/gophkeeper/internal/client/model"
	"github.com/spf13/cobra"
)

// editOptions carries the editable fields of the edit command. Only
// flags the user actually provided (tracked in provided, see
// markProvided) are applied; everything else keeps its current
// value.
type editOptions struct {
	label    string
	metadata string

	username string
	password string
	text     string
	file     string

	number string
	holder string
	expiry string
	cvv    string

	// provided records which flags were set on the command line.
	provided map[string]bool
}

// markProvided fills the provided set from the command's flag set:
// each named flag contributes its name when it was set explicitly.
func (o *editOptions) markProvided(cmd *cobra.Command, names ...string) {
	o.provided = make(map[string]bool, len(names))
	for _, name := range names {
		if cmd.Flags().Changed(name) {
			o.provided[name] = true
		}
	}
}

// was reports whether the named flag was provided.
func (o *editOptions) was(name string) bool {
	return o.provided[name]
}

// anyProvided reports whether at least one editable field was
// provided.
func (o *editOptions) anyProvided() bool {
	for _, provided := range o.provided {
		if provided {
			return true
		}
	}
	return false
}

// newEditCommand builds `gophkeeper edit`: it fetches the current
// entry, applies the provided flags and re-puts the result with the
// current version (optimistic locking).
func newEditCommand(app *App) *cobra.Command {
	var id string
	opts := &editOptions{}

	cmd := &cobra.Command{
		Use:   "edit --id=<id> [--label=...] [--metadata=...] [payload flags]",
		Short: "Update an existing entry",
		Long: `Update an existing entry.

The current entry is fetched from the server, the provided flags are
applied on top of it and the result is stored back (the entry version
bumps; an update based on a stale version fails with a conflict).

Payload flags depend on the entry type:

  login:  --username, --password
  text:   --text=<content>, or --text=- to read stdin
  binary: --file=<path> replaces the stored bytes
  card:   --number, --holder, --expiry, --cvv

Only the provided flags change; everything else is kept.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := app.requireAuth(); err != nil {
				return err
			}
			opts.markProvided(cmd, "label", "metadata", "username", "password",
				"text", "file", "number", "holder", "expiry", "cvv")
			if !opts.anyProvided() {
				return errors.New("nothing to update: provide --label, --metadata or a payload flag")
			}
			fullID, err := app.resolveEntryID(cmd.Context(), id)
			if err != nil {
				return err
			}
			current, err := app.entries.Get(cmd.Context(), fullID)
			if err != nil {
				return err
			}
			updated, err := applyEditOptions(app, current, opts)
			if err != nil {
				return err
			}
			saved, err := app.entries.Edit(cmd.Context(), updated)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(app.Out, "updated entry %s (version %d)\n",
				shortID(saved.ID), saved.Version)
			return nil
		},
	}

	cmd.Flags().StringVar(&id, "id", "",
		"entry id or unique id prefix, as shown by list (required)")
	cmd.Flags().StringVar(&opts.label, "label", "", "new label")
	cmd.Flags().StringVar(&opts.metadata, "metadata", "", "new metadata")
	cmd.Flags().StringVar(&opts.username, "username", "", "new login name (login type)")
	cmd.Flags().StringVar(&opts.password, "password", "", "new password (login type)")
	cmd.Flags().StringVar(&opts.text, "text", "", "new text content, or - to read stdin (text type)")
	cmd.Flags().StringVar(&opts.file, "file", "", "path of a file replacing the stored bytes (binary type)")
	cmd.Flags().StringVar(&opts.number, "number", "", "new card number (card type)")
	cmd.Flags().StringVar(&opts.holder, "holder", "", "new card holder name (card type)")
	cmd.Flags().StringVar(&opts.expiry, "expiry", "", "new card expiry (card type)")
	cmd.Flags().StringVar(&opts.cvv, "cvv", "", "new card CVV (card type)")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

// applyEditOptions applies the provided flags to the current entry,
// returning a copy ready for Edit (the version is carried over for
// the optimistic lock). Payload flags that do not match the entry
// type are rejected; the label and metadata may be cleared with an
// explicit empty value, while entry payloads never become empty.
func applyEditOptions(app *App, current *model.Entry, opts *editOptions) (*model.Entry, error) {
	if !opts.anyProvided() {
		return nil, errors.New("nothing to update: provide --label, --metadata or a payload flag")
	}

	updated := *current
	updated.Data = append([]byte(nil), current.Data...)

	if opts.was("label") {
		updated.Label = opts.label
	}
	if opts.was("metadata") {
		updated.Metadata = opts.metadata
	}

	switch updated.Type {
	case model.EntryTypeLoginPassword:
		if err := rejectForeignEdit(opts, "text", "file", "number", "holder", "expiry", "cvv"); err != nil {
			return nil, err
		}
		if opts.was("username") || opts.was("password") {
			login, err := decodeLogin(updated.Data)
			if err != nil {
				return nil, err
			}
			if opts.was("username") {
				login.Username = opts.username
			}
			if opts.was("password") {
				login.Password = opts.password
			}
			if login.Username == "" || login.Password == "" {
				return nil, errors.New("type login requires a non-empty username and password")
			}
			data, err := encodeLogin(login.Username, login.Password)
			if err != nil {
				return nil, err
			}
			updated.Data = data
		}
	case model.EntryTypeText:
		if err := rejectForeignEdit(opts, "username", "password", "file", "number", "holder", "expiry", "cvv"); err != nil {
			return nil, err
		}
		if opts.was("text") {
			text, err := readTextPayload(opts.text, app.In)
			if err != nil {
				return nil, err
			}
			if text == "" {
				return nil, errors.New("text entry data must not be empty")
			}
			updated.Data = []byte(text)
		}
	case model.EntryTypeBinary:
		if err := rejectForeignEdit(opts, "text", "username", "password", "number", "holder", "expiry", "cvv"); err != nil {
			return nil, err
		}
		if opts.was("file") {
			data, err := os.ReadFile(opts.file)
			if err != nil {
				return nil, fmt.Errorf("read --file: %w", err)
			}
			if len(data) == 0 {
				return nil, errors.New("binary entry data must not be empty")
			}
			updated.Data = data
		}
	case model.EntryTypeCard:
		if err := rejectForeignEdit(opts, "text", "file", "username", "password"); err != nil {
			return nil, err
		}
		if opts.was("number") || opts.was("holder") || opts.was("expiry") || opts.was("cvv") {
			card, err := decodeCard(updated.Data)
			if err != nil {
				return nil, err
			}
			if opts.was("number") {
				card.Number = opts.number
			}
			if opts.was("holder") {
				card.Holder = opts.holder
			}
			if opts.was("expiry") {
				card.Expiry = opts.expiry
			}
			if opts.was("cvv") {
				card.CVV = opts.cvv
			}
			if card.Number == "" {
				return nil, errors.New("card entry requires a non-empty number")
			}
			data, err := encodeCard(card.Number, card.Holder, card.Expiry, card.CVV)
			if err != nil {
				return nil, err
			}
			updated.Data = data
		}
	default:
		return nil, fmt.Errorf("cannot edit payload of entry type %q", typeLabel(updated.Type))
	}

	return &updated, nil
}

// rejectForeignEdit reports an error when a payload flag that does
// not belong to the entry type was provided.
func rejectForeignEdit(opts *editOptions, names ...string) error {
	for _, name := range names {
		if opts.was(name) {
			return fmt.Errorf("flag --%s does not belong to this entry type", name)
		}
	}
	return nil
}
