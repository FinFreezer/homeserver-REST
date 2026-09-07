-- +goose Up
CREATE TABLE guestbook
(
    id UUID PRIMARY KEY,
    created_at timestamp NOT NULL,
    updated_at timestamp NOT NULL,
    author TEXT NOT NULL DEFAULT 'Guest',
    content TEXT NOT NULL,
    authorId UUID,
    CONSTRAINT fk_authorid
    FOREIGN KEY (authorId) REFERENCES users(id)
    ON DELETE CASCADE
);

-- +goose Down
DROP TABLE guestbook;