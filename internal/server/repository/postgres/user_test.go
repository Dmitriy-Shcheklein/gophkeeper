package postgres_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitriy/gophkeeper/internal/server/model"
)

func TestUserRepository_Create(t *testing.T) {
	resetTables(t)
	users := testStorage.Users()

	user := &model.User{Login: "alice", PassHash: "hash-alice"}
	require.NoError(t, users.Create(t.Context(), user))

	assert.NotEmpty(t, user.ID)
	assertValidUUID(t, user.ID)
	assert.False(t, user.CreatedAt.IsZero())
}

func TestUserRepository_Create_DuplicateLogin(t *testing.T) {
	resetTables(t)
	users := testStorage.Users()

	first := &model.User{Login: "bob", PassHash: "hash-bob"}
	require.NoError(t, users.Create(t.Context(), first))

	duplicate := &model.User{Login: "bob", PassHash: "hash-other"}
	err := users.Create(t.Context(), duplicate)
	require.ErrorIs(t, err, model.ErrAlreadyExists)
}

func TestUserRepository_GetByLogin(t *testing.T) {
	resetTables(t)
	users := testStorage.Users()

	created := &model.User{Login: "carol", PassHash: "hash-carol"}
	require.NoError(t, users.Create(t.Context(), created))

	got, err := users.GetByLogin(t.Context(), "carol")
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, "carol", got.Login)
	assert.Equal(t, "hash-carol", got.PassHash)
	assert.Equal(t, created.CreatedAt, got.CreatedAt)
}

func TestUserRepository_GetByLogin_NotFound(t *testing.T) {
	resetTables(t)

	_, err := testStorage.Users().GetByLogin(t.Context(), "missing")
	require.ErrorIs(t, err, model.ErrNotFound)
}

func TestUserRepository_GetByID(t *testing.T) {
	resetTables(t)
	users := testStorage.Users()

	created := &model.User{Login: "dave", PassHash: "hash-dave"}
	require.NoError(t, users.Create(t.Context(), created))

	got, err := users.GetByID(t.Context(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, "dave", got.Login)
	assert.Equal(t, "hash-dave", got.PassHash)
	assert.Equal(t, created.CreatedAt, got.CreatedAt)
}

func TestUserRepository_GetByID_NotFound(t *testing.T) {
	resetTables(t)

	_, err := testStorage.Users().GetByID(t.Context(), missingUUID(t))
	require.ErrorIs(t, err, model.ErrNotFound)
}
