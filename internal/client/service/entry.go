package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"

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
)

// EntryService is the client-side entry management business logic.
// It validates entries against the same rules as the server (fast
// client-side feedback) and delegates everything else to the
// gateway; gateway sentinels (gateway.ErrNotFound,
// gateway.ErrConflict, gateway.ErrUnauthenticated) propagate
// unchanged so the CLI can map them to friendly messages.
type EntryService struct {
	gw gateway.EntryGateway
}

// NewEntryService returns an EntryService working through gw, which
// must not be nil.
func NewEntryService(gw gateway.EntryGateway) *EntryService {
	return &EntryService{gw: gw}
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
		return nil, wrapEntryErr("create", err)
	}
	return created, nil
}

// List returns all entries of the authenticated user without payload
// bytes: entries come with metadata and DataSize, the content of a
// concrete entry is fetched with Get or Download. This keeps List
// usable with large binary entries.
func (s *EntryService) List(ctx context.Context) ([]*model.Entry, error) {
	entries, err := s.gw.List(ctx, false)
	if err != nil {
		return nil, wrapEntryErr("list", err)
	}
	return entries, nil
}

// Get returns a single entry by id, or an error wrapping
// gateway.ErrNotFound when it does not exist.
func (s *EntryService) Get(ctx context.Context, id string) (*model.Entry, error) {
	if id == "" {
		return nil, ErrEmptyEntryID
	}
	entry, err := s.gw.Get(ctx, id)
	if err != nil {
		return nil, wrapEntryErr("get", err)
	}
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
		return nil, wrapEntryErr("update", err)
	}
	return updated, nil
}

// Remove deletes an entry by id, or returns an error wrapping
// gateway.ErrNotFound when it does not exist.
func (s *EntryService) Remove(ctx context.Context, id string) error {
	if id == "" {
		return ErrEmptyEntryID
	}
	if err := s.gw.Delete(ctx, id); err != nil {
		return wrapEntryErr("delete", err)
	}
	return nil
}

// Sync returns the full current set of the user's entries without
// payload bytes (metadata and DataSize only), used by the CLI/TUI to
// reconcile the local view with the server.
func (s *EntryService) Sync(ctx context.Context) ([]*model.Entry, error) {
	entries, err := s.gw.Sync(ctx, false)
	if err != nil {
		return nil, wrapEntryErr("sync", err)
	}
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
		return nil, wrapEntryErr("upload", err)
	}
	return stored, nil
}

// Download streams the payload of the entry with the given id into w
// without holding it fully in memory. It works for both inline and
// chunked entries. io.EOF is never returned for a successful download.
func (s *EntryService) Download(ctx context.Context, id string, w io.Writer) error {
	if id == "" {
		return ErrEmptyEntryID
	}
	stream, err := s.gw.DownloadEntryData(ctx, id)
	if err != nil {
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
