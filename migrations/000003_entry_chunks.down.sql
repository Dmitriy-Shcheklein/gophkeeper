DROP TABLE entry_chunks;
DROP INDEX idx_entries_user_state;
ALTER TABLE entries
    DROP COLUMN state,
    DROP COLUMN data_size;
