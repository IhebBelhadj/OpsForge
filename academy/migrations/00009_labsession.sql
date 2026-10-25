-- labsession (docs/courses/content-model.md "Database"): lab attempts, their step results and the hints shown.
-- One active attempt (setup or running) per user, by a partial unique index. step_results since the last reset
-- (a reset deletes them; the history keeps every check); hints_shown counts each hint once per attempt.

-- +goose Up
CREATE TABLE attempts (
    id           INTEGER PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users (id),
    lab_id       TEXT    NOT NULL,
    course_id    TEXT,
    item_id      TEXT,
    content_hash TEXT    NOT NULL,
    namespace    TEXT,
    workspace    TEXT,
    status       TEXT    NOT NULL CHECK (status IN ('setup', 'running', 'passed', 'abandoned', 'failed')),
    started_at   TEXT    NOT NULL,
    finished_at  TEXT,
    hints_used   INTEGER NOT NULL DEFAULT 0,
    seconds      INTEGER,
    xp           INTEGER,
    error        TEXT
);

CREATE UNIQUE INDEX attempts_one_active ON attempts (user_id) WHERE status IN ('setup', 'running');
CREATE INDEX attempts_user_status ON attempts (user_id, status);

CREATE TABLE step_results (
    id         INTEGER PRIMARY KEY,
    attempt_id INTEGER NOT NULL REFERENCES attempts (id),
    step_id    TEXT    NOT NULL,
    passed     INTEGER NOT NULL CHECK (passed IN (0, 1)),
    detail     TEXT    NOT NULL,
    at         TEXT    NOT NULL
);

CREATE INDEX step_results_attempt ON step_results (attempt_id);

CREATE TABLE hints_shown (
    attempt_id INTEGER NOT NULL REFERENCES attempts (id),
    step_id    TEXT    NOT NULL,
    idx        INTEGER NOT NULL,
    at         TEXT    NOT NULL,
    PRIMARY KEY (attempt_id, step_id, idx)
);

-- +goose Down
DROP TABLE hints_shown;
DROP TABLE step_results;
DROP TABLE attempts;
