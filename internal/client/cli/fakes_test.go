package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/dmitriy/gophkeeper/internal/client/gateway"
	"github.com/dmitriy/gophkeeper/internal/client/model"
)

// fakeAuth is an in-memory authClient fake recording calls and
// configurable failures.
type fakeAuth struct {
	token string

	registered [][2]string
	logins     [][2]string
	logouts    int
	restores   int

	registerErr error
	loginErr    error
	logoutErr   error
	restoreErr  error
}

func (f *fakeAuth) Register(_ context.Context, login, password string) error {
	if f.registerErr != nil {
		return f.registerErr
	}
	f.registered = append(f.registered, [2]string{login, password})
	f.token = "token-" + login
	return nil
}

func (f *fakeAuth) Login(_ context.Context, login, password string) error {
	if f.loginErr != nil {
		return f.loginErr
	}
	f.logins = append(f.logins, [2]string{login, password})
	f.token = "token-" + login
	return nil
}

func (f *fakeAuth) Logout() error {
	if f.logoutErr != nil {
		return f.logoutErr
	}
	f.logouts++
	f.token = ""
	return nil
}

func (f *fakeAuth) Restore() error {
	if f.restoreErr != nil {
		return f.restoreErr
	}
	f.restores++
	return nil
}

func (f *fakeAuth) IsAuthenticated() bool {
	return f.token != ""
}

// fakeEntries is an in-memory entryClient fake with optional
// per-method failures.
type fakeEntries struct {
	entries map[string]*model.Entry
	nextID  int

	added    []*model.Entry
	edited   []*model.Entry
	removed  []string
	listed   int
	synced   int
	getCalls []string

	addErr      error
	listErr     error
	getErr      error
	editErr     error
	removeErr   error
	syncErr     error
	uploadErr   error
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

func (f *fakeEntries) List(_ context.Context) ([]*model.Entry, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	f.listed++
	return f.all(), nil
}

func (f *fakeEntries) Get(_ context.Context, id string) (*model.Entry, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	f.getCalls = append(f.getCalls, id)
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
	updated := *entry
	updated.Version = stored.Version + 1
	updated.UpdatedAt = time.Date(2026, 9, 2, 11, 0, 0, 0, time.UTC)
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

func (f *fakeEntries) Sync(_ context.Context) ([]*model.Entry, error) {
	if f.syncErr != nil {
		return nil, f.syncErr
	}
	f.synced++
	return f.all(), nil
}

func (f *fakeEntries) Upload(_ context.Context, entry *model.Entry, expectedVersion int64, r io.Reader) (*model.Entry, error) {
	if f.uploadErr != nil {
		return nil, f.uploadErr
	}
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
	uploaded.CreatedAt = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	uploaded.UpdatedAt = uploaded.CreatedAt
	f.entries[uploaded.ID] = &uploaded
	f.added = append(f.added, &uploaded)
	return &uploaded, nil
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

// newTestApp wires an App with fake services and capture buffers.
func newTestApp(auth *fakeAuth, entries *fakeEntries) (*App, *bytes.Buffer, *bytes.Buffer) {
	out, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	app := &App{
		Version:   "test-version",
		BuildDate: "test-date",
		Commit:    "test-commit",
		In:        strings.NewReader(""),
		Out:       out,
		Err:       errBuf,
		auth:      auth,
		entries:   entries,
	}
	return app, out, errBuf
}

// run executes the CLI with the given args, returning stdout, stderr
// and the error.
func run(t *testing.T, app *App, args ...string) (string, string, error) {
	t.Helper()
	err := Execute(app, args)
	return app.Out.(*bytes.Buffer).String(), app.Err.(*bytes.Buffer).String(), err
}

// compile-time assertions: the fakes satisfy the consumer interfaces
// (mirrors the assertions on the real services in app.go).
var (
	_ authClient  = (*fakeAuth)(nil)
	_ entryClient = (*fakeEntries)(nil)
)
