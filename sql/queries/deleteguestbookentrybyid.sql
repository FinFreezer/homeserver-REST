-- name: DeleteSpecificGuestbookEntry :one
DELETE FROM guestbook
WHERE id = ?
RETURNING *;