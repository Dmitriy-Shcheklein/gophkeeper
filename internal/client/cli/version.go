package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// newVersionCommand builds `gophkeeper version`: it prints the build
// metadata and the platform.
func newVersionCommand(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the client version and build info",
		RunE: func(_ *cobra.Command, _ []string) error {
			_, _ = fmt.Fprintf(app.Out, "gophkeeper-client %s\n", app.Version)
			_, _ = fmt.Fprintf(app.Out, "  built:    %s\n", app.BuildDate)
			_, _ = fmt.Fprintf(app.Out, "  commit:   %s\n", app.Commit)
			_, _ = fmt.Fprintf(app.Out, "  platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
			return nil
		},
	}
}
