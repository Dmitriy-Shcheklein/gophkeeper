package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbletea"

	"github.com/dmitriy/gophkeeper/internal/client/render"
)

// updateList handles keys on the entry list screen.
func (m appModel) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		return m.updateFilter(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c", "esc":
		m.quitting = true
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m.clampOffset()
		}
	case "down", "j":
		if m.cursor < len(m.visible)-1 {
			m.cursor++
			m.clampOffset()
		}
	case "home", "g":
		m.cursor, m.offset = 0, 0
	case "end", "G":
		m.cursor = len(m.visible) - 1
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.clampOffset()
	case "enter":
		if e := m.selected(); e != nil {
			m.current = e
			m.pushScreen(screenDetail)
		}
	case "n":
		m.openNewForm()
	case "e":
		if e := m.selected(); e != nil {
			m.current = e
			m.openEditForm()
		}
	case "d":
		if e := m.selected(); e != nil {
			m.current = e
			m.openConfirm()
		}
	case "r":
		m.loading = true
		return m, loadEntriesCmd(m.entries)
	case "/":
		m.filtering = true
		m.filterInput.SetValue("")
		m.filterInput.Focus()
		return m, textinput.Blink
	}
	return m, nil
}

// updateFilter handles keys while the filter input has focus: the
// input receives ordinary text; enter applies the filter, esc clears
// and closes it.
func (m appModel) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "enter":
		m.filtering = false
		m.filterInput.Blur()
		m.filter = m.filterInput.Value()
		m.cursor, m.offset = 0, 0
		m.applyFilter()
		if m.filter != "" {
			m.setStatus("filter %q: %d entries", m.filter, len(m.visible))
		}
		return m, nil
	case "esc":
		m.filtering = false
		m.filterInput.Blur()
		m.filterInput.SetValue("")
		m.filter = ""
		m.cursor, m.offset = 0, 0
		m.applyFilter()
		return m, nil
	}
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	return m, cmd
}

// viewList renders the list screen: title (and filter input when
// active), the TYPE/LABEL/VERSION table with the selected row
// highlighted, and the status/help bar.
func (m appModel) viewList() string {
	var sb strings.Builder

	if m.filtering {
		sb.WriteString(titleStyle.Render("Filter") + " " + m.filterInput.View() + "\n")
	} else if m.filter != "" {
		sb.WriteString(titleStyle.Render("Entries — filter: ") + filterStyle.Render(fmt.Sprintf("%q", m.filter)) + "\n")
	} else {
		header := "Entries"
		if m.loading {
			header += "  ⟳ loading..."
		}
		sb.WriteString(titleStyle.Render(header) + "\n")
	}

	if len(m.visible) == 0 {
		if m.filter != "" {
			sb.WriteString(dimStyle.Render("no entries match the filter") + "\n")
		} else if m.loading {
			sb.WriteString(dimStyle.Render("loading entries...") + "\n")
		} else {
			sb.WriteString(dimStyle.Render("no entries yet — press n to create one") + "\n")
		}
		sb.WriteString(m.viewStatusBar())
		return sb.String()
	}

	rows := m.viewportRows()
	end := m.offset + rows
	if end > len(m.visible) {
		end = len(m.visible)
	}

	for i := m.offset; i < end; i++ {
		e := m.visible[i]
		row := fmt.Sprintf("  %-8s%-24s v%-4d%9s", render.TypeLabel(e.Type), truncate(e.Label, 24), e.Version, render.SizeLabel(e.DataSize))
		if i == m.cursor {
			sb.WriteString(selectedStyle.Render("> "+row) + "\n")
		} else {
			sb.WriteString("  " + row + "\n")
		}
	}

	if len(m.visible) > rows {
		sb.WriteString(dimStyle.Render(fmt.Sprintf("  showing %d–%d of %d", m.offset+1, end, len(m.visible))) + "\n")
	}

	sb.WriteString(m.viewStatusBar())
	return sb.String()
}

// truncate shortens s to at most limit runes, appending an ellipsis.
func truncate(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "…"
}

// viewStatusBar renders the bottom status line and the screen help.
func (m appModel) viewStatusBar() string {
	var sb strings.Builder
	if m.status != "" {
		if m.statusErr {
			sb.WriteString(statusErrStyle.Render("✗ "+m.status) + "\n")
		} else {
			sb.WriteString(statusOKStyle.Render("✓ "+m.status) + "\n")
		}
	}
	help := "↑/k ↓/j move · enter view · n new · e edit · d delete · / filter · r refresh · q quit"
	if m.filtering {
		help = "enter apply · esc clear filter"
	}
	sb.WriteString(helpStyle.Render(help))
	return sb.String()
}
