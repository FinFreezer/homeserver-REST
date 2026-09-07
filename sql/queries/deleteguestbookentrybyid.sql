-- name: DeleteSpecificGuestbookEntry :one
DELETE FROM guestbook
WHERE id = $1
RETURNING *;