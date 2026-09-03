package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newTUICommand builds `gophkeeper tui`: a stub that keeps the
// command surface stable until the interactive UI stage replaces it.
func newTUICommand(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Interactive terminal UI (not implemented yet)",
		RunE: func(_ *cobra.Command, _ []string) error {
			_, _ = fmt.Fprintln(app.Out, "TUI is not implemented yet")
			return nil
		},
	}
}
