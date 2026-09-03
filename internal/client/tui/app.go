package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbletea"

	"github.com/dmitriy/gophkeeper/internal/client/model"
	"github.com/dmitriy/gophkeeper/internal/client/render"
)

// screen identifies a screen of the navigation stack.
type screen int

// The screens of the TUI. The stack always conceptually starts with
// the list; detail, form and confirm are pushed on top of it. The
// auth screen replaces the list when the TUI starts without a saved
// token (and is popped once login/register succeeds).
const (
	screenList screen = iota
	screenAuth
	screenDetail
	screenForm
	screenConfirm
	screenSave
)

// Async operation results delivered to the model by tea.Cmd values.
type (
	// entriesLoadedMsg carries the result of an initial or refresh
	// load of the full entry set.
	entriesLoadedMsg struct {
		entries []*model.Entry
		err     error
	}
	// savedMsg carries the result of a create or edit.
	savedMsg struct {
		saved *model.Entry
		err   error
	}
	// deletedMsg carries the result of a delete.
	deletedMsg struct{ err error }
	// downloadedMsg carries the result of a save-to-file download.
	downloadedMsg struct {
		path string
		err  error
	}
)

// appModel is the root bubbletea model: it owns the navigation
// stack, the entry set, the status bar and routes keys to the
// topmost screen.
type appModel struct {
	// authSvc is the authentication service; non-nil only while the
	// auth screen is relevant (it submits login/register).
	authSvc authClient
	entries entryClient

	// width and height are the terminal dimensions from the last
	// tea.WindowSizeMsg.
	width, height int

	// stack is the navigation stack; the topmost screen receives
	// the keys. An empty stack means the list screen.
	stack []screen

	// all is the last loaded full entry set; visible is the
	// filtered, ordered view shown by the list.
	all     []*model.Entry
	visible []*model.Entry

	// filter is the applied substring filter (matched against the
	// label, case-insensitively); filtering is true while the
	// filter input has focus.
	filter      string
	filtering   bool
	filterInput textinput.Model

	// cursor is the selected row of the visible list; offset is the
	// first visible row when the list exceeds the viewport height.
	cursor int
	offset int

	// loading is true while an async load/refresh is in flight.
	loading bool

	// status is the bottom status bar message; statusErr marks it
	// as an error (red). Empty status means the help line only.
	status    string
	statusErr bool

	// current is the entry behind the detail and edit screens (and
	// the delete confirmation).
	current *model.Entry

	// form and confirm hold the state of the stacked screens.
	form    formModel
	confirm confirmModel
	// auth holds the state of the login/register screen.
	auth authModel
	// save holds the state of the save-to-file screen.
	save saveModel

	// quitting is set when the user asked to exit.
	quitting bool
}

// newAppModel returns the root model for the given services. Without
// an authenticated session it starts on the login/register screen
// instead of the list; with one, the entry set is loaded
// asynchronously by Init.
func newAppModel(auth authClient, entries entryClient) appModel {
	fi := textinput.New()
	fi.Placeholder = "filter by label..."
	fi.Prompt = "/"
	fi.PromptStyle = filterStyle
	m := appModel{
		authSvc:     auth,
		entries:     entries,
		filterInput: fi,
		visible:     nil,
	}
	if auth != nil && !auth.IsAuthenticated() {
		m.auth = newAuthModel()
		m.pushScreen(screenAuth)
	}
	return m
}

// Init kicks off the asynchronous initial load of the entry set; on
// the auth screen there is nothing to load yet.
func (m appModel) Init() tea.Cmd {
	if m.topScreen() == screenAuth {
		return nil
	}
	return loadEntriesCmd(m.entries)
}

// loadEntriesCmd fetches the full entry set in the background.
func loadEntriesCmd(entries entryClient) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		entries, err := entries.Sync(ctx)
		return entriesLoadedMsg{entries: entries, err: err}
	}
}

// saveCmd creates or updates the entry in the background.
func saveCmd(entries entryClient, entry *model.Entry, isNew bool) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		var saved *model.Entry
		var err error
		if isNew {
			saved, err = entries.Add(ctx, entry)
		} else {
			saved, err = entries.Edit(ctx, entry)
		}
		return savedMsg{saved: saved, err: err}
	}
}

// deleteCmd removes the entry in the background.
func deleteCmd(entries entryClient, id string) tea.Cmd {
	return func() tea.Msg {
		err := entries.Remove(context.Background(), id)
		return deletedMsg{err: err}
	}
}

// uploadCmd stores the entry streamed from the file in the background
// (never holding the whole payload in memory).
func uploadCmd(entries entryClient, entry *model.Entry, expectedVersion int64, path string) tea.Cmd {
	return func() tea.Msg {
		file, err := os.Open(path)
		if err != nil {
			return savedMsg{err: err}
		}
		saved, err := entries.Upload(context.Background(), entry, expectedVersion, file)
		_ = file.Close()
		return savedMsg{saved: saved, err: err}
	}
}

// topScreen returns the screen that currently receives the keys.
func (m appModel) topScreen() screen {
	if len(m.stack) == 0 {
		return screenList
	}
	return m.stack[len(m.stack)-1]
}

// pushScreen switches to a new screen.
func (m *appModel) pushScreen(s screen) {
	m.stack = append(m.stack, s)
}

// popScreen returns to the previous screen.
func (m *appModel) popScreen() {
	if len(m.stack) > 0 {
		m.stack = m.stack[:len(m.stack)-1]
	}
}

// setStatus shows a success/neutral status message.
func (m *appModel) setStatus(format string, args ...any) {
	m.status = fmt.Sprintf(format, args...)
	m.statusErr = false
}

// setError shows an error status message.
func (m *appModel) setError(err error) {
	m.status = err.Error()
	m.statusErr = true
}

// applyFilter recomputes the visible list from all and filter and
// clamps the cursor into range.
func (m *appModel) applyFilter() {
	f := strings.ToLower(strings.TrimSpace(m.filter))
	if f == "" {
		m.visible = m.all
	} else {
		m.visible = make([]*model.Entry, 0, len(m.all))
		for _, e := range m.all {
			if strings.Contains(strings.ToLower(e.Label), f) ||
				strings.Contains(render.TypeLabel(e.Type), f) {
				m.visible = append(m.visible, e)
			}
		}
	}
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.clampOffset()
}

// selected returns the entry under the cursor, or nil.
func (m appModel) selected() *model.Entry {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return nil
	}
	return m.visible[m.cursor]
}

// viewportRows is the number of list rows that fit above the status
// bar; at least 1.
func (m appModel) viewportRows() int {
	rows := m.height - 3 // title + status line + help line
	if rows < 1 {
		rows = 1
	}
	return rows
}

// clampOffset keeps the cursor inside the visible window.
func (m *appModel) clampOffset() {
	rows := m.viewportRows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// Update handles messages: window resizes, async operation results
// and key presses (routed to the topmost screen).
func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampOffset()
		return m, nil

	case entriesLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.setError(fmt.Errorf("load entries: %w", msg.err))
			return m, nil
		}
		m.all = msg.entries
		m.applyFilter()
		m.setStatus("loaded %d entries", len(msg.entries))
		return m, nil

	case savedMsg:
		m.loading = false
		if msg.err != nil {
			// Stay in the form so the user can retry or cancel:
			// a conflict means someone else changed the entry.
			m.setError(msg.err)
			return m, nil
		}
		m.popScreen() // close the form
		if msg.saved != nil {
			// The detail screen (when open underneath) must show
			// the server's copy, not the stale pre-edit entry:
			// re-editing the stale version would always conflict.
			m.current = msg.saved
			m.setStatus("saved %q (v%d)", msg.saved.Label, msg.saved.Version)
		} else {
			m.setStatus("saved")
		}
		return m, loadEntriesCmd(m.entries)

	case authedMsg:
		m.loading = false
		if msg.err != nil {
			// Stay on the auth screen so the user can retry.
			m.auth.err = msg.err.Error()
			return m, nil
		}
		m.stack = nil
		m.setStatus("logged in")
		return m, loadEntriesCmd(m.entries)

	case deletedMsg:
		m.loading = false
		if msg.err != nil {
			m.setError(msg.err)
			return m, nil
		}
		// Deletion always lands on the list, even when the
		// confirmation was opened from the detail screen (which
		// would otherwise show a deleted entry).
		m.stack = nil
		m.current = nil
		m.setStatus("entry deleted")
		return m, loadEntriesCmd(m.entries)

	case downloadedMsg:
		m.loading = false
		if msg.err != nil {
			m.setError(msg.err)
			return m, nil
		}
		m.popScreen() // back to the detail screen
		m.setStatus("saved to %s", msg.path)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// handleKey routes a key press to the topmost screen.
func (m appModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.topScreen() {
	case screenAuth:
		return m.updateAuth(msg)
	case screenList:
		return m.updateList(msg)
	case screenDetail:
		return m.updateDetail(msg)
	case screenForm:
		return m.updateForm(msg)
	case screenConfirm:
		return m.updateConfirm(msg)
	case screenSave:
		return m.updateSave(msg)
	}
	return m, nil
}

// View renders the topmost screen.
func (m appModel) View() string {
	if m.quitting {
		return ""
	}
	switch m.topScreen() {
	case screenAuth:
		return m.viewAuth()
	case screenDetail:
		return m.viewDetail()
	case screenForm:
		return m.viewForm()
	case screenConfirm:
		return m.viewConfirm()
	case screenSave:
		return m.viewSave()
	default:
		return m.viewList()
	}
}

// openNewForm prepares the form for creating an entry.
func (m *appModel) openNewForm() {
	m.form = newForm()
	m.pushScreen(screenForm)
}

// openEditForm prepares the form pre-filled from m.current.
func (m *appModel) openEditForm() {
	m.form = newFormForEdit(m.current)
	m.pushScreen(screenForm)
}

// openConfirm prepares the delete confirmation for m.current.
func (m *appModel) openConfirm() {
	m.confirm = newConfirm(m.current)
	m.pushScreen(screenConfirm)
}
