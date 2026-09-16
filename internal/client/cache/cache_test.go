package cache_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitriy/gophkeeper/internal/client/cache"
	"github.com/dmitriy/gophkeeper/internal/client/model"
)

func sampleEntries() []*model.Entry {
	now := time.Now().UTC().Truncate(time.Second)
	return []*model.Entry{
		{
			ID: "e1", Type: model.EntryTypeLoginPassword, Label: "github", Metadata: "work",
			DataSize: 25, Version: 3, CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "e2", Type: model.EntryTypeBinary, Label: "report.pdf",
			DataSize: 1024, Version: 1, CreatedAt: now, UpdatedAt: now,
		},
	}
}

func newStore(t *testing.T) (*cache.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cache.json")
	s, err := cache.New(path)
	require.NoError(t, err)
	return s, path
}

func TestReplaceRoundTrip(t *testing.T) {
	s, path := newStore(t)

	require.NoError(t, s.Replace(sampleEntries()))

	// A fresh store over the same files must decrypt the snapshot.
	reopened, err := cache.New(path)
	require.NoError(t, err)
	got := reopened.All()
	require.Len(t, got, 2)
	assert.Equal(t, "e1", got[0].ID)
	assert.Equal(t, model.EntryTypeLoginPassword, got[0].Type)
	assert.Equal(t, "github", got[0].Label)
	assert.Equal(t, "work", got[0].Metadata)
	assert.Equal(t, int64(25), got[0].DataSize)
	assert.Equal(t, int64(3), got[0].Version)
	assert.True(t, got[0].UpdatedAt.Equal(sampleEntries()[0].UpdatedAt))
}

// TestCacheNeverStoresPayloads pins the metadata-only contract: the
// on-disk snapshot must not contain the secret payload bytes.
func TestCacheNeverStoresPayloads(t *testing.T) {
	s, path := newStore(t)

	secret := []byte("top-secret-password")
	require.NoError(t, s.Upsert(&model.Entry{
		ID: "e1", Type: model.EntryTypeText, Label: "note", Data: secret, DataSize: int64(len(secret)),
	}))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "top-secret-password")

	got, err := s.Get("e1")
	require.NoError(t, err)
	assert.Empty(t, got.Data)
	assert.Equal(t, int64(len(secret)), got.DataSize, "payload size is still metadata")
}

func TestUpsertGetDelete(t *testing.T) {
	s, _ := newStore(t)

	require.NoError(t, s.Upsert(&model.Entry{ID: "e1", Type: model.EntryTypeText, Label: "v1", DataSize: 2}))
	require.NoError(t, s.Upsert(&model.Entry{ID: "e1", Type: model.EntryTypeText, Label: "v2", DataSize: 4}))
	require.NoError(t, s.Upsert(&model.Entry{ID: "e2", Type: model.EntryTypeCard, Label: "visa", DataSize: 9}))

	got, err := s.Get("e1")
	require.NoError(t, err)
	assert.Equal(t, "v2", got.Label, "upsert must replace by id")
	assert.Len(t, s.All(), 2)

	require.NoError(t, s.Delete("e1"))
	assert.Len(t, s.All(), 1)
	_, err = s.Get("e1")
	assert.ErrorIs(t, err, cache.ErrNotFound)

	// Deleting an unknown id is not an error.
	require.NoError(t, s.Delete("missing"))
}

func TestGetEmptyID(t *testing.T) {
	s, _ := newStore(t)
	_, err := s.Get("")
	assert.ErrorIs(t, err, cache.ErrNotFound)
}

func TestMissingFileIsEmptyCache(t *testing.T) {
	s, _ := newStore(t)
	assert.Empty(t, s.All())
	got, err := s.Get("e1")
	assert.Nil(t, got)
	assert.ErrorIs(t, err, cache.ErrNotFound)
}

func TestClearRemovesFileAndMemory(t *testing.T) {
	s, path := newStore(t)
	require.NoError(t, s.Replace(sampleEntries()))
	require.NoError(t, s.Clear())
	assert.Empty(t, s.All())
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "cache file must be removed")
	// Idempotent.
	require.NoError(t, s.Clear())
}

func TestCorruptFileIsUnreadable(t *testing.T) {
	s, path := newStore(t)
	require.NoError(t, s.Replace(sampleEntries()))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw[:len(raw)/2], 0o600)) // truncated

	reopened, err := cache.New(path)
	require.ErrorIs(t, err, cache.ErrCacheUnreadable)
	assert.Nil(t, reopened)
}

func TestDifferentKeyIsUnreadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")

	s, err := cache.New(path)
	require.NoError(t, err)
	require.NoError(t, s.Replace(sampleEntries()))

	// Replace the key file with a fresh random one.
	require.NoError(t, os.WriteFile(path+".key", []byte("01234567890123456789012345678901"), 0o600))

	_, err = cache.New(path)
	require.ErrorIs(t, err, cache.ErrCacheUnreadable)
}

func TestZeroLengthCacheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	_, err := cache.New(path)
	require.ErrorIs(t, err, cache.ErrCacheUnreadable)
}

func TestConcurrentUse(t *testing.T) {
	s, _ := newStore(t)
	done := make(chan struct{})
	for i := 0; i < 4; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 20; j++ {
				_ = s.Upsert(&model.Entry{ID: "shared", Type: model.EntryTypeText, Label: "x"})
				_ = s.All()
			}
		}()
	}
	for i := 0; i < 4; i++ {
		<-done
	}
	got, err := s.Get("shared")
	require.NoError(t, err)
	assert.Equal(t, "x", got.Label)
}

func TestDefaultPath(t *testing.T) {
	path, err := cache.DefaultPath()
	require.NoError(t, err)
	assert.Contains(t, path, ".gophkeeper")
	assert.True(t, filepath.IsAbs(path))
}

func TestErrorsAreDistinct(t *testing.T) {
	assert.True(t, errors.Is(cache.ErrNotFound, cache.ErrNotFound))
	assert.False(t, errors.Is(cache.ErrNotFound, cache.ErrCacheUnreadable))
}
