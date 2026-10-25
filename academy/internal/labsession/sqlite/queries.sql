-- name: InsertAttempt :one
INSERT INTO attempts (user_id, lab_id, course_id, item_id, content_hash, status, started_at)
VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id;

-- name: SetPlaces :exec
UPDATE attempts SET namespace = ?, workspace = ? WHERE id = ?;

-- name: GetAttempt :one
SELECT * FROM attempts WHERE id = ?;

-- name: ActiveAttempt :one
SELECT * FROM attempts WHERE user_id = ? AND status IN ('setup', 'running');

-- name: FinishAttempt :exec
UPDATE attempts SET status = ?, finished_at = ?, seconds = ?, xp = ?, error = ? WHERE id = ?;

-- name: PassedSteps :many
SELECT DISTINCT step_id FROM step_results WHERE attempt_id = ? AND passed = 1;

-- name: InsertStepResult :exec
INSERT INTO step_results (attempt_id, step_id, passed, detail, at) VALUES (?, ?, ?, ?, ?);

-- name: ClearSteps :exec
DELETE FROM step_results WHERE attempt_id = ?;

-- name: HintCounts :many
SELECT step_id, COUNT(*) AS shown FROM hints_shown WHERE attempt_id = ? GROUP BY step_id;

-- name: InsertHint :execrows
INSERT INTO hints_shown (attempt_id, step_id, idx, at) VALUES (?, ?, ?, ?)
ON CONFLICT (attempt_id, step_id, idx) DO NOTHING;

-- name: CountHint :exec
UPDATE attempts SET hints_used = hints_used + 1 WHERE id = ?;

-- name: PassedAttempts :many
SELECT * FROM attempts WHERE user_id = ? AND status = 'passed' ORDER BY finished_at, id;

-- name: RecoverSetups :execrows
UPDATE attempts SET status = 'running' WHERE status = 'setup';
