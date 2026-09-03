-- name: FindUserByID :one
SELECT * FROM users
WHERE id = $1;