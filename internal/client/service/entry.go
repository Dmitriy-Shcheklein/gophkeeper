package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/dmitriy/gophkeeper/internal/client/cache"
	"github.com/dmitriy/gophkeeper/internal/client/gateway"
	"github.com/dmitriy/gophkeeper/internal/client/model"
)

// Entry validation limits, mirroring the server-side service rules
// (internal/server/service/entry.go) so invalid input fails fast on
// the client with the same semantics.
const (
	// maxLabelLen is the maximum allowed label length in bytes.
	maxLabelLen = 255
	// maxMetadataLen is the maximum allowed metadata length in bytes.
	maxMetadataLen = 10000
)

// Entry validation errors: returned by EntryService before any
// network activity so the user gets immediate feedback. Their
// wording mirrors the server-side sentinels; the CLI maps them to
// friendly messages.
var (
	// ErrNilEntry is returned by Add and Edit when the entry argument
	// is nil: there is nothing to validate or send, and reporting it
	// as a label or id problem would mislead the caller.
	ErrNilEntry = errors.New("entry must not be nil")
	// ErrEmptyEntryID is returned by Get, Edit and Remove when the
	// entry id is empty.
	ErrEmptyEntryID = errors.New("entry id must not be empty")
	// ErrInvalidEntryType is returned when the entry type is not one
	// of the supported model.EntryType values.
	ErrInvalidEntryType = errors.New("invalid entry type")
	// ErrEmptyLabel is returned when the entry label is empty.
	ErrEmptyLabel = errors.New("label must not be empty")
	// ErrLabelTooLong is returned when the label exceeds maxLabelLen
	// bytes.
	ErrLabelTooLong = errors.New("label is too long")
	// ErrEmptyData is returned when the secret payload is empty.
	// Every entry kind carries a payload, so there is nothing
	// meaningful to store without it.
	ErrEmptyData = errors.New("data must not be empty")
	// ErrMetadataTooLong is returned when the optional metadata
	// exceeds maxMetadataLen bytes.
	ErrMetadataTooLong = errors.New("metadata is too long")
	// ErrInvalidVersion is returned when an update carries a version
	// below 1: versions are server-assigned and start at 1.
	ErrInvalidVersion = errors.New("version must be at least 1")
	// ErrOffline marks results served from the local offline cache
	// while the server is unreachable (read operations) or rejects an
	// operation that needs the server (writes are not supported
	// offline); use errors.Is to detect it.
	ErrOffline = errors.New("server is unreachable")
)

// EntryService is the client-side entry management business logic.
// It validates entries against the same rules as the server (fast
// client-side feedback) and delegates everything else to the
// gateway; gateway sentinels (gateway.ErrNotFound,
// gateway.ErrConflict, gateway.ErrUnauthenticated) propagate
// unchanged so the CLI can map them to friendly messages.
//
// With a cache store attached (NewEntryServiceWithCache) the service
// also implements the offline read mode: after every successful read
// the cache snapshot is refreshed, and when the server is
// unreachable List/Sync/Get serve the cached data with an error
// wrapping ErrOffline. Write operations always require the server.
type EntryService struct {
	gw gateway.EntryGateway
	// cache is the optional offline snapshot store; nil disables the
	// offline mode (every failure propagates to the caller).
	cache *cache.Store
}

// NewEntryService returns an EntryService working through gw without
// the offline cache (every gateway failure propagates). gw must not
// be nil.
func NewEntryService(gw gateway.EntryGateway) *EntryService {
	return &EntryService{gw: gw}
}

// NewEntryServiceWithCache returns an EntryService with the offline
// read mode backed by c. gw must not be nil; a nil c is equivalent
// to NewEntryService.
func NewEntryServiceWithCache(gw gateway.EntryGateway, c *cache.Store) *EntryService {
	return &EntryService{gw: gw, cache: c}
}

// Add stores a new entry and returns the server's copy with the
// assigned id, version and timestamps. The entry must pass
// validateNewEntry, otherwise a validation sentinel is returned and
// the gateway is not called.
func (s *EntryService) Add(ctx context.Context, entry *model.Entry) (*model.Entry, error) {
	if err := validateNewEntry(entry); err != nil {
		return nil, err
	}
	created, err := s.gw.Create(ctx, entry)
	if err != nil {
		if s.isUnavailable(err) {
			return nil, offlineWriteErr("create")
		}
		return nil, wrapEntryErr("create", err)
	}
	s.cacheUpsert(created)
	return created, nil
}

// List returns all entries of the authenticated user without payload
// bytes: entries come with metadata and DataSize, the content of a
// concrete entry is fetched with Get or Download. This keeps List
// usable with large binary entries.
//
// When the server is unreachable and the offline cache is attached,
// the cached snapshot is served with an error wrapping ErrOffline.
func (s *EntryService) List(ctx context.Context) ([]*model.Entry, error) {
	entries, err := s.gw.List(ctx, false)
	if err != nil {
		if s.isUnavailable(err) {
			return s.cacheAll("list")
		}
		return nil, wrapEntryErr("list", err)
	}
	s.cacheReplace(entries)
	return entries, nil
}

// Get returns a single entry by id, or an error wrapping
// gateway.ErrNotFound when it does not exist. On success the entry is
// refreshed in the offline cache (payload dropped). When the server
// is unreachable, the cached metadata copy is served with an error
// wrapping ErrOffline (the payload itself is never cached).
func (s *EntryService) Get(ctx context.Context, id string) (*model.Entry, error) {
	if id == "" {
		return nil, ErrEmptyEntryID
	}
	entry, err := s.gw.Get(ctx, id)
	if err != nil {
		if s.isUnavailable(err) && s.cache != nil {
			cached, cerr := s.cache.Get(id)
			if cerr != nil {
				return nil, &entryError{op: "get", err: fmt.Errorf("%w: entry is not in the offline cache", ErrOffline)}
			}
			return cached, offlineErr("get", ErrOffline)
		}
		return nil, wrapEntryErr("get", err)
	}
	s.cacheUpsert(entry)
	return entry, nil
}

// Edit replaces the entry's content. The entry must carry a non-empty
// ID and the Version the update is based on (>= 1, optimistic
// locking); an invalid type (when non-zero — the type is immutable on
// update) or content fails validation before the gateway is called.
// On a stale version the gateway returns ErrConflict.
func (s *EntryService) Edit(ctx context.Context, entry *model.Entry) (*model.Entry, error) {
	if err := validateEditedEntry(entry); err != nil {
		return nil, err
	}
	updated, err := s.gw.Update(ctx, entry)
	if err != nil {
		if s.isUnavailable(err) {
			return nil, offlineWriteErr("update")
		}
		return nil, wrapEntryErr("update", err)
	}
	s.cacheUpsert(updated)
	return updated, nil
}

// Remove deletes an entry by id, or returns an error wrapping
// gateway.ErrNotFound when it does not exist.
func (s *EntryService) Remove(ctx context.Context, id string) error {
	if id == "" {
		return ErrEmptyEntryID
	}
	if err := s.gw.Delete(ctx, id); err != nil {
		if s.isUnavailable(err) {
			return offlineWriteErr("delete")
		}
		return wrapEntryErr("delete", err)
	}
	s.cacheDelete(id)
	return nil
}

// Sync returns the full current set of the user's entries without
// payload bytes (metadata and DataSize only), used by the CLI/TUI to
// reconcile the local view with the server. Like List, it refreshes
// the offline cache on success and serves the cache when the server
// is unreachable.
func (s *EntryService) Sync(ctx context.Context) ([]*model.Entry, error) {
	entries, err := s.gw.Sync(ctx, false)
	if err != nil {
		if s.isUnavailable(err) {
			return s.cacheAll("sync")
		}
		return nil, wrapEntryErr("sync", err)
	}
	s.cacheReplace(entries)
	return entries, nil
}

// Upload stores an entry streamed in chunks from r, keeping only one
// gateway.ChunkSize piece in memory at a time. A zero expectedVersion
// creates a new entry (entry.ID is ignored); a positive version
// replaces the payload of the existing entry with the given ID under
// the optimistic-lock version. entry must carry a valid type, label
// and metadata; its Data field is ignored.
func (s *EntryService) Upload(ctx context.Context, entry *model.Entry, expectedVersion int64, r io.Reader) (*model.Entry, error) {
	if entry == nil {
		return nil, ErrNilEntry
	}
	if !entry.Type.Valid() {
		return nil, ErrInvalidEntryType
	}
	if entry.Label == "" {
		return nil, ErrEmptyLabel
	}
	if len(entry.Label) > maxLabelLen {
		return nil, ErrLabelTooLong
	}
	if len(entry.Metadata) > maxMetadataLen {
		return nil, ErrMetadataTooLong
	}
	if expectedVersion < 0 {
		return nil, ErrInvalidVersion
	}
	if expectedVersion > 0 && entry.ID == "" {
		return nil, ErrEmptyEntryID
	}

	stream, err := s.gw.Upload(ctx)
	if err != nil {
		if s.isUnavailable(err) {
			return nil, offlineWriteErr("upload")
		}
		return nil, wrapEntryErr("upload", err)
	}
	if err := stream.SendHeader(entry, expectedVersion); err != nil {
		return nil, wrapEntryErr("upload", err)
	}

	hash := sha256.New()
	buf := make([]byte, gateway.ChunkSize)
	sent := false
	for {
		n, rerr := r.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if _, err := hash.Write(chunk); err != nil {
				return nil, wrapEntryErr("upload", err)
			}
			if err := stream.SendChunk(chunk); err != nil {
				return nil, wrapEntryErr("upload", err)
			}
			sent = true
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			return nil, wrapEntryErr("upload", rerr)
		}
	}
	if !sent {
		return nil, ErrEmptyData
	}

	stored, err := stream.CloseAndCommit(hex.EncodeToString(hash.Sum(nil)))
	if err != nil {
		if s.isUnavailable(err) {
			return nil, offlineWriteErr("upload")
		}
		return nil, wrapEntryErr("upload", err)
	}
	s.cacheUpsert(stored)
	return stored, nil
}

// Download streams the payload of the entry with the given id into w
// without holding it fully in memory. It works for both inline and
// chunked entries. io.EOF is never returned for a successful download.
// Payloads are never cached, so the download always requires the
// server.
func (s *EntryService) Download(ctx context.Context, id string, w io.Writer) error {
	if id == "" {
		return ErrEmptyEntryID
	}
	stream, err := s.gw.DownloadEntryData(ctx, id)
	if err != nil {
		if s.isUnavailable(err) {
			return offlineErr("download", fmt.Errorf("%w: payloads are not cached offline", ErrOffline))
		}
		return wrapEntryErr("download", err)
	}
	defer func() { _ = stream.Close() }()

	if _, err := stream.RecvSize(); err != nil {
		return wrapEntryErr("download", err)
	}
	for {
		chunk, err := stream.RecvChunk()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return wrapEntryErr("download", err)
		}
		if _, err := w.Write(chunk); err != nil {
			return wrapEntryErr("download", err)
		}
	}
}

// validateNewEntry checks an entry being created: type, label,
// metadata and data.
func validateNewEntry(entry *model.Entry) error {
	if entry == nil {
		return ErrNilEntry
	}
	if !entry.Type.Valid() {
		return ErrInvalidEntryType
	}
	return validateEntryContent(entry)
}

// validateEditedEntry checks an entry being updated: non-empty ID,
// version >= 1, valid type when non-zero (mirroring the server, where
// the type is immutable on update) and the shared content rules.
func validateEditedEntry(entry *model.Entry) error {
	if entry == nil {
		return ErrNilEntry
	}
	if entry.ID == "" {
		return ErrEmptyEntryID
	}
	if entry.Version < 1 {
		return ErrInvalidVersion
	}
	if entry.Type != 0 && !entry.Type.Valid() {
		return ErrInvalidEntryType
	}
	return validateEntryContent(entry)
}

// validateEntryContent checks the mutable content fields shared by
// Add and Edit: label, metadata and data.
func validateEntryContent(entry *model.Entry) error {
	if entry.Label == "" {
		return ErrEmptyLabel
	}
	if len(entry.Label) > maxLabelLen {
		return ErrLabelTooLong
	}
	if len(entry.Metadata) > maxMetadataLen {
		return ErrMetadataTooLong
	}
	if len(entry.Data) == 0 {
		return ErrEmptyData
	}
	return nil
}

// wrapEntryErr adds operation context to a gateway error while
// keeping it comparable with errors.Is (the CLI maps the sentinels).
func wrapEntryErr(op string, err error) error {
	return &entryError{op: op, err: err}
}

// isUnavailable reports whether err means "the server could not be
// reached at all" (as opposed to a server-side rejection).
func (s *EntryService) isUnavailable(err error) bool {
	return errors.Is(err, gateway.ErrUnavailable)
}

// cacheAll serves the offline snapshot for a list-style operation.
// Without a cache the original behavior is preserved: the failure
// propagates.
func (s *EntryService) cacheAll(op string) ([]*model.Entry, error) {
	if s.cache == nil {
		return nil, &entryError{op: op, err: gateway.ErrUnavailable}
	}
	return s.cache.All(), offlineErr(op, ErrOffline)
}

// cacheUpsert refreshes the entry in the offline snapshot. Cache
// failures never fail the online operation: the cache is an
// optimization, the server data has already been delivered.
func (s *EntryService) cacheUpsert(entries ...*model.Entry) {
	if s.cache == nil {
		return
	}
	_ = s.cache.Upsert(entries...)
}

// cacheReplace overwrites the offline snapshot with the server state.
// Best-effort, like cacheUpsert.
func (s *EntryService) cacheReplace(entries []*model.Entry) {
	if s.cache == nil {
		return
	}
	_ = s.cache.Replace(entries)
}

// cacheDelete drops the entry from the offline snapshot. Best-effort.
func (s *EntryService) cacheDelete(id string) {
	if s.cache == nil {
		return
	}
	_ = s.cache.Delete(id)
}

// offlineErr wraps a cache-served result so callers can detect the
// offline mode with errors.Is(err, ErrOffline) while still using the
// returned data.
func offlineErr(op string, err error) error {
	return &entryError{op: op, err: fmt.Errorf("%w: served from the offline cache", err)}
}

// offlineWriteErr rejects a write operation that requires the server.
func offlineWriteErr(op string) error {
	return &entryError{op: op, err: fmt.Errorf("%w: offline changes are not supported", ErrOffline)}
}

// entryError carries the failed operation name and the underlying
// gateway error, exposing both through Error and Unwrap.
type entryError struct {
	op  string
	err error
}

// Error returns the operation context plus the underlying message.
func (e *entryError) Error() string {
	return "service: " + e.op + " entry: " + e.err.Error()
}

// Unwrap returns the underlying gateway error so errors.Is reaches
// the sentinels (gateway.ErrConflict and friends).
func (e *entryError) Unwrap() error {
	return e.err
}
