-- The database itself: one row, written when the database is created (readyz reports it). Each module adds its
-- own tables in later migrations (docs/courses/content-model.md "Database"); released migrations are never edited.

-- +goose Up
CREATE TABLE instance (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    created_at TEXT    NOT NULL
);

-- +goose Down
DROP TABLE instance;
