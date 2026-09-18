package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbletea"
)

// saveModel is the save-to-file screen: it asks for the destination
// path of a binary payload and starts the streaming download.
type saveModel struct {
	input textinput.Model
	err   string
}

// newSaveModel returns the save screen focused on the path input.
func newSaveModel() saveModel {
	ti := textinput.New()
	ti.Placeholder = "path to save the file to..."
	ti.Focus()
	return saveModel{input: ti}
}

// updateSave handles keys on the save screen: esc cancels, enter
// starts the download.
func (m appModel) updateSave(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.current == nil {
		m.popScreen()
		return m, nil
	}
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.popScreen()
		return m, nil
	case "enter":
		path := strings.TrimSpace(m.save.input.Value())
		if path == "" {
			m.save.err = "path must not be empty"
			return m, nil
		}
		m.loading = true
		m.save.err = ""
		return m, downloadCmd(m.entries, m.current.ID, path)
	}
	m.save.input, _ = m.save.input.Update(msg)
	return m, nil
}

// viewSave renders the save-to-file screen.
func (m appModel) viewSave() string {
	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Save binary payload") + "\n\n")
	if m.current != nil {
		sb.WriteString(dimStyle.Render("entry: "+m.current.Label) + "\n")
	}
	sb.WriteString("Path: " + m.save.input.View() + "\n")
	if m.save.err != "" {
		sb.WriteString("\n" + statusErrStyle.Render("✗ "+m.save.err) + "\n")
	}
	sb.WriteString("\n" + helpStyle.Render("enter save · esc cancel"))
	return sb.String()
}
