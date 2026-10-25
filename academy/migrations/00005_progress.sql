-- progress (docs/courses/content-model.md "Database"): what the learner did with each item that no other module
-- records: when it was opened last (the resume point) and a lecture's completion with the content hash it was
-- completed on (a different current hash = "updated since you completed it"). Quizzes, labs and challenges are
-- completed by their own modules' rows. A course's first completion is kept; notes are one text per item.

-- +goose Up
CREATE TABLE item_progress (
    user_id        INTEGER NOT NULL REFERENCES users (id),
    course_id      TEXT    NOT NULL,
    item_id        TEXT    NOT NULL,
    content_hash   TEXT,
    completed_at   TEXT,
    last_opened_at TEXT,
    PRIMARY KEY (user_id, course_id, item_id)
);

CREATE TABLE course_completions (
    user_id      INTEGER NOT NULL REFERENCES users (id),
    course_id    TEXT    NOT NULL,
    completed_at TEXT    NOT NULL,
    PRIMARY KEY (user_id, course_id)
);

CREATE TABLE notes (
    user_id    INTEGER NOT NULL REFERENCES users (id),
    course_id  TEXT    NOT NULL,
    item_id    TEXT    NOT NULL,
    body       TEXT    NOT NULL,
    updated_at TEXT    NOT NULL,
    PRIMARY KEY (user_id, course_id, item_id)
);

-- +goose Down
DROP TABLE notes;
DROP TABLE course_completions;
DROP TABLE item_progress;
