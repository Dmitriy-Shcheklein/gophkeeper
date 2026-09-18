package tui

import (
	"context"
	"os"
	"strings"

	"github.com/charmbracelet/bubbletea"

	"github.com/dmitriy/gophkeeper/internal/client/model"
	"github.com/dmitriy/gophkeeper/internal/client/render"
)

// updateDetail handles keys on the entry detail screen: esc returns
// to the list, e opens the edit form, d the delete confirmation and
// s streams the payload of a binary entry into a file (path asked on
// the save screen).
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
	case "s":
		if m.current != nil && m.current.Type == model.EntryTypeBinary {
			m.save = newSaveModel()
			m.pushScreen(screenSave)
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
	help := "e edit · d delete · esc back"
	if m.current != nil && m.current.Type == model.EntryTypeBinary {
		help = "s save to file · " + help
	}
	sb.WriteString(helpStyle.Render(help))
	return sb.String()
}

// downloadCmd streams the payload of the entry into the file at path
// in the background.
func downloadCmd(entries entryClient, id, path string) tea.Cmd {
	return func() tea.Msg {
		file, err := os.Create(path)
		if err != nil {
			return downloadedMsg{path: path, err: err}
		}
		err = entries.Download(context.Background(), id, file)
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(path)
		}
		return downloadedMsg{path: path, err: err}
	}
}
