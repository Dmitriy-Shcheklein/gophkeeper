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

// entryColumns is the column list shared by the read queries. data is
// included when payloads are requested; data_size always comes along
// so payload-less reads can still report sizes.
const (
	entryColumnsData    = `id::text, user_id::text, type, label, COALESCE(metadata, ''), data, data_size, version, created_at, updated_at, state`
	entryColumnsNoData  = `id::text, user_id::text, type, label, COALESCE(metadata, ''), ''::bytea AS data, data_size, version, created_at, updated_at, state`
	entryStateReadyOnly = ` AND state = 1` // 1=ready; pending uploads are invisible
)

// Create inserts a new entry, filling the generated identifier, initial
// version and timestamps back into entry.
func (r *entryRepository) Create(ctx context.Context, entry *model.Entry) error {
	const q = `
		INSERT INTO entries (user_id, type, label, metadata, data, data_size)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5, octet_length($5::bytea))
		RETURNING id::text, version, created_at, updated_at`

	err := r.pool.QueryRow(ctx, q,
		entry.UserID, entryTypeToDB(entry.Type), entry.Label, entry.Metadata, entry.Data,
	).Scan(&entry.ID, &entry.Version, &entry.CreatedAt, &entry.UpdatedAt)
	if err != nil {
		return fmt.Errorf("postgres: create entry: %w", err)
	}
	entry.DataSize = int64(len(entry.Data))
	entry.State = model.EntryStateReady
	return nil
}

// GetByID returns the entry with the given identifier owned by the given
// user. Pending entries are invisible. For chunked entries Data is
// empty and DataSize reports the content size.
func (r *entryRepository) GetByID(ctx context.Context, userID, entryID string) (*model.Entry, error) {
	const q = `
		SELECT ` + entryColumnsData + `
		FROM entries
		WHERE id = $1 AND user_id = $2` + entryStateReadyOnly

	entry, err := scanEntry(r.pool.QueryRow(ctx, q, entryID, userID))
	if err != nil {
		return nil, fmt.Errorf("postgres: get entry %q: %w", entryID, mapNoRows(err))
	}
	return entry, nil
}

// List returns the entries of the given user, optionally filtered by
// type, ordered by creation time and identifier. Pending entries are
// invisible. When includeData is false the payload column is not
// transferred at all.
func (r *entryRepository) List(ctx context.Context, userID string, entryType *model.EntryType, includeData bool) ([]*model.Entry, error) {
	columns := entryColumnsNoData
	if includeData {
		columns = entryColumnsData
	}
	q := `
		SELECT ` + columns + `
		FROM entries
		WHERE user_id = $1` + entryStateReadyOnly
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
		    data_size = octet_length($6::bytea),
		    version = version + 1, updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND version = $3 AND state = 1
		RETURNING version, updated_at`

	err := r.pool.QueryRow(ctx, q,
		entry.ID, entry.UserID, entry.Version, entry.Label, entry.Metadata, entry.Data,
	).Scan(&entry.Version, &entry.UpdatedAt)
	if err == nil {
		entry.DataSize = int64(len(entry.Data))
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("postgres: update entry %q: %w", entry.ID, err)
	}

	const existsQ = `SELECT 1 FROM entries WHERE id = $1 AND user_id = $2 AND state = 1`
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

// CreatePending inserts an invisible (state=pending) entry for a
// chunked upload; the payload will arrive as chunks.
func (r *entryRepository) CreatePending(ctx context.Context, entry *model.Entry) error {
	const q = `
		INSERT INTO entries (user_id, type, label, metadata, data, data_size, state)
		VALUES ($1, $2, $3, NULLIF($4, ''), ''::bytea, 0, 0)
		RETURNING id::text, version, created_at, updated_at`

	err := r.pool.QueryRow(ctx, q,
		entry.UserID, entryTypeToDB(entry.Type), entry.Label, entry.Metadata,
	).Scan(&entry.ID, &entry.Version, &entry.CreatedAt, &entry.UpdatedAt)
	if err != nil {
		return fmt.Errorf("postgres: create pending entry: %w", err)
	}
	entry.State = model.EntryStatePending
	entry.DataSize = 0
	return nil
}

// AppendChunk stores one payload chunk of an entry being uploaded.
func (r *entryRepository) AppendChunk(ctx context.Context, entryID string, seq int, data []byte) error {
	const q = `
		INSERT INTO entry_chunks (entry_id, seq, data)
		VALUES ($1, $2, $3)`

	_, err := r.pool.Exec(ctx, q, entryID, seq, data)
	if err != nil {
		return fmt.Errorf("postgres: append chunk %d to entry %q: %w", seq, entryID, err)
	}
	return nil
}

// FinalizeCreate completes a chunked upload of a new entry: the entry
// becomes visible and its size is computed from the stored chunks.
func (r *entryRepository) FinalizeCreate(ctx context.Context, entry *model.Entry) error {
	const q = `
		UPDATE entries
		SET state = 1,
		    data_size = (SELECT COALESCE(SUM(octet_length(data)), 0) FROM entry_chunks WHERE entry_id = id),
		    updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND state = 0
		RETURNING version, data_size, updated_at`

	err := r.pool.QueryRow(ctx, q, entry.ID, entry.UserID).
		Scan(&entry.Version, &entry.DataSize, &entry.UpdatedAt)
	if err != nil {
		return fmt.Errorf("postgres: finalize entry %q: %w", entry.ID, mapNoRows(err))
	}
	entry.State = model.EntryStateReady
	return nil
}

// PrepareUpdate verifies ownership and the optimistic-lock version of a
// chunked update target and reports how many chunks are already stored
// (0 for inline entries). The entry stays visible and unchanged until
// FinalizeUpdate.
func (r *entryRepository) PrepareUpdate(ctx context.Context, entry *model.Entry) (int, error) {
	const q = `
		SELECT version, (SELECT COUNT(*) FROM entry_chunks WHERE entry_id = entries.id)
		FROM entries
		WHERE id = $1 AND user_id = $2 AND state = 1`

	var storedVersion int64
	var chunkCount int
	err := r.pool.QueryRow(ctx, q, entry.ID, entry.UserID).
		Scan(&storedVersion, &chunkCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("postgres: prepare update entry %q: %w", entry.ID, model.ErrNotFound)
	}
	if err != nil {
		return 0, fmt.Errorf("postgres: prepare update entry %q: %w", entry.ID, err)
	}
	if storedVersion != entry.Version {
		return 0, fmt.Errorf("postgres: prepare update entry %q: %w", entry.ID, model.ErrConflict)
	}
	return chunkCount, nil
}

// FinalizeUpdate completes a chunked update started with
// PrepareUpdate: in one transaction it re-checks the optimistic-lock
// version, deletes the old chunks (everything below chunkOffset),
// renumbers the new chunks to start at 0 (the storage invariant:
// chunks are always 0-based, which the download path relies on),
// clears the inline payload and bumps the version. Fills version,
// updated_at and DataSize back into entry. Same error semantics as
// Update.
func (r *entryRepository) FinalizeUpdate(ctx context.Context, entry *model.Entry, chunkOffset int) error {
	const lockQ = `
		SELECT version FROM entries
		WHERE id = $1 AND user_id = $2 AND state = 1
		FOR UPDATE`
	const dropOldQ = `DELETE FROM entry_chunks WHERE entry_id = $1 AND seq < $2`
	const renumberQ = `UPDATE entry_chunks SET seq = seq - $2 WHERE entry_id = $1 AND seq >= $2`
	const applyQ = `
		UPDATE entries
		SET label = $2, metadata = NULLIF($3, ''), data = ''::bytea,
		    data_size = (SELECT COALESCE(SUM(octet_length(data)), 0) FROM entry_chunks WHERE entry_id = id),
		    version = version + 1, updated_at = NOW()
		WHERE id = $1
		RETURNING version, data_size, updated_at`

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: finalize update entry %q: %w", entry.ID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var storedVersion int64
	err = tx.QueryRow(ctx, lockQ, entry.ID, entry.UserID).Scan(&storedVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("postgres: finalize update entry %q: %w", entry.ID, model.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("postgres: finalize update entry %q: %w", entry.ID, err)
	}
	if storedVersion != entry.Version {
		return fmt.Errorf("postgres: finalize update entry %q: %w", entry.ID, model.ErrConflict)
	}

	if _, err := tx.Exec(ctx, dropOldQ, entry.ID, chunkOffset); err != nil {
		return fmt.Errorf("postgres: finalize update entry %q: %w", entry.ID, err)
	}
	if _, err := tx.Exec(ctx, renumberQ, entry.ID, chunkOffset); err != nil {
		return fmt.Errorf("postgres: finalize update entry %q: %w", entry.ID, err)
	}

	err = tx.QueryRow(ctx, applyQ, entry.ID, entry.Label, entry.Metadata).
		Scan(&entry.Version, &entry.DataSize, &entry.UpdatedAt)
	if err != nil {
		return fmt.Errorf("postgres: finalize update entry %q: %w", entry.ID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: finalize update entry %q: %w", entry.ID, err)
	}
	return nil
}

// AbortUpdate discards chunks appended after chunkOffset, leaving the
// entry in its pre-update state. Missing chunks are not an error.
func (r *entryRepository) AbortUpdate(ctx context.Context, entryID string, chunkOffset int) error {
	const q = `DELETE FROM entry_chunks WHERE entry_id = $1 AND seq >= $2`

	_, err := r.pool.Exec(ctx, q, entryID, chunkOffset)
	if err != nil {
		return fmt.Errorf("postgres: abort update entry %q: %w", entryID, err)
	}
	return nil
}

// DeleteIfPending removes the entry if it is still a pending upload,
// cascading its chunks. Ready and missing entries are left untouched.
func (r *entryRepository) DeleteIfPending(ctx context.Context, entryID string) error {
	const q = `DELETE FROM entries WHERE id = $1 AND state = 0`

	_, err := r.pool.Exec(ctx, q, entryID)
	if err != nil {
		return fmt.Errorf("postgres: delete pending entry %q: %w", entryID, err)
	}
	return nil
}

// ChunkCount returns the number of stored chunks of the ready entry
// owned by the user (0 for inline entries). model.ErrNotFound when the
// entry does not exist.
func (r *entryRepository) ChunkCount(ctx context.Context, userID, entryID string) (int, error) {
	const q = `
		SELECT (SELECT COUNT(*) FROM entry_chunks WHERE entry_id = e.id)
		FROM entries e
		WHERE e.id = $1 AND e.user_id = $2 AND e.state = 1`

	var count int
	err := r.pool.QueryRow(ctx, q, entryID, userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("postgres: chunk count entry %q: %w", entryID, mapNoRows(err))
	}
	return count, nil
}

// Chunk returns one stored chunk of a ready entry owned by the user.
// model.ErrNotFound when there is no such chunk.
func (r *entryRepository) Chunk(ctx context.Context, userID, entryID string, seq int) ([]byte, error) {
	const q = `
		SELECT c.data
		FROM entry_chunks c
		JOIN entries e ON e.id = c.entry_id
		WHERE c.entry_id = $1 AND c.seq = $2 AND e.user_id = $3 AND e.state = 1`

	var data []byte
	err := r.pool.QueryRow(ctx, q, entryID, seq, userID).Scan(&data)
	if err != nil {
		return nil, fmt.Errorf("postgres: chunk %d of entry %q: %w", seq, entryID, mapNoRows(err))
	}
	return data, nil
}

// scanEntry scans a single entry row returned by one of the read queries.
func scanEntry(row pgx.Row) (*model.Entry, error) {
	var (
		entry    model.Entry
		dbType   int16
		dbState  int16
		dataSize int64
		version  int64
	)
	err := row.Scan(
		&entry.ID, &entry.UserID, &dbType, &entry.Label, &entry.Metadata,
		&entry.Data, &dataSize, &version, &entry.CreatedAt, &entry.UpdatedAt,
		&dbState,
	)
	if err != nil {
		return nil, err
	}
	entry.Type = entryTypeFromDB(dbType)
	entry.DataSize = dataSize
	entry.Version = version
	entry.State = model.EntryState(dbState)
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
