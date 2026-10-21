-- +goose Up
ALTER TABLE users
    ADD COLUMN head_rev BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN next_rev BIGINT NOT NULL DEFAULT 1;

CREATE TABLE save_revisions (
    user_id    BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    rev        BIGINT NOT NULL CHECK (rev > 0),
    state      TEXT NOT NULL CHECK (state IN ('pending', 'committed')),
    blob_key   TEXT NOT NULL UNIQUE,
    size       BIGINT NOT NULL CHECK (size > 0),
    sha256     TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    device     TEXT NOT NULL DEFAULT '',
    reason     TEXT NOT NULL,
    game_build BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (user_id, rev)
);
CREATE INDEX save_revisions_pending_idx ON save_revisions (created_at) WHERE state = 'pending';

CREATE TABLE blob_garbage (
    blob_key    TEXT PRIMARY KEY,
    enqueued_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE blob_garbage;
DROP TABLE save_revisions;
ALTER TABLE users DROP COLUMN next_rev, DROP COLUMN head_rev;
