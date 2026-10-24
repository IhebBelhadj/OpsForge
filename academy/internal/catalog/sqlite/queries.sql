-- name: AllContent :many
SELECT kind, key, pack_id, course_id, position, title, content_hash, doc, retired FROM content ORDER BY kind, key;

-- name: UpsertContent :exec
INSERT INTO content (kind, key, pack_id, course_id, position, title, content_hash, doc, retired, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?)
ON CONFLICT (kind, key) DO UPDATE SET
    pack_id = excluded.pack_id, course_id = excluded.course_id, position = excluded.position, title = excluded.title,
    content_hash = excluded.content_hash, doc = excluded.doc, retired = 0, updated_at = excluded.updated_at;

-- name: RetireContent :exec
UPDATE content SET retired = 1, updated_at = ? WHERE kind = ? AND key = ?;

-- name: ContentUpdatedAt :one
SELECT updated_at FROM content WHERE kind = ? AND key = ?;
