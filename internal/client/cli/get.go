package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/dmitriy/gophkeeper/internal/client/model"
	"github.com/spf13/cobra"
)

// newGetCommand builds `gophkeeper get`: it fetches a single entry
// by id and renders it, including type-specific payload details.
// Binary payloads are not shown (they are opaque); --out=<path>
// streams the payload into a file instead.
func newGetCommand(app *App) *cobra.Command {
	var id string
	var out string

	cmd := &cobra.Command{
		Use:   "get --id=<id> [--out=<path>]",
		Short: "Show a single entry (or save its payload with --out)",
		Long: `Show a single entry, including its payload.

Payload rendering by type: login entries show username and password;
card entries mask the number except the last 4 digits and the CVV
completely; text entries show the raw text; binary entries show the
payload size only.

Binary payloads are saved instead of shown with --out=<path>: the
content is streamed from the server and written to the file without
loading it fully into memory.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := app.requireAuth(); err != nil {
				return err
			}
			fullID, err := app.resolveEntryID(cmd.Context(), id)
			if err != nil {
				return err
			}
			entry, err := app.entries.Get(cmd.Context(), fullID)
			if err != nil {
				return err
			}
			if out != "" && entry.Type == model.EntryTypeBinary {
				return runDownloadBinary(cmd.Context(), app, entry, out)
			}
			renderEntry(app.Out, entry)
			return nil
		},
	}

	cmd.Flags().StringVar(&id, "id", "",
		"entry id or unique id prefix, as shown by list (required)")
	cmd.Flags().StringVar(&out, "out", "",
		"save the binary payload to this file instead of showing it")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

// runDownloadBinary streams the payload of a binary entry into the
// file at path.
func runDownloadBinary(ctx context.Context, app *App, entry *model.Entry, path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := app.entries.Download(ctx, entry.ID, file); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(app.Out, "saved payload of %s (%s) to %s (%d bytes)\n",
		shortID(entry.ID), entry.Label, path, entry.DataSize)
	return nil
}
