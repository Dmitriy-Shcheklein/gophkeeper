package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newRegisterCommand builds `gophkeeper register`: it creates an
// account and logs in (the token is persisted for the next
// invocation).
func newRegisterCommand(app *App) *cobra.Command {
	var login, password string

	cmd := &cobra.Command{
		Use:   "register --login=<login>",
		Short: "Create a new account and log in",
		Long: `Create a new account on the server and log in.

On success the access token is persisted and every subsequent
command is authenticated.

Password note: passing --password exposes it in shell history; omit
the flag to be prompted for the password without echo (recommended).
No confirmation prompt is issued for the register password.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := app.initServices(); err != nil {
				return err
			}
			pass, err := app.resolvePassword(password)
			if err != nil {
				return err
			}
			if err := app.auth.Register(cmd.Context(), login, pass); err != nil {
				return err
			}
			_, _ = fmt.Fprintln(app.Out, "registered and logged in; token saved")
			return nil
		},
	}

	cmd.Flags().StringVar(&login, "login", "", "account login (required)")
	cmd.Flags().StringVar(&password, "password", "",
		"account password (omit to be prompted without echo — recommended)")
	_ = cmd.MarkFlagRequired("login")
	return cmd
}
