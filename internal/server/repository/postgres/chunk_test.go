package postgres_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitriy/gophkeeper/internal/server/model"
)

// seedChunkedEntry uploads a chunked entry the way the streaming flow
// does: a pending row, two chunks and a finalize.
func seedChunkedEntry(t *testing.T, userID, label string, chunks ...[]byte) *model.Entry {
	t.Helper()
	entries := testStorage.Entries()
	entry := &model.Entry{
		UserID: userID,
		Type:   model.EntryTypeBinary,
		Label:  label,
	}
	require.NoError(t, entries.CreatePending(t.Context(), entry))
	require.NotEmpty(t, entry.ID)
	for seq, data := range chunks {
		require.NoError(t, entries.AppendChunk(t.Context(), entry.ID, seq, data))
	}
	require.NoError(t, entries.FinalizeCreate(t.Context(), entry))
	return entry
}

func TestEntryRepository_ChunkedUploadCreate(t *testing.T) {
	resetTables(t)
	user := createTestUser(t, "chunk-create-owner")
	entries := testStorage.Entries()

	entry := &model.Entry{
		UserID: user.ID,
		Type:   model.EntryTypeBinary,
		Label:  "big-file",
	}
	require.NoError(t, entries.CreatePending(t.Context(), entry))
	assert.False(t, entry.CreatedAt.IsZero())

	// A pending entry is invisible to the read paths.
	_, err := entries.GetByID(t.Context(), user.ID, entry.ID)
	assert.ErrorIs(t, err, model.ErrNotFound)
	listed, err := entries.List(t.Context(), user.ID, nil, false)
	require.NoError(t, err)
	assert.Empty(t, listed)

	require.NoError(t, entries.AppendChunk(t.Context(), entry.ID, 0, []byte("chunk-0-data")))
	require.NoError(t, entries.AppendChunk(t.Context(), entry.ID, 1, []byte("chunk-1-data")))

	require.NoError(t, entries.FinalizeCreate(t.Context(), entry))
	assert.Equal(t, int64(1), entry.Version)
	assert.Equal(t, int64(len("chunk-0-data")+len("chunk-1-data")), entry.DataSize)
	assert.False(t, entry.UpdatedAt.IsZero())

	// After the finalize the entry is visible without its inline data.
	stored, err := entries.GetByID(t.Context(), user.ID, entry.ID)
	require.NoError(t, err)
	assert.Empty(t, stored.Data)
	assert.Equal(t, entry.DataSize, stored.DataSize)
	assert.Equal(t, model.EntryStateReady, stored.State)

	listed, err = entries.List(t.Context(), user.ID, nil, false)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Empty(t, listed[0].Data)
	assert.Equal(t, entry.DataSize, listed[0].DataSize)

	// Chunks are readable one by one; unknown sequences are not.
	count, err := entries.ChunkCount(t.Context(), user.ID, entry.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	chunk, err := entries.Chunk(t.Context(), user.ID, entry.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, []byte("chunk-1-data"), chunk)

	_, err = entries.Chunk(t.Context(), user.ID, entry.ID, 5)
	assert.ErrorIs(t, err, model.ErrNotFound)

	// Chunks of another user are not readable.
	_, err = entries.Chunk(t.Context(), missingUUID(t), entry.ID, 0)
	assert.ErrorIs(t, err, model.ErrNotFound)

	// DeleteIfPending must not touch a finalized entry.
	require.NoError(t, entries.DeleteIfPending(t.Context(), entry.ID))
	_, err = entries.GetByID(t.Context(), user.ID, entry.ID)
	assert.NoError(t, err)
}

func TestEntryRepository_ChunkedUploadAbort(t *testing.T) {
	resetTables(t)
	user := createTestUser(t, "chunk-abort-owner")
	entries := testStorage.Entries()

	entry := &model.Entry{
		UserID: user.ID,
		Type:   model.EntryTypeBinary,
		Label:  "abandoned",
	}
	require.NoError(t, entries.CreatePending(t.Context(), entry))
	require.NoError(t, entries.AppendChunk(t.Context(), entry.ID, 0, []byte("half")))

	require.NoError(t, entries.DeleteIfPending(t.Context(), entry.ID))

	_, err := entries.GetByID(t.Context(), user.ID, entry.ID)
	assert.ErrorIs(t, err, model.ErrNotFound)

	var count int
	require.NoError(t, testPool.QueryRow(t.Context(),
		"SELECT COUNT(*) FROM entry_chunks WHERE entry_id = $1", entry.ID).Scan(&count))
	assert.Zero(t, count)
}

func TestEntryRepository_ChunkedUpdate(t *testing.T) {
	resetTables(t)
	user := createTestUser(t, "chunk-update-owner")
	entries := testStorage.Entries()

	created := createTestEntry(t, user.ID, model.Entry{
		Type:  model.EntryTypeText,
		Label: "note",
		Data:  []byte("original"),
	})

	target := &model.Entry{
		ID:      created.ID,
		UserID:  user.ID,
		Version: created.Version,
		Label:   "note-renamed",
	}
	offset, err := entries.PrepareUpdate(t.Context(), target)
	require.NoError(t, err)
	assert.Zero(t, offset)

	require.NoError(t, entries.AppendChunk(t.Context(), created.ID, offset+0, []byte("new-0")))
	require.NoError(t, entries.AppendChunk(t.Context(), created.ID, offset+1, []byte("new-1")))

	// The entry stays readable with its old content during the upload.
	during, err := entries.GetByID(t.Context(), user.ID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, []byte("original"), during.Data)

	require.NoError(t, entries.FinalizeUpdate(t.Context(), target, offset))
	assert.Equal(t, created.Version+1, target.Version)
	assert.Equal(t, int64(len("new-0")+len("new-1")), target.DataSize)

	stored, err := entries.GetByID(t.Context(), user.ID, created.ID)
	require.NoError(t, err)
	assert.Empty(t, stored.Data)
	assert.Equal(t, target.DataSize, stored.DataSize)
	assert.Equal(t, "note-renamed", stored.Label)

	count, err := entries.ChunkCount(t.Context(), user.ID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// A second chunked update offsets behind the existing chunks and
	// replaces them completely: after the finalize only the new chunks
	// remain, renumbered to start at 0.
	target2 := &model.Entry{ID: created.ID, UserID: user.ID, Version: stored.Version, Label: "note-v3"}
	offset2, err := entries.PrepareUpdate(t.Context(), target2)
	require.NoError(t, err)
	assert.Equal(t, 2, offset2)
	require.NoError(t, entries.AppendChunk(t.Context(), created.ID, offset2, []byte("v3")))
	require.NoError(t, entries.FinalizeUpdate(t.Context(), target2, offset2))

	count, err = entries.ChunkCount(t.Context(), user.ID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "old chunks must be deleted by the finalize")
	assert.Equal(t, int64(len("v3")), target2.DataSize)

	// The renumbering invariant: chunks start at 0 again, which the
	// download path relies on.
	chunk, err := entries.Chunk(t.Context(), user.ID, created.ID, 0)
	require.NoError(t, err)
	assert.Equal(t, []byte("v3"), chunk)
}

// TestEntryRepository_PlainUpdateDropsStaleChunks guards the storage
// invariant "either inline payload or chunks, never both": a plain
// (non-streaming) update of an entry that was previously stored as
// chunks must delete the stale chunk rows, otherwise the download path
// keeps serving the old chunked payload.
func TestEntryRepository_PlainUpdateDropsStaleChunks(t *testing.T) {
	resetTables(t)
	user := createTestUser(t, "plain-update-owner")
	entries := testStorage.Entries()

	created := seedChunkedEntry(t, user.ID, "file", []byte("old-payload"))

	updated := &model.Entry{
		ID:      created.ID,
		UserID:  user.ID,
		Version: created.Version,
		Label:   "file",
		Data:    []byte("new-inline-payload"),
	}
	require.NoError(t, entries.Update(t.Context(), updated))

	stored, err := entries.GetByID(t.Context(), user.ID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, []byte("new-inline-payload"), stored.Data)
	assert.Equal(t, int64(len("new-inline-payload")), stored.DataSize)

	count, err := entries.ChunkCount(t.Context(), user.ID, created.ID)
	require.NoError(t, err)
	assert.Zero(t, count, "a plain update must drop the stale chunk rows")
}

func TestEntryRepository_PrepareUpdateErrors(t *testing.T) {
	resetTables(t)
	user := createTestUser(t, "prepare-owner")
	stranger := createTestUser(t, "prepare-stranger")

	created := createTestEntry(t, user.ID, model.Entry{
		Type: model.EntryTypeText, Label: "note", Data: []byte("data"),
	})

	_, err := testStorage.Entries().PrepareUpdate(t.Context(), &model.Entry{
		ID: created.ID, UserID: user.ID, Version: created.Version + 100,
	})
	assert.ErrorIs(t, err, model.ErrConflict)

	_, err = testStorage.Entries().PrepareUpdate(t.Context(), &model.Entry{
		ID: created.ID, UserID: stranger.ID, Version: created.Version,
	})
	assert.ErrorIs(t, err, model.ErrNotFound)
}

func TestEntryRepository_AbortUpdate(t *testing.T) {
	resetTables(t)
	user := createTestUser(t, "abort-update-owner")
	entries := testStorage.Entries()

	created := createTestEntry(t, user.ID, model.Entry{
		Type: model.EntryTypeText, Label: "note", Data: []byte("keep"),
	})

	target := &model.Entry{ID: created.ID, UserID: user.ID, Version: created.Version, Label: "note"}
	offset, err := entries.PrepareUpdate(t.Context(), target)
	require.NoError(t, err)
	require.NoError(t, entries.AppendChunk(t.Context(), created.ID, offset, []byte("abandoned")))

	require.NoError(t, entries.AbortUpdate(t.Context(), created.ID, offset))

	count, err := entries.ChunkCount(t.Context(), user.ID, created.ID)
	require.NoError(t, err)
	assert.Zero(t, count)

	stored, err := entries.GetByID(t.Context(), user.ID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, []byte("keep"), stored.Data)
	assert.Equal(t, created.Version, stored.Version)
}

func TestEntryRepository_ListIncludeData(t *testing.T) {
	resetTables(t)
	user := createTestUser(t, "list-data-owner")

	createTestEntry(t, user.ID, model.Entry{
		Type: model.EntryTypeText, Label: "inline", Data: []byte("inline-data"),
	})
	seedChunkedEntry(t, user.ID, "chunked", []byte("chunked-data"))

	entries := testStorage.Entries()

	withData, err := entries.List(t.Context(), user.ID, nil, true)
	require.NoError(t, err)
	require.Len(t, withData, 2)
	byLabel := map[string]*model.Entry{}
	for _, e := range withData {
		byLabel[e.Label] = e
	}
	assert.Equal(t, []byte("inline-data"), byLabel["inline"].Data)
	assert.Empty(t, byLabel["chunked"].Data)
	assert.Equal(t, int64(len("chunked-data")), byLabel["chunked"].DataSize)

	withoutData, err := entries.List(t.Context(), user.ID, nil, false)
	require.NoError(t, err)
	require.Len(t, withoutData, 2)
	for _, e := range withoutData {
		assert.Empty(t, e.Data, "payload must not be loaded when includeData is false")
	}
	assert.Equal(t, int64(len("inline-data")), byLabelLookup(withoutData, "inline"))
	assert.Equal(t, int64(len("chunked-data")), byLabelLookup(withoutData, "chunked"))
}

// byLabelLookup returns the DataSize of the entry with the given label.
func byLabelLookup(entries []*model.Entry, label string) int64 {
	for _, e := range entries {
		if e.Label == label {
			return e.DataSize
		}
	}
	return -1
}

func TestEntryRepository_DeleteCascadesChunks(t *testing.T) {
	resetTables(t)
	user := createTestUser(t, "cascade-owner")

	entry := seedChunkedEntry(t, user.ID, "doomed", []byte("a"), []byte("b"))

	require.NoError(t, testStorage.Entries().Delete(t.Context(), user.ID, entry.ID))

	var count int
	require.NoError(t, testPool.QueryRow(t.Context(),
		"SELECT COUNT(*) FROM entry_chunks WHERE entry_id = $1", entry.ID).Scan(&count))
	assert.Zero(t, count)
}
