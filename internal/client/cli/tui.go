package cli

import (
	"github.com/spf13/cobra"

	"github.com/dmitriy/gophkeeper/internal/client/tui"
)

// newTUICommand builds `gophkeeper tui`: the interactive terminal UI
// over the same service layer as the other commands. It requires an
// authenticated session (the standard requireAuth guard); without
// one the friendly "not authenticated" error is printed and the
// process exits non-zero instead of starting the program.
func newTUICommand(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Interactive terminal UI",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := app.requireAuth(); err != nil {
				return err
			}
			if app.runTUI != nil {
				return app.runTUI(app.auth, app.entries)
			}
			return tui.Run(app.auth, app.entries)
		},
	}
}
