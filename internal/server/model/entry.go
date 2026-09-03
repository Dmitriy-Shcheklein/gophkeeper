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
	// Data is the secret payload.
	Data []byte
	// Version is a monotonically increasing revision used for conflict
	// detection during synchronization.
	Version int64
	// CreatedAt is the moment the entry was created.
	CreatedAt time.Time
	// UpdatedAt is the moment the entry was last modified.
	UpdatedAt time.Time
}
