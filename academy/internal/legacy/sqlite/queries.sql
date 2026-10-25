-- A one-off data migration writes several modules' tables (internal/legacy's package comment).

-- name: LegacyRows :many
SELECT kind, legacy_id, new_id FROM legacy_rows;

-- name: InsertLegacyRow :exec
INSERT INTO legacy_rows (kind, legacy_id, new_id) VALUES (?, ?, ?);

-- name: InsertAttempt :one
INSERT INTO attempts (user_id, lab_id, content_hash, status, started_at, finished_at, hints_used, seconds, xp)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id;

-- name: InsertStepResult :exec
INSERT INTO step_results (attempt_id, step_id, passed, detail, at) VALUES (?, ?, ?, ?, ?);

-- name: InsertHint :exec
INSERT INTO hints_shown (attempt_id, step_id, idx, at) VALUES (?, ?, ?, ?)
ON CONFLICT (attempt_id, step_id, idx) DO NOTHING;

-- name: InsertEvent :one
INSERT INTO events (user_id, kind, subject, data, at) VALUES (?, ?, ?, ?, ?) RETURNING id;

-- name: InsertAward :execrows
INSERT INTO user_badges (user_id, badge_id, awarded_at) VALUES (?, ?, ?)
ON CONFLICT (user_id, badge_id) DO NOTHING;
