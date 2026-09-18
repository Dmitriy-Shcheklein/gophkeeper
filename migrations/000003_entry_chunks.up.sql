-- Chunked payload storage for large entries:
-- entries stay lightweight metadata rows, big payloads live in
-- entry_chunks and are streamed to clients on demand.
ALTER TABLE entries
    ADD COLUMN state SMALLINT NOT NULL DEFAULT 1, -- 0=pending upload, 1=ready
    ADD COLUMN data_size BIGINT NOT NULL DEFAULT 0; -- total payload size in bytes

CREATE TABLE entry_chunks (
    entry_id UUID NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    seq      INT NOT NULL,
    data     BYTEA NOT NULL,
    PRIMARY KEY (entry_id, seq)
);

CREATE INDEX idx_entries_user_state ON entries(user_id, state);
