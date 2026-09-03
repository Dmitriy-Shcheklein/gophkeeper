package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newSyncCommand builds `gophkeeper sync`: it fetches the full
// current server state and prints it.
func newSyncCommand(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Fetch the full current state from the server",
		Long: `Fetch the full current state from the server and print it.

Sync replaces the local view with the server state: the printed set
is the authoritative list of the user's entries at this moment.
The TUI loads the same state when it opens.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := app.requireAuth(); err != nil {
				return err
			}
			entries, err := app.entries.Sync(cmd.Context())
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(app.Out, "synced %d entries (this replaces the local view with the server state)\n", len(entries))
			renderTable(app.Out, entries)
			return nil
		},
	}
}
