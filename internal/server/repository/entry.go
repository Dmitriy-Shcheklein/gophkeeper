package repository

import (
	"context"

	"github.com/dmitriy/gophkeeper/internal/server/model"
)

// EntryRepository persists and retrieves user entries (stored secrets).
//
// All methods are scoped by user: entries of one user are never visible
// through another user's identifier.
//
// Entries have two payload layouts. Inline entries keep their payload
// in entry.Data (unary Create/Update). Chunked entries keep it in
// per-entry ordered chunks uploaded with the streaming API; their
// entry.Data is empty and DataSize carries the real content size.
type EntryRepository interface {
	// Create inserts a new entry. The database generates the identifier,
	// initial version and timestamps, which are filled back into the
	// corresponding fields of entry.
	Create(ctx context.Context, entry *model.Entry) error
	// GetByID returns the entry with the given identifier owned by the
	// given user, or model.ErrNotFound if no such (ready) entry exists.
	// Pending entries (uploads in progress) are invisible.
	GetByID(ctx context.Context, userID, entryID string) (*model.Entry, error)
	// List returns the ready entries of the given user ordered by
	// creation time. If entryType is not nil, only entries of that type
	// are returned. When includeData is false the payloads are not
	// loaded: entry.Data stays empty and DataSize reports the size.
	List(ctx context.Context, userID string, entryType *model.EntryType, includeData bool) ([]*model.Entry, error)
	// Update modifies mutable fields of the entry using optimistic locking:
	// it succeeds only if the stored version matches entry.Version. On
	// success the new version and updated timestamp are filled back into
	// the entry. Returns model.ErrConflict if the stored version differs
	// (stale copy) and model.ErrNotFound if the entry does not exist.
	Update(ctx context.Context, entry *model.Entry) error
	// Delete removes the entry with the given identifier owned by the
	// given user. Returns model.ErrNotFound if no such entry exists.
	Delete(ctx context.Context, userID, entryID string) error

	// CreatePending inserts a new invisible (pending) entry for a
	// chunked upload. Chunks are added with AppendChunk and the entry
	// becomes visible after FinalizeCreate. On success the generated
	// identifier, initial version and timestamps are filled back into
	// entry.
	CreatePending(ctx context.Context, entry *model.Entry) error
	// AppendChunk stores one payload chunk (0-based seq) of an entry
	// being uploaded.
	AppendChunk(ctx context.Context, entryID string, seq int, data []byte) error
	// FinalizeCreate completes a chunked upload of a new entry: the
	// entry becomes visible, its DataSize is computed from the stored
	// chunks and filled back together with version and updated_at.
	FinalizeCreate(ctx context.Context, entry *model.Entry) error
	// PrepareUpdate verifies that the entry exists for the user and is
	// at entry.Version (model.ErrNotFound / model.ErrConflict
	// otherwise) and returns the number of already stored chunks of the
	// entry. Incoming chunks of the update must use sequence numbers
	// starting at that offset so the old chunks stay readable until the
	// update is finalized.
	PrepareUpdate(ctx context.Context, entry *model.Entry) (int, error)
	// FinalizeUpdate completes a chunked update started with
	// PrepareUpdate: in one transaction it re-checks the version,
	// deletes the old chunks (everything below chunkOffset), clears the
	// inline payload and bumps the version. Fills version, updated_at
	// and DataSize back into entry. Same error semantics as Update.
	FinalizeUpdate(ctx context.Context, entry *model.Entry, chunkOffset int) error
	// AbortUpdate discards chunks appended after chunkOffset, leaving
	// the entry in its pre-update state. Used when an update stream
	// fails midway.
	AbortUpdate(ctx context.Context, entryID string, chunkOffset int) error
	// DeleteIfPending removes the entry if it is still a pending
	// (never finalized) upload, cascading its chunks. Missing or ready
	// entries are left untouched.
	DeleteIfPending(ctx context.Context, entryID string) error
	// ChunkCount returns the number of stored chunks of the ready entry
	// owned by the user (0 for inline entries) together with the total
	// payload size. model.ErrNotFound when the entry does not exist.
	ChunkCount(ctx context.Context, userID, entryID string) (int, error)
	// Chunk returns one stored chunk of a ready entry owned by the
	// user. model.ErrNotFound when there is no such chunk.
	Chunk(ctx context.Context, userID, entryID string, seq int) ([]byte, error)
}
