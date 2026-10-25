-- name: InsertWorkspace :exec
INSERT INTO workspaces (id, user_id, created_at) VALUES (?, ?, ?);

-- name: WorkspaceOwner :one
SELECT user_id FROM workspaces WHERE id = ?;
