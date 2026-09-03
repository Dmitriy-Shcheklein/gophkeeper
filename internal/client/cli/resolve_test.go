package cli

import (
	"errors"
	"testing"

	"github.com/dmitriy/gophkeeper/internal/client/model"
	"github.com/stretchr/testify/require"
)

var errListFailed = errors.New("list failed")

func seededEntries() *fakeEntries {
	return newFakeEntries(
		&model.Entry{ID: "aaaaaaaa-1111-2222-3333-444444444444", Type: model.EntryTypeText, Label: "a", Version: 1},
		&model.Entry{ID: "aaaaaaab-1111-2222-3333-444444444444", Type: model.EntryTypeText, Label: "b", Version: 1},
		&model.Entry{ID: "cccccccc-1111-2222-3333-444444444444", Type: model.EntryTypeText, Label: "c", Version: 1},
	)
}

func TestResolveEntryIDEmpty(t *testing.T) {
	app, _, _ := newTestApp(&fakeAuth{token: "x"}, seededEntries())

	_, err := app.resolveEntryID(t.Context(), "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "entry id must not be empty")
}

func TestResolveEntryIDExactMatch(t *testing.T) {
	app, _, _ := newTestApp(&fakeAuth{token: "x"}, seededEntries())

	id, err := app.resolveEntryID(t.Context(), "aaaaaaaa-1111-2222-3333-444444444444")
	require.NoError(t, err)
	require.Equal(t, "aaaaaaaa-1111-2222-3333-444444444444", id)
}

func TestResolveEntryIDUniquePrefix(t *testing.T) {
	app, _, _ := newTestApp(&fakeAuth{token: "x"}, seededEntries())

	id, err := app.resolveEntryID(t.Context(), "cccccccc")
	require.NoError(t, err)
	require.Equal(t, "cccccccc-1111-2222-3333-444444444444", id)
}

func TestResolveEntryIDAmbiguousPrefix(t *testing.T) {
	app, _, _ := newTestApp(&fakeAuth{token: "x"}, seededEntries())

	// "aaaaaaaa" and "aaaaaaab" share the 7-char prefix "aaaaaaa".
	_, err := app.resolveEntryID(t.Context(), "aaaaaaa")
	require.Error(t, err)
	require.Contains(t, err.Error(), "ambiguous id")
	require.Contains(t, err.Error(), "use a longer prefix")
}

func TestResolveEntryIDNoMatchPassesThrough(t *testing.T) {
	app, _, _ := newTestApp(&fakeAuth{token: "x"}, seededEntries())

	// An unknown id (e.g. a full id of a deleted entry) is passed to
	// the server, which reports the authoritative not-found error.
	id, err := app.resolveEntryID(t.Context(), "dddddddd-1111-2222-3333-444444444444")
	require.NoError(t, err)
	require.Equal(t, "dddddddd-1111-2222-3333-444444444444", id)
}

func TestResolveEntryIDListFailure(t *testing.T) {
	entries := seededEntries()
	entries.listErr = errListFailed
	app, _, _ := newTestApp(&fakeAuth{token: "x"}, entries)

	_, err := app.resolveEntryID(t.Context(), "cccccccc")
	require.Error(t, err)
	require.ErrorIs(t, err, errListFailed)
}

func TestGetCommandWithShortID(t *testing.T) {
	data, err := encodeLogin("alice", "pw")
	require.NoError(t, err)
	entries := newFakeEntries(&model.Entry{
		ID: "entry-9abcdef", Type: model.EntryTypeLoginPassword, Label: "github", Data: data, Version: 1,
	})
	app, out, _ := newTestApp(&fakeAuth{token: "x"}, entries)

	_, _, err = run(t, app, "get", "--id=entry-9a")
	require.NoError(t, err)
	require.Contains(t, out.String(), "ID:       entry-9abcdef")
	// Get received the full id.
	require.Equal(t, []string{"entry-9abcdef"}, entries.getCalls)
}

func TestDeleteCommandWithShortID(t *testing.T) {
	entries := newFakeEntries(&model.Entry{ID: "entry-1234567890", Type: model.EntryTypeText, Label: "n", Version: 1})
	app, _, _ := newTestApp(&fakeAuth{token: "x"}, entries)

	_, _, err := run(t, app, "delete", "--id=entry-12", "--yes")
	require.NoError(t, err)
	require.Equal(t, []string{"entry-1234567890"}, entries.removed)
}
