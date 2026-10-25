-- operations (docs/courses/architecture.md, "HTTP API"): long operations answered with 202 (a lab's setup, a
-- check, a teardown). result is the module's JSON document; error is set when the operation could not finish.

-- +goose Up
CREATE TABLE operations (
    id          TEXT    PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users (id),
    kind        TEXT    NOT NULL,
    subject     TEXT    NOT NULL,
    status      TEXT    NOT NULL CHECK (status IN ('running', 'done', 'failed')),
    result      TEXT,
    error       TEXT,
    created_at  TEXT    NOT NULL,
    finished_at TEXT
);

CREATE INDEX operations_running ON operations (status) WHERE status = 'running';

-- +goose Down
DROP TABLE operations;
