package postgres_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitriy/gophkeeper/internal/server/model"
)

// createTestUser seeds a user with a unique login and returns it with
// the generated ID filled in.
func createTestUser(t *testing.T, login string) *model.User {
	t.Helper()
	user := &model.User{Login: login, PassHash: "hash-" + login}
	require.NoError(t, testStorage.Users().Create(t.Context(), user))
	return user
}

// createTestEntry seeds an entry owned by the given user and returns it
// with the generated fields filled in.
func createTestEntry(t *testing.T, userID string, entry model.Entry) *model.Entry {
	t.Helper()
	entry.UserID = userID
	require.NoError(t, testStorage.Entries().Create(t.Context(), &entry))
	return &entry
}

func TestEntryRepository_Create(t *testing.T) {
	resetTables(t)
	user := createTestUser(t, "entry-owner")

	entry := &model.Entry{
		UserID:   user.ID,
		Type:     model.EntryTypeLoginPassword,
		Label:    "example.com",
		Metadata: "2FA enabled",
		Data:     []byte("secret-payload"),
	}
	require.NoError(t, testStorage.Entries().Create(t.Context(), entry))

	assert.NotEmpty(t, entry.ID)
	assertValidUUID(t, entry.ID)
	assert.Equal(t, int64(1), entry.Version)
	assert.False(t, entry.CreatedAt.IsZero())
	assert.False(t, entry.UpdatedAt.IsZero())
}

func TestEntryRepository_GetByID(t *testing.T) {
	resetTables(t)
	user := createTestUser(t, "get-owner")

	created := createTestEntry(t, user.ID, model.Entry{
		Type:     model.EntryTypeCard,
		Label:    "my-card",
		Metadata: "meta-value",
		Data:     []byte("card-data"),
	})

	got, err := testStorage.Entries().GetByID(t.Context(), user.ID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, user.ID, got.UserID)
	assert.Equal(t, model.EntryTypeCard, got.Type)
	assert.Equal(t, "my-card", got.Label)
	assert.Equal(t, "meta-value", got.Metadata)
	assert.Equal(t, []byte("card-data"), got.Data)
	assert.Equal(t, int64(1), got.Version)
	assert.Equal(t, created.CreatedAt, got.CreatedAt)
	assert.Equal(t, created.UpdatedAt, got.UpdatedAt)
}

func TestEntryRepository_GetByID_EmptyMetadataCoalesced(t *testing.T) {
	resetTables(t)
	user := createTestUser(t, "meta-owner")

	created := createTestEntry(t, user.ID, model.Entry{
		Type: model.EntryTypeText,
		Data: []byte("note"),
	})

	got, err := testStorage.Entries().GetByID(t.Context(), user.ID, created.ID)
	require.NoError(t, err)
	assert.Empty(t, got.Metadata)
}

func TestEntryRepository_GetByID_UserIsolation(t *testing.T) {
	resetTables(t)
	owner := createTestUser(t, "iso-owner")
	other := createTestUser(t, "iso-other")

	created := createTestEntry(t, owner.ID, model.Entry{
		Type: model.EntryTypeText,
		Data: []byte("private"),
	})

	_, err := testStorage.Entries().GetByID(t.Context(), other.ID, created.ID)
	require.ErrorIs(t, err, model.ErrNotFound)
}

func TestEntryRepository_GetByID_NotFound(t *testing.T) {
	resetTables(t)
	user := createTestUser(t, "nf-owner")

	_, err := testStorage.Entries().GetByID(t.Context(), user.ID, missingUUID(t))
	require.ErrorIs(t, err, model.ErrNotFound)
}

func TestEntryRepository_List(t *testing.T) {
	resetTables(t)
	user := createTestUser(t, "list-owner")
	stranger := createTestUser(t, "list-stranger")

	entries := testStorage.Entries()

	pw := createTestEntry(t, user.ID, model.Entry{
		Type: model.EntryTypeLoginPassword, Label: "pw", Data: []byte("d1"),
	})
	note := createTestEntry(t, user.ID, model.Entry{
		Type: model.EntryTypeText, Label: "note", Data: []byte("d2"),
	})
	card := createTestEntry(t, user.ID, model.Entry{
		Type: model.EntryTypeCard, Label: "card", Data: []byte("d3"),
	})
	// Belongs to another user and must never appear in the lists below.
	createTestEntry(t, stranger.ID, model.Entry{
		Type: model.EntryTypeLoginPassword, Label: "foreign", Data: []byte("d4"),
	})

	// Without a type filter: all of the user's entries in creation order.
	all, err := entries.List(t.Context(), user.ID, nil, true)
	require.NoError(t, err)
	require.Len(t, all, 3)
	assert.Equal(t, []string{pw.ID, note.ID, card.ID},
		[]string{all[0].ID, all[1].ID, all[2].ID})

	// With a type filter: only entries of the requested type.
	textType := model.EntryTypeText
	filtered, err := entries.List(t.Context(), user.ID, &textType, true)
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	assert.Equal(t, note.ID, filtered[0].ID)

	// Another user sees none of the owner's entries.
	foreign, err := entries.List(t.Context(), stranger.ID, nil, true)
	require.NoError(t, err)
	require.Len(t, foreign, 1)
	assert.Equal(t, "foreign", foreign[0].Label)

	// Unknown user has an empty list, not an error.
	none, err := entries.List(t.Context(), missingUUID(t), nil, true)
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestEntryRepository_Update_HappyPath(t *testing.T) {
	resetTables(t)
	user := createTestUser(t, "upd-owner")
	entries := testStorage.Entries()

	created := createTestEntry(t, user.ID, model.Entry{
		Type: model.EntryTypeLoginPassword, Label: "old-label", Data: []byte("old-data"),
	})

	updated := &model.Entry{
		ID:       created.ID,
		UserID:   user.ID,
		Label:    "new-label",
		Metadata: "new-meta",
		Data:     []byte("new-data"),
		Version:  created.Version,
	}
	require.NoError(t, entries.Update(t.Context(), updated))

	assert.Equal(t, created.Version+1, updated.Version)
	assert.True(t, !updated.UpdatedAt.Before(created.UpdatedAt),
		"updated_at must not go backwards")

	got, err := entries.GetByID(t.Context(), user.ID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "new-label", got.Label)
	assert.Equal(t, "new-meta", got.Metadata)
	assert.Equal(t, []byte("new-data"), got.Data)
	assert.Equal(t, updated.Version, got.Version)
}

func TestEntryRepository_Update_Conflict(t *testing.T) {
	resetTables(t)
	user := createTestUser(t, "conflict-owner")
	entries := testStorage.Entries()

	created := createTestEntry(t, user.ID, model.Entry{
		Type: model.EntryTypeText, Label: "label", Data: []byte("data"),
	})

	// Simulate a stale copy by bumping the stored version once.
	fresh := &model.Entry{
		ID: created.ID, UserID: user.ID, Label: "v2", Data: []byte("data"),
		Version: created.Version,
	}
	require.NoError(t, entries.Update(t.Context(), fresh))

	stale := &model.Entry{
		ID: created.ID, UserID: user.ID, Label: "stale", Data: []byte("data"),
		Version: created.Version,
	}
	err := entries.Update(t.Context(), stale)
	require.ErrorIs(t, err, model.ErrConflict)
}

func TestEntryRepository_Update_Missing(t *testing.T) {
	resetTables(t)
	user := createTestUser(t, "missing-owner")

	entry := &model.Entry{
		ID: missingUUID(t), UserID: user.ID, Label: "x", Data: []byte("x"), Version: 1,
	}
	err := testStorage.Entries().Update(t.Context(), entry)
	require.ErrorIs(t, err, model.ErrNotFound)
}

func TestEntryRepository_Delete(t *testing.T) {
	resetTables(t)
	user := createTestUser(t, "del-owner")
	entries := testStorage.Entries()

	created := createTestEntry(t, user.ID, model.Entry{
		Type: model.EntryTypeBinary, Label: "blob", Data: []byte("blob"),
	})

	require.NoError(t, entries.Delete(t.Context(), user.ID, created.ID))

	_, err := entries.GetByID(t.Context(), user.ID, created.ID)
	require.ErrorIs(t, err, model.ErrNotFound)

	// Deleting an already-deleted entry is reported as not found.
	err = entries.Delete(t.Context(), user.ID, created.ID)
	require.ErrorIs(t, err, model.ErrNotFound)
}

func TestEntryRepository_Delete_UserIsolation(t *testing.T) {
	resetTables(t)
	owner := createTestUser(t, "del-iso-owner")
	other := createTestUser(t, "del-iso-other")

	created := createTestEntry(t, owner.ID, model.Entry{
		Type: model.EntryTypeText, Label: "private", Data: []byte("private"),
	})

	err := testStorage.Entries().Delete(t.Context(), other.ID, created.ID)
	require.ErrorIs(t, err, model.ErrNotFound)
}
