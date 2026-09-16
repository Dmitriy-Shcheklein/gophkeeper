// Package cache provides a persistent, encrypted local snapshot of
// the user's entries — the offline-mode backing store of the client.
//
// The cache is a single AES-256-GCM sealed JSON file holding entry
// metadata only (ids, labels, types, sizes and versions — never the
// secret payloads: offline mode is read-only, payloads stay on the
// server and are not written to disk). The 256-bit key lives in a
// separate key file with 0600 permissions, created on first use —
// the same trust model as the plaintext token file.
//
// The service layer refreshes the snapshot after every successful
// read operation and falls back to it when the server is
// unreachable. The whole file is rewritten atomically
// (write-then-rename), so an interrupted process can never leave a
// half-written snapshot behind.
package cache

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dmitriy/gophkeeper/internal/client/model"
)

// ErrCacheUnreadable is returned when the cache file exists but
// cannot be decrypted (corrupted, truncated or written with a
// different key); use errors.Is to detect it. The snapshot in such a
// file is unrecoverable and the file is left in place.
var ErrCacheUnreadable = errors.New("cache file is corrupted or was written with a different key")

const (
	filePerm os.FileMode = 0o600
	dirPerm  os.FileMode = 0o700

	// formatVersion pins the snapshot layout; a reader seeing a
	// higher version treats the file as unreadable rather than
	// misinterpreting it.
	formatVersion = 1

	keySize = 32 // AES-256
)

// cachedEntry is the JSON representation of one entry: a mirror of
// model.Entry without the payload (offline mode is metadata-only).
type cachedEntry struct {
	ID        string    `json:"id"`
	Type      int32     `json:"type"`
	Label     string    `json:"label"`
	Metadata  string    `json:"metadata,omitempty"`
	DataSize  int64     `json:"data_size"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// snapshot is the decrypted file content.
type snapshot struct {
	Format  int           `json:"format"`
	SavedAt time.Time     `json:"saved_at"`
	Entries []cachedEntry `json:"entries"`
}

// Store keeps the entry snapshot in memory and persists it encrypted
// to a single file. It is safe for concurrent use.
type Store struct {
	mu       sync.Mutex
	path     string
	key      []byte
	snapshot snapshot
}

// New returns a Store persisting the encrypted snapshot to path (an
// empty path resolves to DefaultPath). The encryption key is loaded
// from the key file next to the cache file or created on first use.
func New(path string) (*Store, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return nil, err
		}
	}
	key, err := loadOrCreateKey(keyPath(path))
	if err != nil {
		return nil, err
	}
	s := &Store{path: path, key: key}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// DefaultPath returns the default cache file location:
// ~/.gophkeeper/cache.json.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cache: resolve home directory: %w", err)
	}
	return filepath.Join(home, ".gophkeeper", "cache.json"), nil
}

// keyPath returns the key file location for a cache file path: the
// same directory, "<name>.key".
func keyPath(cachePath string) string {
	return cachePath + ".key"
}

// loadOrCreateKey reads the 256-bit cache key from path or creates a
// fresh random one (with the parent directory and 0600 file).
func loadOrCreateKey(path string) ([]byte, error) {
	if data, err := os.ReadFile(path); err == nil {
		if len(data) != keySize {
			return nil, fmt.Errorf("cache: key file %s has unexpected size %d", path, len(data))
		}
		return data, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("cache: read key file %s: %w", path, err)
	}

	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("cache: generate key: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return nil, fmt.Errorf("cache: create directory %s: %w", dir, err)
	}
	if err := os.WriteFile(path, key, filePerm); err != nil {
		return nil, fmt.Errorf("cache: write key file %s: %w", path, err)
	}
	return key, nil
}

// Path returns the file path the store persists the snapshot to.
func (s *Store) Path() string {
	return s.path
}

// Replace overwrites the snapshot with the given full set of entries
// (the authoritative server state after a successful List or Sync)
// and persists it. The payloads are dropped: the cache is
// metadata-only.
func (s *Store) Replace(entries []*model.Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.snapshot = snapshot{
		Format:  formatVersion,
		SavedAt: time.Now().UTC(),
		Entries: make([]cachedEntry, 0, len(entries)),
	}
	for _, e := range entries {
		if e == nil {
			continue
		}
		s.snapshot.Entries = append(s.snapshot.Entries, toCached(e))
	}
	return s.persistLocked()
}

// Upsert inserts or updates the given entries in the snapshot (the
// result of a successful Get, Add or Edit) and persists it.
func (s *Store) Upsert(entries ...*model.Entry) error {
	if len(entries) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, e := range entries {
		if e == nil || e.ID == "" {
			continue
		}
		replaced := false
		for i := range s.snapshot.Entries {
			if s.snapshot.Entries[i].ID == e.ID {
				s.snapshot.Entries[i] = toCached(e)
				replaced = true
				break
			}
		}
		if !replaced {
			s.snapshot.Entries = append(s.snapshot.Entries, toCached(e))
		}
	}
	return s.persistLocked()
}

// Delete removes the entry with the given id from the snapshot
// (after a successful Remove) and persists it. Deleting an unknown
// id is not an error.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	kept := s.snapshot.Entries[:0]
	for _, e := range s.snapshot.Entries {
		if e.ID != id {
			kept = append(kept, e)
		}
	}
	s.snapshot.Entries = kept
	return s.persistLocked()
}

// Clear drops the snapshot from memory and disk (used on logout and
// account switching). It is idempotent.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.snapshot = snapshot{}
	err := os.Remove(s.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("cache: remove %s: %w", s.path, err)
	}
	return nil
}

// All returns the cached entries (payload-free). An empty snapshot
// (no cache file yet) yields a nil slice and no error.
func (s *Store) All() []*model.Entry {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]*model.Entry, 0, len(s.snapshot.Entries))
	for i := range s.snapshot.Entries {
		out = append(out, s.snapshot.Entries[i].toModel())
	}
	return out
}

// Get returns the cached entry with the given id, or an error
// wrapping ErrNotFound. The returned entry carries no payload.
func (s *Store) Get(id string) (*model.Entry, error) {
	if id == "" {
		return nil, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.snapshot.Entries {
		if s.snapshot.Entries[i].ID == id {
			return s.snapshot.Entries[i].toModel(), nil
		}
	}
	return nil, fmt.Errorf("cache: entry %q: %w", id, ErrNotFound)
}

// ErrNotFound is returned by Get when the snapshot holds no entry
// with the requested id; use errors.Is to detect it.
var ErrNotFound = errors.New("not found in cache")

// persistLocked encrypts the current snapshot and writes it
// atomically. Callers must hold mu.
func (s *Store) persistLocked() error {
	s.snapshot.Format = formatVersion
	plain, err := json.Marshal(&s.snapshot)
	if err != nil {
		return fmt.Errorf("cache: encode snapshot: %w", err)
	}
	sealed, err := s.seal(plain)
	if err != nil {
		return err
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("cache: create directory %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("cache: create temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write(sealed); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("cache: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cache: write temp file: %w", err)
	}
	if err := os.Chmod(tmp.Name(), filePerm); err != nil {
		return fmt.Errorf("cache: chmod temp file: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return fmt.Errorf("cache: replace %s: %w", s.path, err)
	}
	return nil
}

// seal encrypts plain with AES-256-GCM: the returned layout is
// nonce || ciphertext+tag.
func (s *Store) seal(plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, fmt.Errorf("cache: init cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cache: init gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("cache: generate nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

// open decrypts the file content produced by seal.
func (s *Store) open(sealed []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, fmt.Errorf("cache: init cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cache: init gcm: %w", err)
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, ErrCacheUnreadable
	}
	plain, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], nil)
	if err != nil {
		return nil, ErrCacheUnreadable
	}
	return plain, nil
}

// load reads and decrypts the cache file. A missing file leaves the
// snapshot empty (a valid, empty cache); an undecryptable file is
// reported as ErrCacheUnreadable.
func (s *Store) load() error {
	sealed, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cache: read %s: %w", s.path, err)
	}
	plain, err := s.open(sealed)
	if err != nil {
		return err
	}
	var snap snapshot
	if err := json.Unmarshal(plain, &snap); err != nil {
		return ErrCacheUnreadable
	}
	if snap.Format != formatVersion {
		return ErrCacheUnreadable
	}
	s.snapshot = snap
	return nil
}

// toCached strips the payload and converts to the JSON form.
func toCached(e *model.Entry) cachedEntry {
	c := cachedEntry{
		ID:        e.ID,
		Type:      int32(e.Type),
		Label:     e.Label,
		Metadata:  e.Metadata,
		DataSize:  e.DataSize,
		Version:   e.Version,
		CreatedAt: e.CreatedAt.UTC(),
		UpdatedAt: e.UpdatedAt.UTC(),
	}
	if c.DataSize == 0 {
		c.DataSize = int64(len(e.Data)) // payload size is still metadata
	}
	return c
}

// toModel converts back to the domain type; Data is always empty.
func (c cachedEntry) toModel() *model.Entry {
	return &model.Entry{
		ID:        c.ID,
		Type:      model.EntryType(c.Type),
		Label:     c.Label,
		Metadata:  c.Metadata,
		DataSize:  c.DataSize,
		Version:   c.Version,
		CreatedAt: c.CreatedAt.UTC(),
		UpdatedAt: c.UpdatedAt.UTC(),
	}
}
