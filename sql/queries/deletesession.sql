-- name: DeleteSession :one
DELETE FROM sessions
WHERE token_hash = ?
RETURNING *;