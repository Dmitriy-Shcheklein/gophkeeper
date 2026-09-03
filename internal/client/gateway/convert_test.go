package gateway

import (
	"reflect"
	"testing"
	"time"

	"github.com/dmitriy/gophkeeper/internal/client/model"
	v1 "github.com/dmitriy/gophkeeper/internal/common/proto/gophkeeperv1"
)

func TestEntryToProtoAllFields(t *testing.T) {
	in := cannedClientEntry()
	got := entryToProto(in)

	if got.GetId() != in.ID {
		t.Errorf("Id = %q, want %q", got.GetId(), in.ID)
	}
	if got.GetType() != v1.EntryType_ENTRY_TYPE_CARD {
		t.Errorf("Type = %v, want ENTRY_TYPE_CARD", got.GetType())
	}
	if got.GetLabel() != in.Label {
		t.Errorf("Label = %q, want %q", got.GetLabel(), in.Label)
	}
	if got.GetMetadata() != in.Metadata {
		t.Errorf("Metadata = %q, want %q", got.GetMetadata(), in.Metadata)
	}
	if !reflect.DeepEqual(got.GetData(), in.Data) {
		t.Errorf("Data = %v, want %v", got.GetData(), in.Data)
	}
	if got.GetVersion() != in.Version {
		t.Errorf("Version = %d, want %d", got.GetVersion(), in.Version)
	}
	// The server owns the timestamps; entryToProto must not send
	// them.
	if got.GetCreatedAt() != 0 || got.GetUpdatedAt() != 0 {
		t.Errorf("timestamps sent to server: created=%d updated=%d, want 0",
			got.GetCreatedAt(), got.GetUpdatedAt())
	}
}

func TestEntryFromProtoAllFields(t *testing.T) {
	in := cannedProtoEntry()
	got := entryFromProto(in)

	if got.ID != in.GetId() {
		t.Errorf("ID = %q, want %q", got.ID, in.GetId())
	}
	if got.Type != model.EntryTypeCard {
		t.Errorf("Type = %v, want EntryTypeCard", got.Type)
	}
	if got.Label != in.GetLabel() {
		t.Errorf("Label = %q, want %q", got.Label, in.GetLabel())
	}
	if got.Metadata != in.GetMetadata() {
		t.Errorf("Metadata = %q, want %q", got.Metadata, in.GetMetadata())
	}
	if !reflect.DeepEqual(got.Data, in.GetData()) {
		t.Errorf("Data = %v, want %v", got.Data, in.GetData())
	}
	if got.Version != in.GetVersion() {
		t.Errorf("Version = %d, want %d", got.Version, in.GetVersion())
	}
	if want := time.Unix(in.GetCreatedAt(), 0).UTC(); !got.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want)
	}
	if want := time.Unix(in.GetUpdatedAt(), 0).UTC(); !got.UpdatedAt.Equal(want) {
		t.Errorf("UpdatedAt = %v, want %v", got.UpdatedAt, want)
	}
}

func TestEntryConvertNil(t *testing.T) {
	if got := entryToProto(nil); got != nil {
		t.Errorf("entryToProto(nil) = %v, want nil", got)
	}
	if got := entryFromProto(nil); got != nil {
		t.Errorf("entryFromProto(nil) = %v, want nil", got)
	}
}

func TestEntryTypeValuesMatchProto(t *testing.T) {
	tests := []struct {
		client model.EntryType
		proto  v1.EntryType
	}{
		{model.EntryTypeUnspecified, v1.EntryType_ENTRY_TYPE_UNSPECIFIED},
		{model.EntryTypeLoginPassword, v1.EntryType_ENTRY_TYPE_LOGIN_PASSWORD},
		{model.EntryTypeText, v1.EntryType_ENTRY_TYPE_TEXT},
		{model.EntryTypeBinary, v1.EntryType_ENTRY_TYPE_BINARY},
		{model.EntryTypeCard, v1.EntryType_ENTRY_TYPE_CARD},
	}
	for _, tt := range tests {
		if entryTypeToProto(tt.client) != tt.proto {
			t.Errorf("entryTypeToProto(%d) != %d", tt.client, tt.proto)
		}
		if entryTypeFromProto(tt.proto) != tt.client {
			t.Errorf("entryTypeFromProto(%d) != %d", tt.proto, tt.client)
		}
	}
}

func TestEntryTypeUnknownPassthrough(t *testing.T) {
	// Unknown values must survive the roundtrip so the server can
	// reject them with a proper validation error.
	const unknown = model.EntryType(99)
	if got := entryTypeFromProto(entryTypeToProto(unknown)); got != unknown {
		t.Errorf("unknown type roundtrip = %d, want %d", got, unknown)
	}
}
