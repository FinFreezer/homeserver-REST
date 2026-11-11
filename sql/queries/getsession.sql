-- name: GetSession :one
SELECT * FROM sessions 
WHERE token_hash = ?;