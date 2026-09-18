package model

import "time"

// EntryType is the kind of data stored in an Entry.
type EntryType int

// Supported entry types, mirroring the EntryType enum in the public API.
const (
	// EntryTypeLoginPassword is a login/password pair.
	EntryTypeLoginPassword EntryType = 1
	// EntryTypeText is an arbitrary piece of text (e.g. a secure note).
	EntryTypeText EntryType = 2
	// EntryTypeBinary is an arbitrary binary blob (e.g. a file).
	EntryTypeBinary EntryType = 3
	// EntryTypeCard is a payment card record.
	EntryTypeCard EntryType = 4
)

// Valid reports whether t is one of the supported entry types.
func (t EntryType) Valid() bool {
	switch t {
	case EntryTypeLoginPassword, EntryTypeText, EntryTypeBinary, EntryTypeCard:
		return true
	default:
		return false
	}
}

// Entry is a single stored secret belonging to a user.
type Entry struct {
	// ID is the unique entry identifier (UUID).
	ID string
	// UserID is the identifier of the user who owns the entry.
	UserID string
	// Type is the kind of data stored in Data.
	Type EntryType
	// Label is a user-visible name of the entry.
	Label string
	// Metadata is optional user-supplied auxiliary information.
	Metadata string
	// Data is the secret payload. For entries uploaded in chunks it is
	// empty and the payload lives in entry_chunks; DataSize then
	// reports the real content size.
	Data []byte
	// DataSize is the total payload size in bytes: len(Data) for
	// inline entries or the summed chunk size for chunked ones.
	DataSize int64
	// Version is a monotonically increasing revision used for conflict
	// detection during synchronization.
	Version int64
	// CreatedAt is the moment the entry was created.
	CreatedAt time.Time
	// UpdatedAt is the moment the entry was last modified.
	UpdatedAt time.Time
	// State is the upload lifecycle state of the entry.
	State EntryState
}

// EntryState is the upload lifecycle state of an entry.
type EntryState int

const (
	// EntryStateReady marks a fully uploaded entry, visible to all
	// reads.
	EntryStateReady EntryState = 1
	// EntryStatePending marks an entry whose chunked upload is still in
	// progress. Pending entries are invisible to List/Get/Sync and are
	// deleted (cascading their chunks) when an upload fails or is
	// aborted.
	EntryStatePending EntryState = 2
)
