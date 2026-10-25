-- name: InsertAttempt :one
INSERT INTO quiz_attempts (user_id, course_id, item_id, content_hash, started_at, deadline, questions, pass, offered)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id;

-- name: GetAttempt :one
SELECT * FROM quiz_attempts WHERE id = ?;

-- name: SubmitAttempt :execrows
UPDATE quiz_attempts SET submitted_at = ?, answers = ?, correct = ?, score = ?, passed = ?, xp = ?
WHERE id = ? AND submitted_at IS NULL;

-- name: ItemAttempts :many
SELECT * FROM quiz_attempts WHERE user_id = ? AND course_id = ? AND item_id = ? ORDER BY id DESC;

-- name: PassedAttempts :many
SELECT * FROM quiz_attempts WHERE user_id = ? AND passed = 1 ORDER BY submitted_at, id;
