-- +goose Up
CREATE TABLE sessions
(
    token_hash TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT current_timestamp,
    expires_at TEXT NOT NULL,
    revoked_at TEXT DEFAULT NULL,
    last_authenticated_at TEXT NOT NULL DEFAULT current_timestamp
);

-- +goose Down
DROP TABLE sessions;