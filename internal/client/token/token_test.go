package token

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreSaveLoadRoundtrip(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "token"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := store.Save("jwt-token-123"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != "jwt-token-123" {
		t.Fatalf("Load returned %q, want %q", got, "jwt-token-123")
	}
}

func TestStoreSaveOverwrites(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "token"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := store.Save("first"); err != nil {
		t.Fatalf("Save first: %v", err)
	}
	if err := store.Save("second"); err != nil {
		t.Fatalf("Save second: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != "second" {
		t.Fatalf("Load returned %q, want %q", got, "second")
	}
}

func TestStoreLoadMissingFile(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "token"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := store.Load(); !errors.Is(err, ErrNoToken) {
		t.Fatalf("Load error = %v, want ErrNoToken", err)
	}
}

func TestStoreClear(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "token"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := store.Save("jwt"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, err := store.Load(); !errors.Is(err, ErrNoToken) {
		t.Fatalf("Load after Clear error = %v, want ErrNoToken", err)
	}
}

func TestStoreClearIdempotent(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "token"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := store.Clear(); err != nil {
		t.Fatalf("Clear on missing file: %v", err)
	}
	// Clearing twice must not fail either.
	if err := store.Clear(); err != nil {
		t.Fatalf("second Clear: %v", err)
	}
}

func TestStoreSavePermissions(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "token"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := store.Save("jwt"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(store.Path())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("token file permissions = %v, want %v", perm, os.FileMode(0o600))
	}
}

func TestStoreSaveCreatesMissingDirectories(t *testing.T) {
	dir := t.TempDir()
	store, err := New(filepath.Join(dir, "nested", "deep", "token"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := store.Save("jwt"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != "jwt" {
		t.Fatalf("Load returned %q, want %q", got, "jwt")
	}
}

func TestNewDefaultPath(t *testing.T) {
	store, err := New("")
	if err != nil {
		t.Fatalf("New(\"\"): %v", err)
	}

	want, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if store.Path() != want {
		t.Fatalf("New(\"\").Path() = %q, want %q", store.Path(), want)
	}
}
