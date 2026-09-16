-- Supported entry types. Labels mirror the EntryType enum of the
-- domain model and the public gRPC API.
CREATE TYPE entry_type AS ENUM ('login_password', 'text', 'binary', 'card');

CREATE TABLE entries (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type       entry_type NOT NULL,
    label      VARCHAR(255) NOT NULL,
    metadata   TEXT,
    data       BYTEA NOT NULL,
    version    BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_entries_user_type ON entries(user_id, type);
