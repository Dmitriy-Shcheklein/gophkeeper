package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/dmitriy/gophkeeper/internal/server/model"
	"github.com/dmitriy/gophkeeper/internal/server/repository"
)

// Input validation limits and errors of EntryService.
var (
	// ErrEmptyUserID is returned when the user identifier is missing.
	// Normally impossible: the transport layer fills it from the
	// authenticated identity, the check is purely defensive.
	ErrEmptyUserID = errors.New("user id must not be empty")
	// ErrEmptyEntryID is returned by Get, Update and Delete when the
	// entry identifier is empty.
	ErrEmptyEntryID = errors.New("entry id must not be empty")
	// ErrInvalidEntryType is returned when the entry type is not one of
	// the supported model.EntryType values.
	ErrInvalidEntryType = errors.New("invalid entry type")
	// ErrEmptyLabel is returned when the entry label is empty.
	ErrEmptyLabel = errors.New("label must not be empty")
	// ErrLabelTooLong is returned when the label exceeds maxLabelLen
	// bytes.
	ErrLabelTooLong = errors.New("label is too long")
	// ErrEmptyData is returned when the secret payload is empty. Every
	// entry type carries its payload in Data (for login/password it is
	// the serialized pair, for text the note itself, for binary the
	// file content, for card the serialized card record), so an entry
	// without data is meaningless and rejected.
	ErrEmptyData = errors.New("data must not be empty")
	// ErrMetadataTooLong is returned when the optional metadata exceeds
	// maxMetadataLen bytes.
	ErrMetadataTooLong = errors.New("metadata is too long")
	// ErrInvalidVersion is returned when an update carries a version
	// below 1, which can never match a stored entry and would make the
	// optimistic-lock precondition meaningless.
	ErrInvalidVersion = errors.New("version must be at least 1")
)

const (
	// maxLabelLen is the maximum allowed label length in bytes.
	maxLabelLen = 255
	// maxMetadataLen is the maximum allowed metadata length in bytes.
	// Metadata is auxiliary (e.g. a serialized card number or URL), so
	// a generous but bounded cap keeps entries small without being
	// restrictive in practice.
	maxMetadataLen = 10000
)

// EntryService implements the business logic for entry CRUD and
// synchronization on top of repository.EntryRepository.
//
// Validation rules:
//   - UserID must be non-empty (defensive; the transport layer sets it
//     from the authenticated identity and it always wins over any UserID
//     carried inside the entry);
//   - Type must be a valid model.EntryType;
//   - Label must be non-empty and at most maxLabelLen bytes;
//   - Data must be non-empty for every entry type, since Data holds the
//     secret payload regardless of type;
//   - Metadata is optional but at most maxMetadataLen bytes when present.
//
// Type is immutable after creation: repository.Update ignores it, so
// Update only validates and applies label, metadata and data.
type EntryService struct {
	entries repository.EntryRepository
}

// NewEntryService creates an EntryService backed by the given entry
// repository.
func NewEntryService(entries repository.EntryRepository) *EntryService {
	return &EntryService{entries: entries}
}

// Create validates the entry and stores it for the given user. On
// success the returned entry is the same object filled in by the
// repository with the generated identifier, initial version 1 and
// timestamps. Repository errors are propagated as-is.
func (s *EntryService) Create(ctx context.Context, userID string, entry *model.Entry) (*model.Entry, error) {
	if err := validateUserID(userID); err != nil {
		return nil, err
	}
	if err := validateEntry(entry); err != nil {
		return nil, err
	}

	entry.UserID = userID
	if err := s.entries.Create(ctx, entry); err != nil {
		return nil, fmt.Errorf("service: create entry: %w", err)
	}
	return entry, nil
}

// Get returns the entry with the given identifier owned by the given
// user, or model.ErrNotFound if it does not exist.
func (s *EntryService) Get(ctx context.Context, userID, entryID string) (*model.Entry, error) {
	if err := validateUserID(userID); err != nil {
		return nil, err
	}
	if entryID == "" {
		return nil, ErrEmptyEntryID
	}

	entry, err := s.entries.GetByID(ctx, userID, entryID)
	if err != nil {
		return nil, fmt.Errorf("service: get entry: %w", err)
	}
	return entry, nil
}

// List returns the entries of the given user, optionally filtered by
// type. A non-nil filter must be a valid model.EntryType. Repository
// errors are propagated as-is.
func (s *EntryService) List(ctx context.Context, userID string, entryType *model.EntryType) ([]*model.Entry, error) {
	if err := validateUserID(userID); err != nil {
		return nil, err
	}
	if entryType != nil && !entryType.Valid() {
		return nil, ErrInvalidEntryType
	}

	entries, err := s.entries.List(ctx, userID, entryType)
	if err != nil {
		return nil, fmt.Errorf("service: list entries: %w", err)
	}
	return entries, nil
}

// Update applies label, metadata and data changes to the entry guarded
// by optimistic locking: entry.Version must match the stored version,
// otherwise model.ErrConflict is returned (the caller holds a stale
// copy and must re-fetch). model.ErrNotFound is returned when the entry
// does not exist for the user. On success the entry is filled back by
// the repository with the bumped version and new updated timestamp.
//
// The entry Type is immutable and ignored by the repository; it is
// validated only when non-zero to catch obviously broken input early.
func (s *EntryService) Update(ctx context.Context, userID string, entry *model.Entry) (*model.Entry, error) {
	if err := validateUserID(userID); err != nil {
		return nil, err
	}
	if entry.ID == "" {
		return nil, ErrEmptyEntryID
	}
	if entry.Version < 1 {
		return nil, ErrInvalidVersion
	}
	if entry.Type != 0 && !entry.Type.Valid() {
		return nil, ErrInvalidEntryType
	}
	if err := validateEntryContent(entry); err != nil {
		return nil, err
	}

	entry.UserID = userID
	if err := s.entries.Update(ctx, entry); err != nil {
		return nil, fmt.Errorf("service: update entry: %w", err)
	}
	return entry, nil
}

// Delete removes the entry with the given identifier owned by the given
// user, or returns model.ErrNotFound if it does not exist.
func (s *EntryService) Delete(ctx context.Context, userID, entryID string) error {
	if err := validateUserID(userID); err != nil {
		return err
	}
	if entryID == "" {
		return ErrEmptyEntryID
	}

	if err := s.entries.Delete(ctx, userID, entryID); err != nil {
		return fmt.Errorf("service: delete entry: %w", err)
	}
	return nil
}

// Sync returns the full entry state of the given user (all types,
// ordered by creation time), used by the transport-level Sync RPC to
// reconcile client copies.
func (s *EntryService) Sync(ctx context.Context, userID string) ([]*model.Entry, error) {
	return s.List(ctx, userID, nil)
}

// validateUserID enforces the non-empty user identifier.
func validateUserID(userID string) error {
	if userID == "" {
		return ErrEmptyUserID
	}
	return nil
}

// validateEntry checks an entry being created: type, label, metadata
// and data.
func validateEntry(entry *model.Entry) error {
	if entry == nil {
		return ErrEmptyData
	}
	if !entry.Type.Valid() {
		return ErrInvalidEntryType
	}
	return validateEntryContent(entry)
}

// validateEntryContent checks the mutable content fields shared by
// Create and Update: label, metadata and data.
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
