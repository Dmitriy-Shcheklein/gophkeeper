// Package token provides a small persistent store for the client's
// access token.
//
// The GophKeeper CLI is one-shot per command, so a token obtained by
// `login` or `register` in one process must survive into the next
// invocation. Store persists the token to a single file (by default
// ~/.gophkeeper/token) with 0600 permissions, using an atomic
// write-then-rename so an interrupted process can never leave a
// half-written token file behind.
package token

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrNoToken is returned by Store.Load when no token is persisted
// (the file is missing); use errors.Is to detect it.
var ErrNoToken = errors.New("no saved token")

// filePerm is the permission the token file is created with: only the
// owner may read or write it.
const filePerm os.FileMode = 0o600

// dirPerm is the permission for the directory holding the token file.
const dirPerm os.FileMode = 0o700

// Store keeps the access token in memory and persists it to a single
// file. It is safe for concurrent use.
type Store struct {
	path string
}

// New returns a Store persisting the token to path. An empty path
// resolves to the default location (~/.gophkeeper/token).
func New(path string) (*Store, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return nil, err
		}
	}
	return &Store{path: path}, nil
}

// DefaultPath returns the default token file location:
// ~/.gophkeeper/token.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("token: resolve home directory: %w", err)
	}
	return filepath.Join(home, ".gophkeeper", "token"), nil
}

// Path returns the file path the store persists the token to.
func (s *Store) Path() string {
	return s.path
}

// Save atomically persists the token: it writes to a temporary file
// in the same directory (so the rename stays on one filesystem) with
// 0600 permissions, then renames it over the target file. The token
// is also kept in memory, though Store holds no authoritative copy —
// Load always reads the file.
func (s *Store) Save(value string) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("token: create directory %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("token: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()

	if err := s.writeTemp(tmp, value); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("token: replace %s: %w", s.path, err)
	}
	return nil
}

// writeTemp writes the value to tmp, fsyncs and closes the file,
// leaving the cleanup decision to the caller.
func (s *Store) writeTemp(tmp *os.File, value string) error {
	if _, err := tmp.WriteString(value); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("token: write temp file: %w", err)
	}
	if err := tmp.Chmod(filePerm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("token: chmod temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("token: sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("token: close temp file: %w", err)
	}
	return nil
}

// Load reads the persisted token. It returns an error wrapping
// ErrNoToken when no token file exists; all read failures are
// wrapped with the file path for context.
func (s *Store) Load() (string, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNoToken
		}
		return "", fmt.Errorf("token: read %s: %w", s.path, err)
	}
	return string(data), nil
}

// Clear removes the persisted token file. It is idempotent: clearing
// when no token file exists is not an error.
func (s *Store) Clear() error {
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("token: remove %s: %w", s.path, err)
	}
	return nil
}
