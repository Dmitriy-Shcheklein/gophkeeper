package model

import "testing"

// TestEntryTypeValid checks EntryType.Valid against the full range of
// known and out-of-range values; the semantics must stay in parity
// with internal/server/model.EntryType.Valid, which mirrors the same
// proto enum.
func TestEntryTypeValid(t *testing.T) {
	tests := []struct {
		name  string
		entry EntryType
		want  bool
	}{
		{"login/password", EntryTypeLoginPassword, true},
		{"text", EntryTypeText, true},
		{"binary", EntryTypeBinary, true},
		{"card", EntryTypeCard, true},
		{"zero (unspecified)", EntryTypeUnspecified, false},
		{"negative", EntryType(-1), false},
		{"out of range", EntryType(5), false},
		{"far out of range", EntryType(100), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.entry.Valid(); got != tt.want {
				t.Errorf("EntryType(%d).Valid() = %v, want %v", tt.entry, got, tt.want)
			}
		})
	}
}
