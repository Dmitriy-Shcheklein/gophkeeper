package cli

import (
	"github.com/spf13/cobra"

	"github.com/dmitriy/gophkeeper/internal/client/tui"
)

// newTUICommand builds `gophkeeper tui`: the interactive terminal UI
// over the same service layer as the other commands. It starts
// without a saved token too: in that case the TUI shows its own
// login/register screen instead of the entry list.
func newTUICommand(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Interactive terminal UI (includes login/register)",
		RunE: func(_ *cobra.Command, _ []string) error {
			// The TUI handles login/register on its own auth screen,
			// so only the lazy service wiring is needed here — no
			// authentication guard.
			if err := app.initServices(); err != nil {
				return err
			}
			if app.runTUI != nil {
				return app.runTUI(app.auth, app.entries)
			}
			return tui.Run(app.auth, app.entries)
		},
	}
}
