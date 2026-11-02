-- +goose Up
CREATE TABLE guestbook
(
    id TEXT PRIMARY KEY,
    created_at TEXT NOT NULL DEFAULT current_timestamp,
    updated_at TEXT NOT NULL DEFAULT current_timestamp,
    author TEXT NOT NULL DEFAULT 'Guest',
    content TEXT NOT NULL,
    authorId TEXT,
    CONSTRAINT fk_authorid
    FOREIGN KEY (authorId) REFERENCES users(id)
    ON DELETE CASCADE
);

-- +goose Down
DROP TABLE guestbook;