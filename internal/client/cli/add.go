package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/dmitriy/gophkeeper/internal/client/model"
	"github.com/spf13/cobra"
)

// addOptions carries the payload flags of the add command.
type addOptions struct {
	// typeFlag is the raw --type value.
	typeFlag string
	// label and metadata are shared by all entry types.
	label    string
	metadata string
	// username and password feed the login payload.
	username string
	password string
	// text feeds the text payload; "-" means "read stdin".
	text string
	// file is the path of the binary payload.
	file string
	// number, holder, expiry and cvv feed the card payload.
	number string
	holder string
	expiry string
	cvv    string
}

// newAddCommand builds `gophkeeper add`: it assembles the payload
// for the requested entry type and stores it on the server.
func newAddCommand(app *App) *cobra.Command {
	opts := &addOptions{}

	cmd := &cobra.Command{
		Use:   "add --type=<login|text|binary|card> --label=<label>",
		Short: "Store a new entry",
		Long: `Store a new entry on the server.

The payload flags depend on --type:

  login:  --username and --password, stored as JSON
          {"username":"...","password":"..."}
  text:   --text=<content>; use --text=- to read the text from stdin
  binary: --file=<path>, the file bytes are stored as-is
  card:   --number, --holder, --expiry and --cvv, stored as JSON
          {"number":"...","holder":"...","expiry":"...","cvv":"..."}

--metadata is an optional free-form note available to all types.

Password note: --password (for both login entries and registration)
is visible in shell history; prefer a password manager or an
interactive prompt.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := app.requireAuth(); err != nil {
				return err
			}
			entryType, err := parseTypeFlag(opts.typeFlag)
			if err != nil {
				return err
			}

			// Binary entries are streamed from the file in chunks, so
			// arbitrarily large files are stored without loading them
			// into memory.
			if entryType == model.EntryTypeBinary {
				return runAddBinary(cmd, app, opts)
			}

			data, err := buildAddData(app, entryType, opts)
			if err != nil {
				return err
			}
			created, err := app.entries.Add(cmd.Context(), &model.Entry{
				Type:     entryType,
				Label:    opts.label,
				Metadata: opts.metadata,
				Data:     data,
			})
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(app.Out, "created entry %s (version %d)\n",
				shortID(created.ID), created.Version)
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.typeFlag, "type", "", "entry type: login, text, binary or card (required)")
	cmd.Flags().StringVar(&opts.label, "label", "", "entry label (required)")
	cmd.Flags().StringVar(&opts.metadata, "metadata", "", "optional free-form metadata")
	cmd.Flags().StringVar(&opts.username, "username", "", "login name (login type)")
	cmd.Flags().StringVar(&opts.password, "password", "", "password (login type)")
	cmd.Flags().StringVar(&opts.text, "text", "", "text content, or - to read stdin (text type)")
	cmd.Flags().StringVar(&opts.file, "file", "", "path of the file to store (binary type)")
	cmd.Flags().StringVar(&opts.number, "number", "", "card number (card type)")
	cmd.Flags().StringVar(&opts.holder, "holder", "", "card holder name (card type)")
	cmd.Flags().StringVar(&opts.expiry, "expiry", "", "card expiry, e.g. 12/28 (card type)")
	cmd.Flags().StringVar(&opts.cvv, "cvv", "", "card CVV (card type)")
	_ = cmd.MarkFlagRequired("type")
	_ = cmd.MarkFlagRequired("label")
	return cmd
}

// runAddBinary stores a binary entry streamed from --file: the file
// is read piece by piece (never fully in memory), its SHA-256 digest
// is verified by the server.
func runAddBinary(cmd *cobra.Command, app *App, opts *addOptions) error {
	if err := rejectForeignFlags(opts, "text", "username", "password", "number", "holder", "expiry", "cvv"); err != nil {
		return err
	}
	if opts.file == "" {
		return errors.New("type binary requires --file=<path>")
	}
	file, err := os.Open(opts.file)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return errors.New("binary entry data must not be empty")
	}

	created, err := app.entries.Upload(cmd.Context(), &model.Entry{
		Type:     model.EntryTypeBinary,
		Label:    opts.label,
		Metadata: opts.metadata,
	}, 0, file)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(app.Out, "created entry %s (version %d, %d bytes, streamed)\n",
		shortID(created.ID), created.Version, created.DataSize)
	return nil
}

// buildAddData assembles the entry payload for the given type from
// the add flags. Data flags that do not belong to the type are
// rejected to catch typos early.
func buildAddData(app *App, entryType model.EntryType, opts *addOptions) ([]byte, error) {
	switch entryType {
	case model.EntryTypeLoginPassword:
		if err := rejectForeignFlags(opts, "file", "text", "number", "holder", "expiry", "cvv"); err != nil {
			return nil, err
		}
		if opts.username == "" || opts.password == "" {
			return nil, errors.New("type login requires --username and --password")
		}
		return encodeLogin(opts.username, opts.password)
	case model.EntryTypeText:
		if err := rejectForeignFlags(opts, "file", "username", "password", "number", "holder", "expiry", "cvv"); err != nil {
			return nil, err
		}
		if opts.text == "" {
			return nil, errors.New("type text requires --text (use --text=- to read stdin)")
		}
		text, err := readTextPayload(opts.text, app.In)
		if err != nil {
			return nil, err
		}
		if text == "" {
			return nil, errors.New("text entry data must not be empty")
		}
		return []byte(text), nil
	case model.EntryTypeBinary:
		if err := rejectForeignFlags(opts, "text", "username", "password", "number", "holder", "expiry", "cvv"); err != nil {
			return nil, err
		}
		if opts.file == "" {
			return nil, errors.New("type binary requires --file=<path>")
		}
		data, err := os.ReadFile(opts.file)
		if err != nil {
			return nil, err
		}
		if len(data) == 0 {
			return nil, errors.New("binary entry data must not be empty")
		}
		return data, nil
	case model.EntryTypeCard:
		if err := rejectForeignFlags(opts, "file", "text", "username", "password"); err != nil {
			return nil, err
		}
		if opts.number == "" {
			return nil, errors.New("type card requires --number")
		}
		return encodeCard(opts.number, opts.holder, opts.expiry, opts.cvv)
	default:
		return nil, fmt.Errorf("cannot build payload for type %q", typeLabel(entryType))
	}
}

// rejectForeignFlags reports an error when a payload flag that does
// not belong to the entry type was provided. A flag counts as
// provided when it is non-empty.
func rejectForeignFlags(opts *addOptions, names ...string) error {
	values := map[string]string{
		"username": opts.username,
		"password": opts.password,
		"text":     opts.text,
		"file":     opts.file,
		"number":   opts.number,
		"holder":   opts.holder,
		"expiry":   opts.expiry,
		"cvv":      opts.cvv,
	}
	for _, name := range names {
		if values[name] != "" {
			return fmt.Errorf("flag --%s does not belong to this entry type", name)
		}
	}
	return nil
}
