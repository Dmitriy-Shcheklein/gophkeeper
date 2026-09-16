package cache_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dmitriy/gophkeeper/internal/client/cache"
	"github.com/dmitriy/gophkeeper/internal/client/model"
)

// TestWriteReadAcrossProcesses mimics the CLI one-shot flow: process
// A writes the snapshot and exits, process B (fresh Store, same
// files) must read it back.
func TestWriteReadAcrossProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")

	a, err := cache.New(path)
	if err != nil {
		t.Fatalf("A open: %v", err)
	}
	if err := a.Upsert(&model.Entry{ID: "e1", Type: 2, Label: "x", DataSize: 3, Version: 1}); err != nil {
		t.Fatalf("A upsert: %v", err)
	}
	raw1, _ := os.ReadFile(path)
	key1, _ := os.ReadFile(path + ".key")

	b, err := cache.New(path)
	if err != nil {
		t.Fatalf("B open: %v", err)
	}
	if got := b.All(); len(got) != 1 {
		t.Fatalf("B read: got %d entries, want 1", len(got))
	}
	_ = raw1
	_ = key1
}
