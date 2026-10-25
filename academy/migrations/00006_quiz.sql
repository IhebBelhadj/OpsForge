-- quiz (docs/courses/content-model.md "Database"): attempts at quiz items. questions holds the attempt's drawn
-- questions with their keys (JSON), so a quiz edited during an attempt does not change it; pass and offered are
-- the quiz's pass mark and xp when it started, xp what the attempt earned (the first pass of an item only).

-- +goose Up
CREATE TABLE quiz_attempts (
    id           INTEGER PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users (id),
    course_id    TEXT    NOT NULL,
    item_id      TEXT    NOT NULL,
    content_hash TEXT    NOT NULL,
    started_at   TEXT    NOT NULL,
    deadline     TEXT,
    questions    TEXT    NOT NULL,
    pass         INTEGER NOT NULL,
    offered      INTEGER NOT NULL,
    submitted_at TEXT,
    answers      TEXT,
    correct      INTEGER NOT NULL DEFAULT 0,
    score        INTEGER NOT NULL DEFAULT 0,
    passed       INTEGER NOT NULL DEFAULT 0 CHECK (passed IN (0, 1)),
    xp           INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX quiz_attempts_item ON quiz_attempts (user_id, course_id, item_id);

-- +goose Down
DROP TABLE quiz_attempts;
