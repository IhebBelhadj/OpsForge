-- name: InsertUser :exec
INSERT INTO users (name, created_at) VALUES (?, ?) ON CONFLICT (name) DO NOTHING;

-- name: UserByName :one
SELECT id, name, created_at FROM users WHERE name = ?;

-- name: UserIDs :many
SELECT id FROM users ORDER BY id;

-- name: UserByID :one
SELECT id, name, created_at FROM users WHERE id = ?;

-- name: InsertSession :exec
INSERT INTO sessions (token_hash, user_id, csrf, created_at, expires_at) VALUES (?, ?, ?, ?, ?);

-- name: SessionByHash :one
SELECT token_hash, user_id, csrf, created_at, expires_at FROM sessions WHERE token_hash = ?;

-- name: DeleteSessionsExpiredBefore :exec
DELETE FROM sessions WHERE expires_at < ?;
