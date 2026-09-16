package service

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"

	"github.com/dmitriy/gophkeeper/internal/server/model"
)

// Upload limits.
const (
	// DefaultMaxDataSize is the maximum entry payload size accepted by
	// an EntryService created without explicit limits (1 GiB).
	DefaultMaxDataSize = int64(1 << 30)
	// MaxChunkSize is the maximum size of a single upload chunk (1 MiB).
	// A client sending larger chunks gets the upload rejected.
	MaxChunkSize = 1 << 20
)

// Upload errors.
var (
	// ErrChunkTooLarge is returned when a single upload chunk exceeds
	// MaxChunkSize.
	ErrChunkTooLarge = errors.New("upload chunk is too large")
	// ErrDataTooLarge is returned when the total payload size exceeds
	// the configured maximum.
	ErrDataTooLarge = errors.New("data exceeds the maximum allowed size")
	// ErrChecksumMismatch is returned when the SHA-256 digest sent with
	// the upload footer does not match the received payload.
	ErrChecksumMismatch = errors.New("checksum mismatch")
	// ErrEmptyChunkedData is returned when an upload finishes without
	// any payload bytes.
	ErrEmptyChunkedData = errors.New("uploaded data must not be empty")
)

// UploadSession is one chunked upload in progress. The transport layer
// drives it: BeginUpload creates the session, every received chunk goes
// through AddChunk, the footer digest and Commit finalize it and Abort
// rolls everything back when the stream fails midway.
type UploadSession interface {
	// AddChunk appends one payload chunk. ctx is the upload stream
	// context: it is cancelled when the client disconnects, letting
	// the in-flight database write stop early. Errors abort the
	// eligibility of the session: after a failed AddChunk the
	// transport must Abort.
	AddChunk(ctx context.Context, data []byte) error
	// Commit verifies the hex-encoded SHA-256 digest of the whole
	// payload and finalizes the upload, returning the stored entry.
	Commit(ctx context.Context, sha256hex string) (*model.Entry, error)
	// Abort rolls the upload back: a not-yet-finalized entry is
	// deleted, an in-progress update is reset to its pre-update state.
	Abort(ctx context.Context)
}

// ChunkedUpload is the service-side state of one chunked upload.
type ChunkedUpload struct {
	svc *EntryService

	entry       *model.Entry // create: pending row; update: existing row
	isUpdate    bool
	chunkOffset int // update: first free chunk sequence
	seq         int // next chunk sequence number
	hash        hash.Hash
	size        int64
}

// AddChunk appends one payload chunk to the pending entry, enforcing
// the per-chunk and total size limits. The digest is computed over the
// chunks as they are accepted. ctx is the upload stream context, so a
// client disconnect cancels the pending database write.
func (u *ChunkedUpload) AddChunk(ctx context.Context, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if len(data) > MaxChunkSize {
		return ErrChunkTooLarge
	}
	if u.size+int64(len(data)) > u.svc.maxDataSize {
		return ErrDataTooLarge
	}
	if _, err := u.hash.Write(data); err != nil {
		return fmt.Errorf("service: hash chunk: %w", err)
	}
	if err := u.svc.entries.AppendChunk(ctx, u.entry.ID, u.seq, data); err != nil {
		return fmt.Errorf("service: append chunk: %w", err)
	}
	u.seq++
	u.size += int64(len(data))
	return nil
}

// Commit verifies the payload digest and finalizes the upload. For a
// new entry it makes the pending row visible; for an update it swaps
// the old payload for the streamed one under the optimistic-lock
// version. The returned entry is the full stored state.
func (u *ChunkedUpload) Commit(ctx context.Context, sha256hex string) (*model.Entry, error) {
	if u.size == 0 {
		return nil, ErrEmptyChunkedData
	}
	if err := verifyDigest(u.hash, sha256hex); err != nil {
		return nil, err
	}

	if u.isUpdate {
		if err := u.svc.entries.FinalizeUpdate(ctx, u.entry, u.chunkOffset); err != nil {
			return nil, fmt.Errorf("service: finalize update: %w", err)
		}
	} else {
		if err := u.svc.entries.FinalizeCreate(ctx, u.entry); err != nil {
			return nil, fmt.Errorf("service: finalize create: %w", err)
		}
	}

	stored, err := u.svc.Get(ctx, u.entry.UserID, u.entry.ID)
	if err != nil {
		return nil, fmt.Errorf("service: reload uploaded entry: %w", err)
	}
	return stored, nil
}

// Abort rolls the upload back. It is safe to call after a failed
// Commit as well: the cleanup only touches the parts the session
// created.
func (u *ChunkedUpload) Abort(ctx context.Context) {
	if u.isUpdate {
		_ = u.svc.entries.AbortUpdate(ctx, u.entry.ID, u.chunkOffset)
		return
	}
	_ = u.svc.entries.DeleteIfPending(ctx, u.entry.ID)
}

// verifyDigest compares the hex-encoded SHA-256 digest with the hash
// accumulated over the received chunks.
func verifyDigest(received hash.Hash, sha256hex string) error {
	expected := received.Sum(nil)
	given, err := hex.DecodeString(sha256hex)
	if err != nil || len(given) != len(expected) ||
		subtle.ConstantTimeCompare(given, expected) != 1 {
		return ErrChecksumMismatch
	}
	return nil
}

// BeginUpload starts a chunked upload for the user: header carries the
// entry metadata (Type, Label, Metadata and, for updates, ID) and the
// optimistic-lock version (0 creates a new entry). The returned session
// must be finished with either Commit or Abort.
func (s *EntryService) BeginUpload(ctx context.Context, userID string, header *model.Entry, expectedVersion int64) (UploadSession, error) {
	if err := validateUserID(userID); err != nil {
		return nil, err
	}
	if header == nil {
		return nil, ErrEmptyData
	}
	if !header.Type.Valid() {
		return nil, ErrInvalidEntryType
	}
	if header.Label == "" {
		return nil, ErrEmptyLabel
	}
	if len(header.Label) > maxLabelLen {
		return nil, ErrLabelTooLong
	}
	if len(header.Metadata) > maxMetadataLen {
		return nil, ErrMetadataTooLong
	}

	if expectedVersion < 0 {
		return nil, ErrInvalidVersion
	}

	if expectedVersion == 0 {
		entry := &model.Entry{
			UserID:   userID,
			Type:     header.Type,
			Label:    header.Label,
			Metadata: header.Metadata,
		}
		if err := s.entries.CreatePending(ctx, entry); err != nil {
			return nil, fmt.Errorf("service: begin upload: %w", err)
		}
		return &ChunkedUpload{svc: s, entry: entry, hash: sha256.New()}, nil
	}

	if err := validateEntryID(header.ID); err != nil {
		return nil, err
	}
	target := &model.Entry{
		ID:       header.ID,
		UserID:   userID,
		Version:  expectedVersion,
		Label:    header.Label,
		Metadata: header.Metadata,
	}
	offset, err := s.entries.PrepareUpdate(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("service: begin upload: %w", err)
	}
	return &ChunkedUpload{
		svc:         s,
		entry:       target,
		isUpdate:    true,
		chunkOffset: offset,
		seq:         offset, // continue after the stored chunks
		hash:        sha256.New(),
	}, nil
}
