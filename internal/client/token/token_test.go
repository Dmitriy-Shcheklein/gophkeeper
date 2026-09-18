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

func TestDefaultPathWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")

	if _, err := DefaultPath(); err == nil {
		t.Fatal("DefaultPath without HOME returned nil error, want error")
	}
	if _, err := New(""); err == nil {
		t.Fatal("New(\"\") without HOME returned nil error, want error")
	}
}

func TestWriteTempWriteError(t *testing.T) {
	// A closed file makes WriteString fail immediately, exercising
	// the write-error branch of writeTemp.
	f, err := os.CreateTemp(t.TempDir(), "closed")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	store, err := New(filepath.Join(t.TempDir(), "token"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := store.writeTemp(f, "jwt"); err == nil {
		t.Fatal("writeTemp on closed file returned nil error, want error")
	}
}

func TestStoreLoadOnDirectory(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Loading a path that is a directory must fail with a read error
	// (not ErrNoToken, which is reserved for a missing file).
	if _, err := store.Load(); err == nil || errors.Is(err, ErrNoToken) {
		t.Fatalf("Load on directory error = %v, want a non-ErrNoToken read error", err)
	}
}

func TestStoreClearOnDirectory(t *testing.T) {
	dir := t.TempDir()
	// A non-empty directory cannot be removed; make sure Clear
	// reports the removal failure instead of pretending the token is
	// gone.
	if err := os.WriteFile(filepath.Join(dir, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	store, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := store.Clear(); err == nil {
		t.Fatal("Clear on non-empty directory returned nil error, want error")
	}
}

func TestStoreSaveBlockedParentFile(t *testing.T) {
	// Plant a regular file where Save needs a directory: both the
	// MkdirAll and the temp file creation fail.
	base := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(base, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	store, err := New(filepath.Join(base, "token"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := store.Save("jwt"); err == nil {
		t.Fatal("Save with blocked parent returned nil error, want error")
	}
	// Nothing was written: loading fails with a path error distinct
	// from the missing-file sentinel.
	if _, err := store.Load(); err == nil || errors.Is(err, ErrNoToken) {
		t.Fatalf("Load after failed Save error = %v, want a non-ErrNoToken error", err)
	}
}
