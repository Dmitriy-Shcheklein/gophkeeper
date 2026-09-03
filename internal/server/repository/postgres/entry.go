package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitriy/gophkeeper/internal/server/model"
)

// entryRepository is a PostgreSQL implementation of
// repository.EntryRepository.
type entryRepository struct {
	pool *pgxpool.Pool
}

// Create inserts a new entry, filling the generated identifier, initial
// version and timestamps back into entry.
func (r *entryRepository) Create(ctx context.Context, entry *model.Entry) error {
	const q = `
		INSERT INTO entries (user_id, type, label, metadata, data)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5)
		RETURNING id::text, version, created_at, updated_at`

	err := r.pool.QueryRow(ctx, q,
		entry.UserID, entryTypeToDB(entry.Type), entry.Label, entry.Metadata, entry.Data,
	).Scan(&entry.ID, &entry.Version, &entry.CreatedAt, &entry.UpdatedAt)
	if err != nil {
		return fmt.Errorf("postgres: create entry: %w", err)
	}
	return nil
}

// GetByID returns the entry with the given identifier owned by the given
// user.
func (r *entryRepository) GetByID(ctx context.Context, userID, entryID string) (*model.Entry, error) {
	const q = `
		SELECT id::text, user_id::text, type, label, COALESCE(metadata, ''),
		       data, version, created_at, updated_at
		FROM entries
		WHERE id = $1 AND user_id = $2`

	entry, err := scanEntry(r.pool.QueryRow(ctx, q, entryID, userID))
	if err != nil {
		return nil, fmt.Errorf("postgres: get entry %q: %w", entryID, mapNoRows(err))
	}
	return entry, nil
}

// List returns the entries of the given user, optionally filtered by
// type, ordered by creation time and identifier.
func (r *entryRepository) List(ctx context.Context, userID string, entryType *model.EntryType) ([]*model.Entry, error) {
	q := `
		SELECT id::text, user_id::text, type, label, COALESCE(metadata, ''),
		       data, version, created_at, updated_at
		FROM entries
		WHERE user_id = $1`
	args := []any{userID}
	if entryType != nil {
		q += " AND type = $" + strconv.Itoa(len(args)+1)
		args = append(args, entryTypeToDB(*entryType))
	}
	q += " ORDER BY created_at, id"

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: list entries: %w", err)
	}
	defer rows.Close()

	var entries []*model.Entry
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: list entries: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list entries: %w", err)
	}
	return entries, nil
}

// Update modifies mutable fields of the entry using optimistic locking:
// the statement applies only if the stored version matches
// entry.Version, which is then incremented. On success the new version
// and updated timestamp are filled back into entry. If no row was
// affected, the entry is looked up to distinguish a version conflict
// (model.ErrConflict) from a missing entry (model.ErrNotFound).
func (r *entryRepository) Update(ctx context.Context, entry *model.Entry) error {
	const q = `
		UPDATE entries
		SET label = $4, metadata = NULLIF($5, ''), data = $6,
		    version = version + 1, updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND version = $3
		RETURNING version, updated_at`

	err := r.pool.QueryRow(ctx, q,
		entry.ID, entry.UserID, entry.Version, entry.Label, entry.Metadata, entry.Data,
	).Scan(&entry.Version, &entry.UpdatedAt)
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("postgres: update entry %q: %w", entry.ID, err)
	}

	const existsQ = `SELECT 1 FROM entries WHERE id = $1 AND user_id = $2`
	var one int
	err = r.pool.QueryRow(ctx, existsQ, entry.ID, entry.UserID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("postgres: update entry %q: %w", entry.ID, model.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("postgres: update entry %q: %w", entry.ID, err)
	}
	return fmt.Errorf("postgres: update entry %q: %w", entry.ID, model.ErrConflict)
}

// Delete removes the entry with the given identifier owned by the given
// user.
func (r *entryRepository) Delete(ctx context.Context, userID, entryID string) error {
	const q = `DELETE FROM entries WHERE id = $1 AND user_id = $2`

	tag, err := r.pool.Exec(ctx, q, entryID, userID)
	if err != nil {
		return fmt.Errorf("postgres: delete entry %q: %w", entryID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("postgres: delete entry %q: %w", entryID, model.ErrNotFound)
	}
	return nil
}

// scanEntry scans a single entry row returned by one of the read queries.
func scanEntry(row pgx.Row) (*model.Entry, error) {
	var (
		entry   model.Entry
		dbType  int16
		version int64
	)
	err := row.Scan(
		&entry.ID, &entry.UserID, &dbType, &entry.Label, &entry.Metadata,
		&entry.Data, &version, &entry.CreatedAt, &entry.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	entry.Type = entryTypeFromDB(dbType)
	entry.Version = version
	return &entry, nil
}

// entryTypeToDB converts a domain entry type to its SMALLINT
// representation.
func entryTypeToDB(t model.EntryType) int16 {
	return int16(t)
}

// entryTypeFromDB converts a SMALLINT value back to a domain entry type.
func entryTypeFromDB(v int16) model.EntryType {
	return model.EntryType(v)
}
