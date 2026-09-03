package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newDeleteCommand builds `gophkeeper delete`: it removes an entry
// by id, asking for confirmation unless --yes is given.
func newDeleteCommand(app *App) *cobra.Command {
	var (
		id  string
		yes bool
	)

	cmd := &cobra.Command{
		Use:   "delete --id=<id>",
		Short: "Delete an entry",
		Long: `Delete an entry by id.

A confirmation is requested on the terminal; pass --yes to skip it
(e.g. in scripts).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := app.requireAuth(); err != nil {
				return err
			}
			if !yes && !app.confirm(fmt.Sprintf("Delete entry %s?", id)) {
				_, _ = fmt.Fprintln(app.Out, "aborted")
				return nil
			}
			fullID, err := app.resolveEntryID(cmd.Context(), id)
			if err != nil {
				return err
			}
			if err := app.entries.Remove(cmd.Context(), fullID); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(app.Out, "deleted entry %s\n", shortID(fullID))
			return nil
		},
	}

	cmd.Flags().StringVar(&id, "id", "",
		"entry id or unique id prefix, as shown by list (required)")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}
