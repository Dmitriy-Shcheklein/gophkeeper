package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newLogoutCommand builds `gophkeeper logout`: it clears the
// persisted and in-memory token. It is idempotent.
func newLogoutCommand(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Clear the saved access token",
		Long: `Clear the saved access token.

After logging out the data commands refuse to run until
` + "`gophkeeper login`" + ` is executed again. Logging out without a
saved token is not an error.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := app.initServices(); err != nil {
				return err
			}
			if err := app.auth.Logout(); err != nil {
				return err
			}
			_, _ = fmt.Fprintln(app.Out, "logged out; token removed")
			return nil
		},
	}
}
