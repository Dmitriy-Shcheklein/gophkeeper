package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// Shared lipgloss styles of the TUI screens. Colors degrade
// gracefully on terminals without true color support.

var (
	// titleStyle renders screen titles (list header, form heading).
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	// selectedStyle highlights the selected list row.
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	// statusErrStyle renders error status messages.
	statusErrStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
	// statusOKStyle renders success status messages.
	statusOKStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("71"))
	// helpStyle renders the bottom help/status bar.
	helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	// dimStyle renders secondary information (empty states, hints).
	dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	// filterStyle renders the active filter prompt.
	filterStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
)
