-- name: CreateUser :one
INSERT INTO users (id, created_at, updated_at, name, password_hash, is_admin)
VALUES (
    gen_random_uuid(),
    datetime()(),
    datetime()(),
    ?,
    ?,
    ?
)
RETURNING *;