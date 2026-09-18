package model

import "testing"

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
		{"zero", EntryType(0), false},
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
