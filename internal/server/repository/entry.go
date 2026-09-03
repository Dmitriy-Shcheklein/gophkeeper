package repository

import (
	"context"

	"github.com/dmitriy/gophkeeper/internal/server/model"
)

// EntryRepository persists and retrieves user entries (stored secrets).
//
// All methods are scoped by user: entries of one user are never visible
// through another user's identifier.
type EntryRepository interface {
	// Create inserts a new entry. The database generates the identifier,
	// initial version and timestamps, which are filled back into the
	// corresponding fields of entry.
	Create(ctx context.Context, entry *model.Entry) error
	// GetByID returns the entry with the given identifier owned by the
	// given user, or model.ErrNotFound if no such entry exists.
	GetByID(ctx context.Context, userID, entryID string) (*model.Entry, error)
	// List returns the entries of the given user ordered by creation time.
	// If entryType is not nil, only entries of that type are returned.
	List(ctx context.Context, userID string, entryType *model.EntryType) ([]*model.Entry, error)
	// Update modifies mutable fields of the entry using optimistic locking:
	// it succeeds only if the stored version matches entry.Version. On
	// success the new version and updated timestamp are filled back into
	// the entry. Returns model.ErrConflict if the stored version differs
	// (stale copy) and model.ErrNotFound if the entry does not exist.
	Update(ctx context.Context, entry *model.Entry) error
	// Delete removes the entry with the given identifier owned by the
	// given user. Returns model.ErrNotFound if no such entry exists.
	Delete(ctx context.Context, userID, entryID string) error
}
