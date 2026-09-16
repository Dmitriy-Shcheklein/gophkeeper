package cli

import (
	"fmt"

	"github.com/dmitriy/gophkeeper/internal/client/config"
	"github.com/spf13/cobra"
)

// NewRootCommand builds the gophkeeper command tree over app: the
// persistent --server/--token-path flags, the auth/entry subcommands
// and the informational commands (version, tui).
//
// Both SilenceUsage and SilenceErrors are set: on failure the caller
// (Execute) prints a single friendly message instead of a usage dump
// followed by the raw error.
func NewRootCommand(app *App) *cobra.Command {
	root := &cobra.Command{
		Use:           "gophkeeper",
		Short:         "GophKeeper — a private data keeper client",
		Long:          "GophKeeper client: store logins, texts, files and bank cards on a GophKeeper server.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(app.Out)
	root.SetErr(app.Err)
	root.PersistentFlags().StringVar(&app.serverFlag, "server", "",
		"GophKeeper server address host:port ($"+config.EnvServer+", default "+config.DefaultServer+")")
	root.PersistentFlags().StringVar(&app.tokenFlag, "token-path", "",
		"token file path ($GOPHKEEPER_TOKEN_PATH, default ~/.gophkeeper/token)")
	root.PersistentFlags().StringVar(&app.cacheFlag, "cache-path", "",
		"offline cache file path ($GOPHKEEPER_CACHE_PATH, default ~/.gophkeeper/cache.json)")

	root.AddCommand(
		newRegisterCommand(app),
		newLoginCommand(app),
		newLogoutCommand(app),
		newAddCommand(app),
		newListCommand(app),
		newGetCommand(app),
		newEditCommand(app),
		newDeleteCommand(app),
		newSyncCommand(app),
		newVersionCommand(app),
		newTUICommand(app),
	)
	return root
}

// Execute runs the CLI with the given arguments (os.Args[1:] in
// production), releases the backend connection afterwards and
// returns an error so the caller can set the exit code. Command
// failures are printed to app.Err as friendly messages; no usage
// dump is produced.
func Execute(app *App, args []string) error {
	defer app.shutdown()
	root := NewRootCommand(app)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		_, _ = fmt.Fprintf(app.Err, "gophkeeper: %s\n", friendlyMessage(err))
		return err
	}
	return nil
}
