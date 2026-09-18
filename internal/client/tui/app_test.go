package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

	registered  [][2]string
	loggedIn    [][2]string
	registerErr error
	loginErr    error
}

func (f *fakeAuth) Restore() error        { return nil }
func (f *fakeAuth) IsAuthenticated() bool { return f.token != "" }

func (f *fakeAuth) Register(_ context.Context, login, password string) error {
	if f.registerErr != nil {
		return f.registerErr
	}
	f.registered = append(f.registered, [2]string{login, password})
	f.token = "new-token"
	return nil
}

func (f *fakeAuth) Login(_ context.Context, login, password string) error {
	if f.loginErr != nil {
		return f.loginErr
	}
	f.loggedIn = append(f.loggedIn, [2]string{login, password})
	f.token = "new-token"
	return nil
}

// authed returns an authenticated fakeAuth for tests that start on
// the entry list.
func authed() *fakeAuth {
	return &fakeAuth{token: "test-token"}
}

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

	addErr      error
	editErr     error
	removeErr   error
	syncErr     error
	downloadErr error

	downloaded []string
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

func (f *fakeEntries) Download(_ context.Context, id string, w io.Writer) error {
	if f.downloadErr != nil {
		return f.downloadErr
	}
	entry, ok := f.entries[id]
	if !ok {
		return gateway.ErrNotFound
	}
	payload := entry.Data
	if len(payload) == 0 && entry.DataSize > 0 {
		payload = make([]byte, entry.DataSize)
	}
	_, err := w.Write(payload)
	f.downloaded = append(f.downloaded, id)
	return err
}

func (f *fakeEntries) Upload(_ context.Context, entry *model.Entry, expectedVersion int64, r io.Reader) (*model.Entry, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	uploaded := *entry
	uploaded.Data = data
	uploaded.DataSize = int64(len(data))
	if expectedVersion > 0 {
		stored, ok := f.entries[entry.ID]
		if !ok {
			return nil, gateway.ErrNotFound
		}
		uploaded.Version = stored.Version + 1
		uploaded.CreatedAt = stored.CreatedAt
		f.entries[entry.ID] = &uploaded
		f.edited = append(f.edited, &uploaded)
		return &uploaded, nil
	}
	uploaded.ID = fmt.Sprintf("entry-%03d", f.nextID)
	f.nextID++
	uploaded.Version = 1
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	uploaded.CreatedAt = now
	uploaded.UpdatedAt = now
	f.entries[uploaded.ID] = &uploaded
	f.added = append(f.added, &uploaded)
	return &uploaded, nil
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
// Batch commands are flattened: every sub-command runs and its
// message is fed back too.
func run(m tea.Model, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if cmd == nil {
		return m, nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			m, _ = run(m, c)
		}
		return m, nil
	}
	return m.Update(msg)
}

// load performs the asynchronous initial load: it runs the Init
// command and feeds the result back, after sizing the window.
func load(t *testing.T, m appModel, _ *fakeEntries) appModel {
	t.Helper()
	_ = muteSyncTick(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(appModel)
	cmd := m.Init()
	require.NotNil(t, cmd)
	// Init returns a batch (initial load + the auto-sync tick): run
	// every command in it.
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			up, _ := run(m, c)
			m = up.(appModel)
		}
		return m
	}
	up, _ := run(m, cmd)
	return up.(appModel)
}

// mutedTickMsg is an inert stand-in for the real timer tick: the
// model ignores unknown messages, so feeding it back is a no-op.
type mutedTickMsg struct{}

// syncTickState guards the muted auto-sync timer: muteSyncTick is
// idempotent (load calls it again), so all nested mutes share one
// counter.
var syncTickState struct {
	armed    int
	previous func() tea.Cmd
	muted    bool
}

// muteSyncTick replaces the wall-clock auto-sync timer with an inert,
// counting stub (the real tea.Tick would block the synchronous test
// helpers). The returned function reports how many times a tick was
// armed.
func muteSyncTick(t *testing.T) func() int {
	t.Helper()
	if !syncTickState.muted {
		syncTickState.previous = scheduleSync
		syncTickState.muted = true
		scheduleSync = func() tea.Cmd {
			syncTickState.armed++
			return func() tea.Msg { return mutedTickMsg{} }
		}
		t.Cleanup(func() {
			scheduleSync = syncTickState.previous
			syncTickState.muted = false
			syncTickState.armed = 0
		})
	}
	return func() int { return syncTickState.armed }
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
			Metadata: "alice@corp.example", Data: login, Version: 2,
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
	// Without a token the model starts on the auth screen instead
	// of failing.
	m := newAppModel(&fakeAuth{}, newFakeEntries())
	require.Equal(t, screenAuth, m.topScreen())
	require.False(t, m.authSvc.IsAuthenticated())
	require.Contains(t, m.View(), "Login to GophKeeper")
}

func TestInitialLoad(t *testing.T) {
	entries := newFakeEntries(sampleEntries(t)...)
	m := load(t, newAppModel(authed(), entries), entries)

	require.Len(t, m.all, 2)
	require.Len(t, m.visible, 2)
	require.False(t, m.loading)
	require.False(t, m.statusErr)
	require.Contains(t, m.status, "loaded 2 entries")
	require.Contains(t, m.View(), "github")
	require.Contains(t, m.View(), "bank")
}

func TestLoadErrorShowsStatus(t *testing.T) {
	muteSyncTick(t)
	entries := newFakeEntries()
	entries.syncErr = errors.New("server unreachable")
	m := newAppModel(authed(), entries)

	cmd := m.Init()
	updated, _ := run(m, cmd)
	m = updated.(appModel)

	require.True(t, m.statusErr)
	require.Contains(t, m.status, "server unreachable")
	require.False(t, m.quitting)
}

func TestListNavigation(t *testing.T) {
	entries := newFakeEntries(sampleEntries(t)...)
	m := load(t, newAppModel(authed(), entries), entries)

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
	m := load(t, newAppModel(authed(), entries), entries)

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
	m := load(t, newAppModel(authed(), entries), entries)
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
	m := load(t, newAppModel(authed(), entries), entries)

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

// TestFilterByMetadata verifies the filter matches entry metadata:
// users often put searchable info (site address, bank name, account
// name) there, so a hit must be found even when the word is absent
// from the label.
func TestFilterByMetadata(t *testing.T) {
	entries := newFakeEntries(sampleEntries(t)...)
	m := load(t, newAppModel(authed(), entries), entries)

	m = send(m, "/").(appModel)
	// The word exists only in the metadata of the github entry.
	m = send(m, "c", "o", "r", "p").(appModel)
	m = send(m, "enter").(appModel)
	require.False(t, m.filtering)
	require.Len(t, m.visible, 1)
	require.Equal(t, "github", m.visible[0].Label)
	require.Contains(t, m.View(), "github")
	require.NotContains(t, m.View(), "bank")

	// A word that matches no metadata and no label leaves the list
	// empty.
	m = send(m, "/").(appModel)
	m = send(m, "z", "z", "z").(appModel)
	m = send(m, "enter").(appModel)
	require.Empty(t, m.visible)
}

func TestQuitFromList(t *testing.T) {
	entries := newFakeEntries(sampleEntries(t)...)
	m := load(t, newAppModel(authed(), entries), entries)

	updated, cmd := m.Update(key("q"))
	m = updated.(appModel)
	require.True(t, m.quitting)
	require.NotNil(t, cmd)
}

func TestRefreshKeyReloads(t *testing.T) {
	entries := newFakeEntries(sampleEntries(t)...)
	m := load(t, newAppModel(authed(), entries), entries)
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
	m := load(t, newAppModel(authed(), entries), entries)

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
	m := load(t, newAppModel(authed(), entries), entries)

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
	m := load(t, newAppModel(authed(), entries), entries)

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

func TestSaveFromDetailRefreshesDetail(t *testing.T) {
	entries := newFakeEntries(sampleEntries(t)...)
	m := load(t, newAppModel(authed(), entries), entries)

	// Open the edit form from the detail screen.
	m = send(m, "enter", "e").(appModel)
	require.Equal(t, screenForm, m.topScreen())
	require.Equal(t, []screen{screenDetail, screenForm}, m.stack)

	// Change the label and save.
	m = send(m, "backspace", "backspace", "backspace",
		"backspace", "backspace", "backspace").(appModel)
	m = send(m, "g", "i", "t", "h", "u", "b", "2").(appModel)
	_, cmd := m.Update(key("ctrl+s"))
	require.NotNil(t, cmd)

	up, next := run(m, cmd)
	m = up.(appModel)

	// The form closed onto the detail screen with the server's
	// copy: the new label and the bumped version, not the stale
	// pre-edit entry.
	require.Equal(t, screenDetail, m.topScreen())
	require.Same(t, entries.edited[0], m.current)
	require.Equal(t, "github2", m.current.Label)
	require.Equal(t, int64(3), m.current.Version)
	require.Contains(t, m.View(), "github2")
	require.Contains(t, m.View(), "Version:  3")

	// Re-editing submits the NEW version: no guaranteed conflict.
	m = send(m, "e").(appModel)
	require.Equal(t, screenForm, m.topScreen())
	_, cmd = m.Update(key("ctrl+s"))
	require.NotNil(t, cmd)
	_, _ = run(m, cmd)
	require.Equal(t, []int64{2, 3}, entries.editVersions,
		"the second edit must carry the refreshed version")

	// The refresh triggered by the save also updated the list.
	up, _ = run(m, next)
	m = up.(appModel)
	require.Equal(t, "github2", m.all[0].Label)
}

func TestDeleteFromDetailLandsOnList(t *testing.T) {
	entries := newFakeEntries(sampleEntries(t)...)
	m := load(t, newAppModel(authed(), entries), entries)

	m = send(m, "enter", "d").(appModel)
	require.Equal(t, screenConfirm, m.topScreen())

	_, cmd := m.Update(key("y"))
	require.NotNil(t, cmd)
	up, next := run(m, cmd)
	m = up.(appModel)

	require.Equal(t, screenList, m.topScreen(), "deleting from detail must land on the list")
	require.Nil(t, m.current)
	require.Contains(t, m.status, "deleted")

	// The refresh drops the entry from the list.
	up, _ = run(m, next)
	m = up.(appModel)
	require.Len(t, m.all, 1)
	require.Equal(t, "e2", m.all[0].ID)
}

func TestFormMasksSecretInputs(t *testing.T) {
	entries := newFakeEntries(sampleEntries(t)...)
	m := load(t, newAppModel(authed(), entries), entries)

	// Login form: the password input is masked.
	m = send(m, "e").(appModel)
	form := m.View()
	require.Contains(t, form, "•••")
	require.NotContains(t, form, "s3cret")
	m = send(m, "esc").(appModel)

	// Card form: the CVV input is masked, the number is not.
	m = send(m, "down", "e").(appModel)
	form = m.View()
	require.Contains(t, form, "•••")
	require.NotContains(t, form, "123")
	require.Contains(t, form, "4111111111111111")
}

func TestSaveErrorStaysInForm(t *testing.T) {
	entries := newFakeEntries(sampleEntries(t)...)
	entries.editErr = gateway.ErrConflict
	m := load(t, newAppModel(authed(), entries), entries)

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
	m := load(t, newAppModel(authed(), entries), entries)

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
	m := load(t, newAppModel(authed(), entries), entries)

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
		m := load(t, newAppModel(authed(), entries), entries)
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
	m := load(t, newAppModel(authed(), entries), entries)
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
	m := load(t, newAppModel(authed(), entries), entries)

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
	entry, _, err := loginSrc.buildEntry()
	require.NoError(t, err)
	require.Equal(t, model.EntryTypeLoginPassword, entry.Type)
	login, err := render.DecodeLogin(entry.Data)
	require.NoError(t, err)
	require.Equal(t, "u", login.Username)

	// Card: number is required.
	card := newForm()
	card.cycleType(3)
	card.fields[0].input.SetValue("bank")
	_, _, err = card.buildEntry()
	require.Error(t, err)
	require.Contains(t, err.Error(), "card number")

	card.fields[2].input.SetValue("4111111111111111")
	entry, _, err = card.buildEntry()
	require.NoError(t, err)
	got, err := render.DecodeCard(entry.Data)
	require.NoError(t, err)
	require.Equal(t, "4111111111111111", got.Number)

	// Binary (new): label and path are required.
	bin := newForm()
	bin.cycleType(2)
	_, _, err = bin.buildEntry()
	require.Error(t, err)
	require.Contains(t, err.Error(), "label must not be empty")

	bin.fields[0].input.SetValue("file")
	_, _, err = bin.buildEntry()
	require.Error(t, err)
	require.Contains(t, err.Error(), "file path")

	// Binary (edit): an empty path keeps the stored content.
	src := &model.Entry{
		ID: "e1", Type: model.EntryTypeBinary, Label: "file",
		Data: []byte("stored"), Version: 4,
	}
	editBin := newFormForEdit(src)
	entry, _, err = editBin.buildEntry()
	require.NoError(t, err)
	require.Equal(t, []byte("stored"), entry.Data)
	require.Equal(t, int64(4), entry.Version)
}

func TestViewAlwaysRendersSomething(t *testing.T) {
	entries := newFakeEntries()
	m := newAppModel(authed(), entries)
	require.NotEmpty(t, strings.TrimSpace(m.View()))

	m = load(t, m, entries)
	require.Contains(t, m.View(), "no entries yet")
}

func TestAuthScreenStartsInLoginMode(t *testing.T) {
	m := newAppModel(&fakeAuth{}, newFakeEntries())

	require.Equal(t, screenAuth, m.topScreen())
	view := m.View()
	require.Contains(t, view, "Login to GophKeeper")
	require.Contains(t, view, "login")
	// Password input is masked: the view must not leak the typed value.
	m = send(m, "l", "o", "g", "i", "n").(appModel)
	require.Contains(t, m.View(), "login")
	require.NotContains(t, m.View(), "••")
}

func TestAuthSubmitCallsLogin(t *testing.T) {
	auth := &fakeAuth{}
	m := newAppModel(auth, newFakeEntries())

	m = send(m, "u", "s", "e", "r").(appModel)        // login field
	m = send(m, "tab", "p", "a", "s", "s").(appModel) // password field
	updated, cmd := m.tryAuth()
	m = updated.(appModel)
	up, _ := run(m, cmd)
	m = up.(appModel)

	// Success: auth screen is replaced by the list, entries load.
	require.Equal(t, screenList, m.topScreen())
	require.Len(t, auth.loggedIn, 1)
	require.Equal(t, "user", auth.loggedIn[0][0])
	require.Equal(t, "pass", auth.loggedIn[0][1])
	require.Contains(t, m.status, "logged in")
}

func TestAuthRegisterMode(t *testing.T) {
	auth := &fakeAuth{}
	m := newAppModel(auth, newFakeEntries())

	// Focus the mode selector and switch to register.
	m = send(m, "shift+tab").(appModel)
	require.Equal(t, 0, m.auth.focus)
	m = send(m, "right").(appModel)
	require.True(t, m.auth.register)
	require.Contains(t, m.View(), "Register a new account")

	m = send(m, "n", "e", "w").(appModel)
	m = send(m, "tab", "s", "e", "c", "r", "e", "t").(appModel)
	updated, cmd := m.tryAuth()
	up, _ := run(updated.(appModel), cmd)
	m = up.(appModel)

	require.Len(t, auth.registered, 1)
	require.Equal(t, "new", auth.registered[0][0])
	require.Equal(t, "secret", auth.registered[0][1])
	require.Equal(t, screenList, m.topScreen())
}

func TestAuthValidationErrorStaysOnScreen(t *testing.T) {
	auth := &fakeAuth{}
	m := newAppModel(auth, newFakeEntries())

	// Empty login → in-screen error, no submit.
	updated, cmd := m.tryAuth()
	m = updated.(appModel)
	require.Nil(t, cmd)
	require.Equal(t, screenAuth, m.topScreen())
	require.Contains(t, m.View(), "login must not be empty")
	require.Empty(t, auth.loggedIn)

	// Empty password → same.
	m = send(m, "u", "s", "e", "r").(appModel)
	updated, cmd = m.tryAuth()
	m = updated.(appModel)
	require.Nil(t, cmd)
	require.Contains(t, m.View(), "password must not be empty")
	require.Empty(t, auth.loggedIn)
}

func TestAuthLoginErrorStaysOnScreen(t *testing.T) {
	auth := &fakeAuth{loginErr: errors.New("invalid login or password")}
	m := newAppModel(auth, newFakeEntries())

	m = send(m, "u", "s", "e", "r").(appModel)
	m = send(m, "tab", "p", "w").(appModel)
	updated, cmd := m.tryAuth()
	up, _ := run(updated.(appModel), cmd)
	m = up.(appModel)

	require.Equal(t, screenAuth, m.topScreen())
	require.Contains(t, m.View(), "invalid login or password")
}

func TestAuthEscQuits(t *testing.T) {
	m := newAppModel(&fakeAuth{}, newFakeEntries())
	m = send(m, "esc").(appModel)
	require.True(t, m.quitting)
}

func TestSaveToFileScreenFlow(t *testing.T) {
	origDir, err := os.Getwd()
	require.NoError(t, err)
	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	entries := newFakeEntries(&model.Entry{
		ID: "e-bin", Type: model.EntryTypeBinary, Label: "movie.bin",
		Data: []byte("payload"), DataSize: 7, Version: 1,
	})
	m := load(t, newAppModel(authed(), entries), entries)

	// Open the detail of the binary entry, then press s.
	m = send(m, "enter").(appModel)
	require.Equal(t, screenDetail, m.topScreen())
	require.Contains(t, m.View(), "s save to file")

	m = send(m, "s").(appModel)
	require.Equal(t, screenSave, m.topScreen())
	require.Contains(t, m.View(), "Save binary payload")

	// Empty path is rejected in-screen.
	m = send(m, "enter").(appModel)
	require.Equal(t, screenSave, m.topScreen())
	require.Contains(t, m.View(), "path must not be empty")

	// A path starts the streaming download; success pops back to the
	// detail screen with a status message.
	m = send(m, "o", "u", "t", ".", "b", "i", "n").(appModel)
	updated, cmd := m.updateSave(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(appModel)
	require.NotNil(t, cmd)
	up, _ := run(m, cmd)
	m = up.(appModel)
	require.Equal(t, screenDetail, m.topScreen())
	require.Contains(t, m.status, "saved to out.bin")
	require.Equal(t, []string{"e-bin"}, entries.downloaded)
}

func TestSaveToFileScreenOnlyForBinary(t *testing.T) {
	entries := newFakeEntries(&model.Entry{
		ID: "e-txt", Type: model.EntryTypeText, Label: "note",
		Data: []byte("hello"), Version: 1,
	})
	m := load(t, newAppModel(authed(), entries), entries)

	m = send(m, "enter").(appModel)
	require.Equal(t, screenDetail, m.topScreen())
	// No save action for text entries.
	require.NotContains(t, m.View(), "s save to file")
	m = send(m, "s").(appModel)
	require.Equal(t, screenDetail, m.topScreen())
}

func TestListShowsBinarySizeWithoutPayload(t *testing.T) {
	entries := newFakeEntries(&model.Entry{
		ID: "e1", Type: model.EntryTypeBinary, Label: "big.bin",
		DataSize: 1536, Version: 1, // no Data: synced without payload
	})
	m := load(t, newAppModel(authed(), entries), entries)

	view := m.View()
	require.Contains(t, view, "big.bin")
	require.Contains(t, view, "1.5 KB")
}

func TestFormBinaryWithFilePathStreamsUpload(t *testing.T) {
	// Editing a binary entry with a new path must go through the
	// streaming upload (expectedVersion = the source version), not
	// read the file into memory.
	payload := "small-file-content"
	path := filepath.Join(t.TempDir(), "new.bin")
	require.NoError(t, os.WriteFile(path, []byte(payload), 0o600))

	src := &model.Entry{
		ID: "e-bin", Type: model.EntryTypeBinary, Label: "old.bin",
		Data: []byte("old-content"), DataSize: 11, Version: 3,
	}
	entries := newFakeEntries(src)
	m := load(t, newAppModel(authed(), entries), entries)
	m = send(m, "enter").(appModel) // detail
	m = send(m, "e").(appModel)     // edit form
	require.Equal(t, screenForm, m.topScreen())

	// Fill the file path field (label and metadata are pre-filled).
	m.form.fields[2].input.SetValue(path)
	updated, cmd := m.trySave()
	m = updated.(appModel)
	require.NotNil(t, cmd)
	up, _ := run(m, cmd)
	m = up.(appModel)

	// The upload succeeded: form closed, entry stored with the new
	// content and a bumped version.
	require.Equal(t, screenDetail, m.topScreen())
	stored := entries.entries["e-bin"]
	require.Equal(t, []byte(payload), stored.Data)
	require.Equal(t, int64(len(payload)), stored.DataSize)
	require.Equal(t, int64(4), stored.Version)
}

func TestSyncTickReloadsEntriesInBackground(t *testing.T) {
	armed := muteSyncTick(t)
	entries := newFakeEntries(&model.Entry{
		ID: "e1", Type: model.EntryTypeText, Label: "note", Data: []byte("x"), Version: 1,
	})
	m := load(t, newAppModel(authed(), entries), entries)
	syncedAfterLoad := entries.synced

	// The tick triggers a background reload plus the next tick.
	updated, cmd := m.Update(syncTickMsg{})
	m = updated.(appModel)
	require.NotNil(t, cmd)
	require.True(t, m.loading)
	require.Equal(t, 2, armed(), "handling the tick re-arms it")

	// Running the batch performs the background sync.
	up, _ := run(m, cmd)
	m = up.(appModel)
	require.False(t, m.loading)
	require.Equal(t, syncedAfterLoad+1, entries.synced)
	require.Contains(t, m.status, "auto-synced")

	// The loaded message is marked as background.
	updated, cmd = m.Update(syncTickMsg{})
	m = updated.(appModel)
	msg := msgFromCmd(t, cmd)
	require.True(t, msg.background)
	require.NoError(t, msg.err)
}

// msgFromCmd extracts the first entriesLoadedMsg produced by cmd (the
// batch may carry the tick and the sync in any order).
func msgFromCmd(t *testing.T, cmd tea.Cmd) entriesLoadedMsg {
	t.Helper()
	for i := 0; i < 3; i++ {
		msg := cmd()
		if loaded, ok := msg.(entriesLoadedMsg); ok {
			return loaded
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				if loaded, ok := c().(entriesLoadedMsg); ok {
					return loaded
				}
			}
			t.Fatalf("no entriesLoadedMsg inside batch of %d commands", len(batch))
		}
	}
	t.Fatal("no entriesLoadedMsg in cmd output")
	return entriesLoadedMsg{}
}

func TestSyncTickSkipsWhileLoading(t *testing.T) {
	armed := muteSyncTick(t)
	entries := newFakeEntries()
	m := load(t, newAppModel(authed(), entries), entries)
	m.loading = true

	updated, cmd := m.Update(syncTickMsg{})
	m = updated.(appModel)
	// No reload started, but the tick is re-armed.
	require.Equal(t, 1, entries.synced)
	require.NotNil(t, cmd)
	require.Equal(t, 2, armed())
}

func TestSyncTickIdleOnAuthScreen(t *testing.T) {
	m := newAppModel(&fakeAuth{}, newFakeEntries())
	require.Equal(t, screenAuth, m.topScreen())

	updated, cmd := m.Update(syncTickMsg{})
	_ = updated.(appModel)
	require.Nil(t, cmd, "no sync and no re-arm on the auth screen")
}

func TestBackgroundSyncFailureKeepsEntriesAndReArms(t *testing.T) {
	armed := muteSyncTick(t)
	entries := newFakeEntries(&model.Entry{
		ID: "e1", Type: model.EntryTypeText, Label: "note", Data: []byte("x"), Version: 1,
	})
	m := load(t, newAppModel(authed(), entries), entries)
	entries.syncErr = errors.New("server unreachable")

	updated, cmd := m.Update(syncTickMsg{})
	m = updated.(appModel)
	up, _ := run(m, cmd)
	m = up.(appModel)

	// The old set is kept, the error is reported as auto-sync failure
	// and the tick is re-armed for a retry.
	require.Len(t, m.all, 1)
	require.True(t, m.statusErr)
	require.Contains(t, m.status, "auto-sync")
	require.Equal(t, 3, armed(), "a failed auto-sync re-arms the tick for a retry")
}

func TestAuthedRestartsAutoSync(t *testing.T) {
	muteSyncTick(t)
	entries := newFakeEntries()
	m := newAppModel(authed(), entries)
	// Simulate being on the auth screen and logging in successfully.
	m.auth = newAuthModel()
	m.pushScreen(screenAuth)
	m.stack = nil

	armed := muteSyncTick(t)
	updated, cmd := m.Update(authedMsg{})
	m = updated.(appModel)
	require.NotNil(t, cmd, "login must arm the initial load and the sync tick")
	require.Equal(t, 1, armed())
}
