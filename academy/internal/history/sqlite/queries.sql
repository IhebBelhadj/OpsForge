-- name: InsertEvent :one
INSERT INTO events (user_id, kind, subject, data, at) VALUES (?, ?, ?, ?, ?) RETURNING id;

-- name: EventsAfter :many
SELECT id, user_id, kind, subject, data, at FROM events WHERE user_id = ? AND id > ? ORDER BY id LIMIT ?;
