-- workspace (docs/courses/architecture.md, "Platform service: modules"): who owns each workspace (the files are in
-- .opsforge/workspaces/<id>/). Kept after the attempt ends: the learner's files stay readable.

-- +goose Up
CREATE TABLE workspaces (
    id         TEXT    PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users (id),
    created_at TEXT    NOT NULL
);

-- +goose Down
DROP TABLE workspaces;
