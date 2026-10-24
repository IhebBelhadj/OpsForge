-- name: InitInstance :exec
INSERT INTO instance (id, created_at) VALUES (1, ?) ON CONFLICT (id) DO NOTHING;

-- name: GetInstance :one
SELECT created_at FROM instance WHERE id = 1;
