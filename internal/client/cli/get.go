package cli

import (
	"github.com/spf13/cobra"
)

// newGetCommand builds `gophkeeper get`: it fetches a single entry
// by id and renders it, including type-specific payload details.
func newGetCommand(app *App) *cobra.Command {
	var id string

	cmd := &cobra.Command{
		Use:   "get --id=<id>",
		Short: "Show a single entry",
		Long: `Show a single entry, including its payload.

Payload rendering by type: login entries show username and password;
card entries mask the number except the last 4 digits and the CVV
completely; text entries show the raw text; binary entries show the
payload size only.`,
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
			renderEntry(app.Out, entry)
			return nil
		},
	}

	cmd.Flags().StringVar(&id, "id", "",
		"entry id or unique id prefix, as shown by list (required)")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}
