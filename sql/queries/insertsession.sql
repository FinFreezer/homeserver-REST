-- name: InsertSession :one
INSERT INTO sessions (token_hash, user_id, created_at, expires_at, revoked_at, last_authenticated_at)
VALUES (
    ?,
    ?,
    datetime(),
    ?,
    NULL,
    datetime()
)
RETURNING *;