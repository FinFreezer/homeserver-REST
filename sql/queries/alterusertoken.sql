-- name: UpdateUserToken :one
UPDATE users
SET authtoken_hash = ?
WHERE name = ?
RETURNING *;