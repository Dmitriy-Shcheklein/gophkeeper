CREATE TABLE entries (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type       SMALLINT NOT NULL, -- 1=login/password, 2=text, 3=binary, 4=card
    label      VARCHAR(255) NOT NULL,
    metadata   TEXT,
    data       BYTEA NOT NULL,
    version    BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_entries_user_id ON entries(user_id);
