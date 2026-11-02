-- +goose Up
CREATE TABLE users
(
    id TEXT PRIMARY KEY,
    created_at TEXT NOT NULL DEFAULT current_timestamp,
    updated_at TEXT NOT NULL DEFAULT current_timestamp,
    name TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    is_admin INTEGER NOT NULL DEFAULT 0
);

-- +goose Down
DROP TABLE users;
