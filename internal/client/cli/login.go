package cli

import (
	"errors"
	"fmt"

	"github.com/dmitriy/gophkeeper/internal/client/gateway"
	"github.com/spf13/cobra"
)

// newLoginCommand builds `gophkeeper login`: it authenticates an
// existing account and persists the token.
func newLoginCommand(app *App) *cobra.Command {
	var login, password string

	cmd := &cobra.Command{
		Use:   "login --login=<login>",
		Short: "Authenticate and save the access token",
		Long: `Authenticate against the server and save the access token.

On success every subsequent command is authenticated.

Password note: passing --password exposes it in shell history; omit
the flag to be prompted for the password without echo (recommended).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := app.initServices(); err != nil {
				return err
			}
			pass, err := app.resolvePassword(password)
			if err != nil {
				return err
			}
			if err := app.auth.Login(cmd.Context(), login, pass); err != nil {
				if errors.Is(err, gateway.ErrUnauthenticated) {
					// On the login call Unauthenticated means bad
					// credentials, not "please log in".
					return ErrInvalidCredentials
				}
				return err
			}
			_, _ = fmt.Fprintln(app.Out, "logged in; token saved")
			return nil
		},
	}

	cmd.Flags().StringVar(&login, "login", "", "account login (required)")
	cmd.Flags().StringVar(&password, "password", "",
		"account password (omit to be prompted without echo — recommended)")
	_ = cmd.MarkFlagRequired("login")
	return cmd
}
