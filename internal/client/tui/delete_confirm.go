package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbletea"

	"github.com/dmitriy/gophkeeper/internal/client/model"
	"github.com/dmitriy/gophkeeper/internal/client/render"
)

// confirmModel is the state of the delete confirmation screen.
type confirmModel struct {
	// entry is the entry the user asked to delete.
	entry *model.Entry
}

// newConfirm returns a confirmation screen for the given entry.
func newConfirm(entry *model.Entry) confirmModel {
	return confirmModel{entry: entry}
}

// updateConfirm handles keys on the delete confirmation screen: y or
// enter confirms and issues the delete command, n or esc cancels and
// returns to the previous screen.
func (m appModel) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "y", "Y", "enter":
		if m.confirm.entry == nil {
			m.popScreen()
			return m, nil
		}
		m.loading = true
		return m, deleteCmd(m.entries, m.confirm.entry.ID)
	case "n", "N", "esc", "q":
		m.popScreen()
		m.setStatus("deletion cancelled")
	}
	return m, nil
}

// viewConfirm renders the delete confirmation screen.
func (m appModel) viewConfirm() string {
	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Delete entry") + "\n\n")
	if m.confirm.entry == nil {
		sb.WriteString(dimStyle.Render("no entry selected") + "\n")
		return sb.String()
	}
	_, _ = fmt.Fprintf(&sb, "Delete %s entry %q?\n\n",
		render.TypeLabel(m.confirm.entry.Type), m.confirm.entry.Label)
	sb.WriteString(statusErrStyle.Render("This cannot be undone.") + "\n")
	sb.WriteString(helpStyle.Render("y confirm · n/esc cancel"))
	return sb.String()
}
