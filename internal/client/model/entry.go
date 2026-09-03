// Package model defines the client-side data types shared by the
// GophKeeper client layers (gateway, service, CLI, TUI).
//
// It deliberately does not import internal/server/model: a real
// distributable client must never depend on server internals. The
// wire representation lives in internal/common/proto/gophkeeperv1 and
// the mapping between the two worlds is done by the gateway
// converters (internal/client/gateway/convert.go).
package model

import "time"

// EntryType enumerates the kinds of private data an entry can hold.
// The numeric values mirror the gophkeeper.v1.EntryType proto enum.
type EntryType int32

// Entry types with the same semantics as their proto counterparts.
const (
	// EntryTypeUnspecified is the zero value and must not be used
	// explicitly; it mirrors ENTRY_TYPE_UNSPECIFIED.
	EntryTypeUnspecified EntryType = 0
	// EntryTypeLoginPassword is a login/password credential pair.
	EntryTypeLoginPassword EntryType = 1
	// EntryTypeText is an arbitrary text note.
	EntryTypeText EntryType = 2
	// EntryTypeBinary is arbitrary binary data (e.g. a file).
	EntryTypeBinary EntryType = 3
	// EntryTypeCard is bank card data.
	EntryTypeCard EntryType = 4
)

// Valid reports whether t is one of the supported entry types. The
// zero value EntryTypeUnspecified is not valid: it must not be sent
// to the server explicitly. The semantics mirror the server-side
// model (see internal/server/model).
func (t EntryType) Valid() bool {
	switch t {
	case EntryTypeLoginPassword, EntryTypeText, EntryTypeBinary, EntryTypeCard:
		return true
	default:
		return false
	}
}

// Entry is a single piece of the user's private data, as seen by the
// client. The payload is carried in Data; its meaning is determined
// by Type.
type Entry struct {
	// ID is the unique server-assigned entry identifier. It is empty
	// for entries not yet created on the server.
	ID string
	// Type defines how the Data field should be interpreted.
	Type EntryType
	// Label is a user-visible name of the entry.
	Label string
	// Metadata holds arbitrary text metadata supplied by the user.
	Metadata string
	// Data is the entry payload (credentials, text, raw bytes, card
	// data — depending on Type).
	Data []byte
	// Version is incremented on every update; updates must carry the
	// version they are based on (optimistic locking).
	Version int64
	// CreatedAt is the entry creation timestamp (UTC).
	CreatedAt time.Time
	// UpdatedAt is the last modification timestamp (UTC).
	UpdatedAt time.Time
}
