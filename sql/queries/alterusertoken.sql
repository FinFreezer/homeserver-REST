-- name: UpdateUserToken :one
UPDATE users
SET authtoken = ?
WHERE name = ?
RETURNING *;