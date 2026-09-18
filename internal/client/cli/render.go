package cli

import (
	"io"

	"github.com/dmitriy/gophkeeper/internal/client/model"
	"github.com/dmitriy/gophkeeper/internal/client/render"
)

// shortID truncates an entry id to 8 characters for the table view;
// shorter ids are returned unchanged. It delegates to the shared
// render package used by the CLI and the TUI.
func shortID(id string) string {
	return render.ShortID(id)
}

// renderTable prints the entries as the shared ID/TYPE/LABEL/VERSION/
// UPDATED table used by list and sync; it prints "no entries" when
// the selection is empty. It delegates to the shared render package.
func renderTable(w io.Writer, entries []*model.Entry) {
	render.Table(w, entries)
}

// renderEntry prints the full entry with type-specific rendering of
// the payload. It delegates to the shared render package.
func renderEntry(w io.Writer, e *model.Entry) {
	render.Entry(w, e)
}
