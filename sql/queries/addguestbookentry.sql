-- name: CreateGuestbookEntry :one
INSERT INTO guestbook (id, created_at, updated_at, author, content, authorId)
VALUES (
    $1,
    NOW(),
    NOW(),
    $2,
    $3,
    $4
)
RETURNING *;