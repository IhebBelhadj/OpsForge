-- name: InsertOperation :exec
INSERT INTO operations (id, user_id, kind, subject, status, created_at) VALUES (?, ?, ?, ?, ?, ?);

-- name: FinishOperation :exec
UPDATE operations SET status = ?, result = ?, error = ?, finished_at = ? WHERE id = ?;

-- name: GetOperation :one
SELECT * FROM operations WHERE id = ?;

-- name: FailRunning :execrows
UPDATE operations SET status = 'failed', error = ?, finished_at = ? WHERE status = 'running';
