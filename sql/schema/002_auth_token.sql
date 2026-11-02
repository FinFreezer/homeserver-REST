-- +goose Up
PRAGMA foreign_keys=OFF;

CREATE TABLE users_new (
    id TEXT PRIMARY KEY,
    created_at TEXT NOT NULL DEFAULT current_timestamp,
    updated_at TEXT NOT NULL DEFAULT current_timestamp,
    name TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    is_admin INTEGER NOT NULL DEFAULT 0,
    authtoken TEXT UNIQUE
);

INSERT INTO users_new (id, created_at, updated_at, name, password_hash, is_admin)
SELECT id, created_at, updated_at, name, password_hash, is_admin FROM users;

DROP TABLE users;

ALTER TABLE users_new RENAME TO users;

PRAGMA foreign_keys=ON;

-- +goose Down
PRAGMA foreign_keys=OFF;

CREATE TABLE users_new (
    id TEXT PRIMARY KEY,
    created_at timestamp NOT NULL,
    updated_at timestamp NOT NULL,
    name TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    is_admin BOOLEAN NOT NULL
);

INSERT INTO users_new (id, created_at, updated_at, name, password_hash, is_admin)
SELECT id, created_at, updated_at, name, password_hash, is_admin FROM users;

DROP TABLE users;

ALTER TABLE users_new RENAME TO users;

PRAGMA foreign_keys=ON;
