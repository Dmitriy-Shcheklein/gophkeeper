package tui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/dmitriy/gophkeeper/internal/client/gateway"
	"github.com/dmitriy/gophkeeper/internal/client/model"
	"github.com/dmitriy/gophkeeper/internal/client/render"
)

// fakeAuth is an in-memory authClient fake.
type fakeAuth struct {
	token string
}

func (f *fakeAuth) Restore() error        { return nil }
func (f *fakeAuth) IsAuthenticated() bool { return f.token != "" }

// fakeEntries is an in-memory entryClient fake recording calls and
// configurable failures.
type fakeEntries struct {
	entries map[string]*model.Entry
	nextID  int

	added  []*model.Entry
	edited []*model.Entry
	// editVersions records the versions carried by the entries as
	// they were submitted (the fake bumps them on storage).
	editVersions []int64
	removed      []string
	synced       int

	addErr    error
	editErr   error
	removeErr error
	syncErr   error
}

func newFakeEntries(entries ...*model.Entry) *fakeEntries {
	f := &fakeEntries{entries: make(map[string]*model.Entry, len(entries))}
	for _, e := range entries {
		f.entries[e.ID] = e
	}
	f.nextID = len(entries) + 1
	return f
}

func (f *fakeEntries) Add(_ context.Context, entry *model.Entry) (*model.Entry, error) {
	if f.addErr != nil {
		return nil, f.addErr
	}
	created := *entry
	created.ID = fmt.Sprintf("entry-%03d", f.nextID)
	f.nextID++
	created.Version = 1
	created.CreatedAt = time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	created.UpdatedAt = created.CreatedAt
	f.entries[created.ID] = &created
	f.added = append(f.added, &created)
	return &created, nil
}

func (f *fakeEntries) List(context.Context) ([]*model.Entry, error) {
	return f.all(), nil
}

func (f *fakeEntries) Get(_ context.Context, id string) (*model.Entry, error) {
	entry, ok := f.entries[id]
	if !ok {
		return nil, gateway.ErrNotFound
	}
	return entry, nil
}

func (f *fakeEntries) Edit(_ context.Context, entry *model.Entry) (*model.Entry, error) {
	if f.editErr != nil {
		return nil, f.editErr
	}
	stored, ok := f.entries[entry.ID]
	if !ok {
		return nil, gateway.ErrNotFound
	}
	f.editVersions = append(f.editVersions, entry.Version)
	updated := *entry
	updated.Version = stored.Version + 1
	f.entries[entry.ID] = &updated
	f.edited = append(f.edited, &updated)
	return &updated, nil
}

func (f *fakeEntries) Remove(_ context.Context, id string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	if _, ok := f.entries[id]; !ok {
		return gateway.ErrNotFound
	}
	delete(f.entries, id)
	f.removed = append(f.removed, id)
	return nil
}

func (f *fakeEntries) Sync(context.Context) ([]*model.Entry, error) {
	if f.syncErr != nil {
		return nil, f.syncErr
	}
	f.synced++
	return f.all(), nil
}

// all returns the stored entries in id order for deterministic
// assertions.
func (f *fakeEntries) all() []*model.Entry {
	ids := make([]string, 0, len(f.entries))
	for id := range f.entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*model.Entry, 0, len(ids))
	for _, id := range ids {
		out = append(out, f.entries[id])
	}
	return out
}

// compile-time assertions: the fakes satisfy the consumer interfaces
// (mirrors the assertions on the real services in tui.go).
var (
	_ authClient  = (*fakeAuth)(nil)
	_ entryClient = (*fakeEntries)(nil)
)

// key converts a symbolic key name into a tea.KeyMsg.
func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// send feeds key presses to the model, returning the updated model.
func send(m tea.Model, keys ...string) tea.Model {
	for _, k := range keys {
		m, _ = m.Update(key(k))
	}
	return m
}

// run executes a command and feeds its message back into the model.
func run(m tea.Model, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if cmd == nil {
		return m, nil
	}
	return m.Update(cmd())
}

// load performs the asynchronous initial load: it runs the Init
// command and feeds the result back, after sizing the window.
func load(t *testing.T, m appModel, _ *fakeEntries) appModel {
	t.Helper()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(appModel)
	cmd := m.Init()
	require.NotNil(t, cmd)
	up, _ := run(m, cmd)
	return up.(appModel)
}

// sampleEntries returns two entries (login and card) for the tests.
func sampleEntries(t *testing.T) []*model.Entry {
	t.Helper()
	login, err := render.EncodeLogin("alice", "s3cret")
	require.NoError(t, err)
	card, err := render.EncodeCard("4111111111111111", "Alice", "12/28", "123")
	require.NoError(t, err)
	return []*model.Entry{
		{
			ID: "e1", Type: model.EntryTypeLoginPassword, Label: "github",
			Data: login, Version: 2,
			CreatedAt: time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC),
		},
		{
			ID: "e2", Type: model.EntryTypeCard, Label: "bank",
			Data: card, Version: 1,
			CreatedAt: time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC),
		},
	}
}

func TestRunNotAuthenticated(t *testing.T) {
	err := Run(&fakeAuth{}, newFakeEntries())
	require.ErrorIs(t, err, ErrNotAuthenticated)
}

func TestInitialLoad(t *testing.T) {
	entries := newFakeEntries(sampleEntries(t)...)
	m := load(t, newAppModel(entries), entries)

	require.Len(t, m.all, 2)
	require.Len(t, m.visible, 2)
	require.False(t, m.loading)
	require.False(t, m.statusErr)
	require.Contains(t, m.status, "loaded 2 entries")
	require.Contains(t, m.View(), "github")
	require.Contains(t, m.View(), "bank")
}

func TestLoadErrorShowsStatus(t *testing.T) {
	entries := newFakeEntries()
	entries.syncErr = errors.New("server unreachable")
	m := newAppModel(entries)

	cmd := m.Init()
	updated, _ := run(m, cmd)
	m = updated.(appModel)

	require.True(t, m.statusErr)
	require.Contains(t, m.status, "server unreachable")
	require.False(t, m.quitting)
}

func TestListNavigation(t *testing.T) {
	entries := newFakeEntries(sampleEntries(t)...)
	m := load(t, newAppModel(entries), entries)

	require.Equal(t, 0, m.cursor)
	m = send(m, "down", "j").(appModel)
	require.Equal(t, 1, m.cursor)
	m = send(m, "k", "up").(appModel)
	require.Equal(t, 0, m.cursor)
	// Cursor does not run off the ends.
	m = send(m, "up").(appModel)
	require.Equal(t, 0, m.cursor)
}

func TestEnterOpensDetailAndEscReturns(t *testing.T) {
	entries := newFakeEntries(sampleEntries(t)...)
	m := load(t, newAppModel(entries), entries)

	m = send(m, "enter").(appModel)
	require.Equal(t, screenDetail, m.topScreen())
	require.NotNil(t, m.current)
	require.Equal(t, "e1", m.current.ID)
	view := m.View()
	require.Contains(t, view, "Entry details")
	require.Contains(t, view, "alice")     // decoded login payload
	require.Contains(t, view, "username:") // shared render format

	m = send(m, "esc").(appModel)
	require.Equal(t, screenList, m.topScreen())
}

func TestDetailEditAndDeleteShortcuts(t *testing.T) {
	entries := newFakeEntries(sampleEntries(t)...)
	m := load(t, newAppModel(entries), entries)
	m = send(m, "enter").(appModel)

	m = send(m, "e").(appModel)
	require.Equal(t, screenForm, m.topScreen())
	require.False(t, m.form.isNew)
	// Pre-filled from the entry.
	require.Equal(t, "github", m.form.value(0))
	require.Equal(t, "alice", m.form.value(2))
	m = send(m, "esc").(appModel)
	require.Equal(t, screenDetail, m.topScreen())

	m = send(m, "d").(appModel)
	require.Equal(t, screenConfirm, m.topScreen())
	m = send(m, "n").(appModel)
	require.Equal(t, screenDetail, m.topScreen())
}

func TestFilterByLabel(t *testing.T) {
	entries := newFakeEntries(sampleEntries(t)...)
	m := load(t, newAppModel(entries), entries)

	m = send(m, "/").(appModel)
	require.True(t, m.filtering)

	// Typing goes into the filter input, not the navigation.
	m = send(m, "b", "a", "n").(appModel)
	require.True(t, m.filtering)
	require.Equal(t, 0, m.cursor)

	m = send(m, "enter").(appModel)
	require.False(t, m.filtering)
	require.Len(t, m.visible, 1)
	require.Equal(t, "bank", m.visible[0].Label)
	require.Contains(t, m.View(), "bank")
	require.NotContains(t, m.View(), "github")

	// Esc clears the filter.
	m = send(m, "/").(appModel)
	m = send(m, "esc").(appModel)
	require.False(t, m.filtering)
	require.Empty(t, m.filter)
	require.Len(t, m.visible, 2)
}

func TestQuitFromList(t *testing.T) {
	entries := newFakeEntries(sampleEntries(t)...)
	m := load(t, newAppModel(entries), entries)

	updated, cmd := m.Update(key("q"))
	m = updated.(appModel)
	require.True(t, m.quitting)
	require.NotNil(t, cmd)
}

func TestRefreshKeyReloads(t *testing.T) {
	entries := newFakeEntries(sampleEntries(t)...)
	m := load(t, newAppModel(entries), entries)
	syncedBefore := entries.synced

	updated, cmd := m.Update(key("r"))
	require.NotNil(t, cmd)
	m = updated.(appModel)
	require.True(t, m.loading)

	updated, _ = run(m, cmd)
	m = updated.(appModel)
	require.Equal(t, syncedBefore+1, entries.synced)
	require.False(t, m.loading)
}

func TestNewEntryViaForm(t *testing.T) {
	entries := newFakeEntries(sampleEntries(t)...)
	m := load(t, newAppModel(entries), entries)

	m = send(m, "n").(appModel)
	require.Equal(t, screenForm, m.topScreen())
	require.True(t, m.form.isNew)

	// Cycle the type selector to "text" (login → text).
	m = send(m, "right").(appModel)
	require.Equal(t, model.EntryTypeText, m.form.entryType)

	// Fill label and text (focus: type → label → metadata → text).
	m = send(m, "tab", "m", "y", " ", "n", "o", "t", "e").(appModel)
	m = send(m, "tab").(appModel) // metadata, leave empty
	m = send(m, "tab").(appModel)
	m = send(m, "h", "e", "l", "l", "o").(appModel)

	updated, cmd := m.Update(key("enter"))
	require.NotNil(t, cmd, "save should issue a command")
	m = updated.(appModel)

	// The save result returns to the list and triggers a refresh.
	var up tea.Model
	up, cmd = run(m, cmd)
	m = up.(appModel)

	require.Len(t, entries.added, 1)
	added := entries.added[0]
	require.Equal(t, model.EntryTypeText, added.Type)
	require.Equal(t, "my note", added.Label)
	require.Equal(t, "hello", string(added.Data))

	require.Equal(t, screenList, m.topScreen())
	require.Contains(t, m.status, `saved "my note"`)
	require.NotNil(t, cmd, "save success should refresh the list")

	up, _ = run(m, cmd)
	m = up.(appModel)
	require.Len(t, m.all, 3)
}

func TestFormValidationShowsError(t *testing.T) {
	entries := newFakeEntries(sampleEntries(t)...)
	m := load(t, newAppModel(entries), entries)

	m = send(m, "n").(appModel)
	// Save immediately: label is empty.
	updated, cmd := m.Update(key("ctrl+s"))
	m = updated.(appModel)
	require.Nil(t, cmd, "validation failure must not issue a save")
	require.Empty(t, entries.added)
	require.Contains(t, m.form.err, "label must not be empty")
	require.Contains(t, m.View(), "label must not be empty")
}

func TestEditCarriesVersion(t *testing.T) {
	entries := newFakeEntries(sampleEntries(t)...)
	m := load(t, newAppModel(entries), entries)

	// Open the edit form from the list (cursor on e1).
	m = send(m, "e").(appModel)
	require.Equal(t, screenForm, m.topScreen())

	// Move to the password field, clear it and type a new value
	// (tab ×3: label, metadata, username → password is the last
	// field).
	m = send(m, "tab", "tab", "tab").(appModel)
	bs := make([]string, len("s3cret"))
	for i := range bs {
		bs[i] = "backspace"
	}
	m = send(m, bs...).(appModel)
	m = send(m, "n", "e", "w").(appModel)

	_, cmd := m.Update(key("enter"))
	require.NotNil(t, cmd)

	_, _ = run(m, cmd)

	require.Len(t, entries.edited, 1)
	edited := entries.edited[0]
	require.Equal(t, "e1", edited.ID)
	require.Equal(t, []int64{2}, entries.editVersions,
		"edit must carry the version it is based on")

	login, err := render.DecodeLogin(edited.Data)
	require.NoError(t, err)
	require.Equal(t, "alice", login.Username)
	require.Equal(t, "new", login.Password)
}

func TestSaveErrorStaysInForm(t *testing.T) {
	entries := newFakeEntries(sampleEntries(t)...)
	entries.editErr = gateway.ErrConflict
	m := load(t, newAppModel(entries), entries)

	m = send(m, "e").(appModel)
	updated, cmd := m.Update(key("ctrl+s"))
	require.NotNil(t, cmd)
	m = updated.(appModel)

	up, _ := run(m, cmd)
	m = up.(appModel)
	require.Equal(t, screenForm, m.topScreen(), "a failed save must keep the form open")
	require.True(t, m.statusErr)
	require.Contains(t, m.View(), "entry was modified")
}

func TestDeleteFlow(t *testing.T) {
	entries := newFakeEntries(sampleEntries(t)...)
	m := load(t, newAppModel(entries), entries)

	m = send(m, "d").(appModel)
	require.Equal(t, screenConfirm, m.topScreen())
	require.Contains(t, m.View(), "Delete")
	require.Contains(t, m.View(), "github")

	updated, cmd := m.Update(key("y"))
	require.NotNil(t, cmd)
	m = updated.(appModel)

	up, next := run(m, cmd)
	m = up.(appModel)
	require.Equal(t, []string{"e1"}, entries.removed)
	require.Equal(t, screenList, m.topScreen())
	require.Contains(t, m.status, "deleted")

	// The refresh after deletion drops the entry from the list.
	up, _ = run(m, next)
	m = up.(appModel)
	require.Len(t, m.all, 1)
}

func TestDeleteCancel(t *testing.T) {
	entries := newFakeEntries(sampleEntries(t)...)
	m := load(t, newAppModel(entries), entries)

	m = send(m, "d").(appModel)
	m = send(m, "esc").(appModel)
	require.Equal(t, screenList, m.topScreen())
	require.Empty(t, entries.removed)
	require.Contains(t, m.status, "cancelled")
}

func TestCtrlCQuitsFromEveryScreen(t *testing.T) {
	entries := newFakeEntries(sampleEntries(t)...)
	for _, keys := range [][]string{
		{},              // list
		{"enter"},       // detail
		{"enter", "e"},  // form
		{"enter", "d"},  // confirm
		{"n"},           // new form
		{"/", "b", "a"}, // filter input: ctrl+c must quit, not type
	} {
		m := load(t, newAppModel(entries), entries)
		m = send(m, keys...).(appModel)
		updated, cmd := m.Update(key("ctrl+c"))
		m = updated.(appModel)
		require.True(t, m.quitting, "keys: %v", keys)
		require.NotNil(t, cmd, "keys: %v", keys)
	}
}

func TestListScrollsWithSmallWindow(t *testing.T) {
	many := make([]*model.Entry, 0, 20)
	for i := 0; i < 20; i++ {
		many = append(many, &model.Entry{
			ID:    fmt.Sprintf("e%02d", i),
			Type:  model.EntryTypeText,
			Label: fmt.Sprintf("entry-%02d", i),
			Data:  []byte("x"),
		})
	}
	entries := newFakeEntries(many...)
	m := load(t, newAppModel(entries), entries)
	m.width, m.height = 80, 10 // 7 viewport rows
	m.applyFilter()

	require.Equal(t, 7, m.viewportRows())
	m = send(m, "down", "down", "down", "down", "down", "down", "down").(appModel)
	require.Equal(t, 7, m.cursor)
	require.Equal(t, 1, m.offset, "the cursor must scroll the window")

	view := m.View()
	require.Contains(t, view, "entry-07")
	require.NotContains(t, view, "entry-00")
	require.Contains(t, view, "showing 2–8 of 20")
}

func TestWindowSizeMsgClamps(t *testing.T) {
	entries := newFakeEntries(sampleEntries(t)...)
	m := load(t, newAppModel(entries), entries)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(appModel)
	require.Equal(t, 100, m.width)
	require.Equal(t, 30, m.height)
}

func TestFormTypeCycleRebuildsFields(t *testing.T) {
	f := newForm()
	require.Equal(t, model.EntryTypeLoginPassword, f.entryType)
	require.Len(t, f.fields, 4) // label, metadata, username, password

	// Type a label, then cycle: the label survives the rebuild.
	f.fields[0].input.SetValue("kept")
	f.cycleType(1)
	require.Equal(t, model.EntryTypeText, f.entryType)
	require.Len(t, f.fields, 3) // label, metadata, text
	require.Equal(t, "kept", f.value(0))

	f.cycleType(-1)
	f.cycleType(-1) // login ← binary ← text
	require.Equal(t, model.EntryTypeCard, f.entryType)
	require.Len(t, f.fields, 6)
}

func TestBuildEntryPerType(t *testing.T) {
	loginSrc := newForm() // login type by default
	loginSrc.fields[0].input.SetValue("site")
	loginSrc.fields[2].input.SetValue("u")
	loginSrc.fields[3].input.SetValue("p")
	entry, err := loginSrc.buildEntry()
	require.NoError(t, err)
	require.Equal(t, model.EntryTypeLoginPassword, entry.Type)
	login, err := render.DecodeLogin(entry.Data)
	require.NoError(t, err)
	require.Equal(t, "u", login.Username)

	// Card: number is required.
	card := newForm()
	card.cycleType(3)
	card.fields[0].input.SetValue("bank")
	_, err = card.buildEntry()
	require.Error(t, err)
	require.Contains(t, err.Error(), "card number")

	card.fields[2].input.SetValue("4111111111111111")
	entry, err = card.buildEntry()
	require.NoError(t, err)
	got, err := render.DecodeCard(entry.Data)
	require.NoError(t, err)
	require.Equal(t, "4111111111111111", got.Number)

	// Binary (new): label and path are required.
	bin := newForm()
	bin.cycleType(2)
	_, err = bin.buildEntry()
	require.Error(t, err)
	require.Contains(t, err.Error(), "label must not be empty")

	bin.fields[0].input.SetValue("file")
	_, err = bin.buildEntry()
	require.Error(t, err)
	require.Contains(t, err.Error(), "file path")

	// Binary (edit): an empty path keeps the stored content.
	src := &model.Entry{
		ID: "e1", Type: model.EntryTypeBinary, Label: "file",
		Data: []byte("stored"), Version: 4,
	}
	editBin := newFormForEdit(src)
	entry, err = editBin.buildEntry()
	require.NoError(t, err)
	require.Equal(t, []byte("stored"), entry.Data)
	require.Equal(t, int64(4), entry.Version)
}

func TestViewAlwaysRendersSomething(t *testing.T) {
	entries := newFakeEntries()
	m := newAppModel(entries)
	require.NotEmpty(t, strings.TrimSpace(m.View()))

	m = load(t, m, entries)
	require.Contains(t, m.View(), "no entries yet")
}
