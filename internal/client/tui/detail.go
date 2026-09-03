package tui

import (
	"strings"

	"github.com/charmbracelet/bubbletea"

	"github.com/dmitriy/gophkeeper/internal/client/render"
)

// updateDetail handles keys on the entry detail screen: esc returns
// to the list, e opens the edit form, d the delete confirmation.
func (m appModel) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.popScreen()
		m.current = nil
	case "e":
		if m.current != nil {
			m.openEditForm()
		}
	case "d":
		if m.current != nil {
			m.openConfirm()
		}
	}
	return m, nil
}

// viewDetail renders the detail screen: the shared entry rendering
// (same format as `gophkeeper get`) above the status/help bar.
func (m appModel) viewDetail() string {
	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Entry details") + "\n\n")

	if m.current == nil {
		sb.WriteString(dimStyle.Render("no entry selected") + "\n")
		sb.WriteString(m.viewDetailHelp())
		return sb.String()
	}

	sb.WriteString(render.EntryString(m.current))
	sb.WriteString("\n")
	sb.WriteString(m.viewDetailHelp())
	return sb.String()
}

// viewDetailHelp renders the status bar with the detail screen keys.
func (m appModel) viewDetailHelp() string {
	var sb strings.Builder
	if m.status != "" {
		if m.statusErr {
			sb.WriteString(statusErrStyle.Render("✗ "+m.status) + "\n")
		} else {
			sb.WriteString(statusOKStyle.Render("✓ "+m.status) + "\n")
		}
	}
	sb.WriteString(helpStyle.Render("e edit · d delete · esc back"))
	return sb.String()
}
