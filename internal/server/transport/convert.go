package transport

import (
	"github.com/dmitriy/gophkeeper/internal/common/proto/gophkeeperv1"
	"github.com/dmitriy/gophkeeper/internal/server/model"
)

// entryTypeToProto converts a domain entry type to its protobuf
// representation. Unsupported values map to ENTRY_TYPE_UNSPECIFIED;
// the service layer never produces them for stored entries.
func entryTypeToProto(t model.EntryType) gophkeeperv1.EntryType {
	switch t {
	case model.EntryTypeLoginPassword:
		return gophkeeperv1.EntryType_ENTRY_TYPE_LOGIN_PASSWORD
	case model.EntryTypeText:
		return gophkeeperv1.EntryType_ENTRY_TYPE_TEXT
	case model.EntryTypeBinary:
		return gophkeeperv1.EntryType_ENTRY_TYPE_BINARY
	case model.EntryTypeCard:
		return gophkeeperv1.EntryType_ENTRY_TYPE_CARD
	default:
		return gophkeeperv1.EntryType_ENTRY_TYPE_UNSPECIFIED
	}
}

// entryTypeFromProto converts a protobuf entry type to its domain
// representation. Unsupported values map to the zero model type, which
// the service layer rejects as invalid input.
func entryTypeFromProto(t gophkeeperv1.EntryType) model.EntryType {
	switch t {
	case gophkeeperv1.EntryType_ENTRY_TYPE_LOGIN_PASSWORD:
		return model.EntryTypeLoginPassword
	case gophkeeperv1.EntryType_ENTRY_TYPE_TEXT:
		return model.EntryTypeText
	case gophkeeperv1.EntryType_ENTRY_TYPE_BINARY:
		return model.EntryTypeBinary
	case gophkeeperv1.EntryType_ENTRY_TYPE_CARD:
		return model.EntryTypeCard
	default:
		return 0
	}
}

// entryToProto converts a domain entry to its protobuf representation.
// Timestamps are expressed as Unix seconds; nil yields nil.
func entryToProto(e *model.Entry) *gophkeeperv1.Entry {
	if e == nil {
		return nil
	}
	return &gophkeeperv1.Entry{
		Id:        e.ID,
		Type:      entryTypeToProto(e.Type),
		Label:     e.Label,
		Metadata:  e.Metadata,
		Data:      e.Data,
		Version:   e.Version,
		CreatedAt: e.CreatedAt.Unix(),
		UpdatedAt: e.UpdatedAt.Unix(),
	}
}

// entriesToProto converts a slice of domain entries to its protobuf
// representation, preserving order.
func entriesToProto(entries []*model.Entry) []*gophkeeperv1.Entry {
	out := make([]*gophkeeperv1.Entry, 0, len(entries))
	for _, e := range entries {
		out = append(out, entryToProto(e))
	}
	return out
}
