package cli

import (
	"github.com/dmitriy/gophkeeper/internal/client/model"
	"github.com/spf13/cobra"
)

// newListCommand builds `gophkeeper list`: it prints the user's
// entries as a table. The proto List call has no server-side type
// filter, so --type filters client-side after the full fetch.
func newListCommand(app *App) *cobra.Command {
	var typeFlag string

	cmd := &cobra.Command{
		Use:   "list [--type=<login|text|binary|card>]",
		Short: "List entries",
		Long: `List the entries of the authenticated user as a table.

The server returns all entries in one call, so --type filters the
results client-side. The ID column shows the first 8 characters of
each id; get, edit and delete accept the full id or a unique prefix
of it.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := app.requireAuth(); err != nil {
				return err
			}
			entries, err := app.entries.List(cmd.Context())
			if err != nil && !app.noteOffline(err) {
				return err
			}
			if typeFlag != "" {
				entryType, err := parseTypeFlag(typeFlag)
				if err != nil {
					return err
				}
				entries = filterByType(entries, entryType)
			}
			renderTable(app.Out, entries)
			return nil
		},
	}

	cmd.Flags().StringVar(&typeFlag, "type", "",
		"only show entries of this type: login, text, binary or card")
	return cmd
}

// filterByType returns the entries of the given type, preserving the
// server order.
func filterByType(entries []*model.Entry, entryType model.EntryType) []*model.Entry {
	filtered := make([]*model.Entry, 0, len(entries))
	for _, e := range entries {
		if e.Type == entryType {
			filtered = append(filtered, e)
		}
	}
	return filtered
}
