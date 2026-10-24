-- catalog (docs/courses/content-model.md "Database"): every piece of imported content, one row per entry (pack,
-- course, item, lecture, quiz, challenge, lab, badge). doc is the entry's bundle document (JSON text); the columns
-- are what queries filter and sort on. Rows are never deleted: removed content is retired (attempts and awards refer
-- to it). Other modules point at an entry with (kind, key), e.g. attempts with a constant 'lab' kind column.

-- +goose Up
CREATE TABLE content (
    kind         TEXT    NOT NULL CHECK (kind IN ('pack', 'course', 'item', 'lecture', 'quiz', 'challenge', 'lab',
                                                  'badge')),
    key          TEXT    NOT NULL,
    pack_id      TEXT    NOT NULL,
    course_id    TEXT,
    position     INTEGER NOT NULL,
    title        TEXT    NOT NULL,
    content_hash TEXT    NOT NULL,
    doc          TEXT    NOT NULL,
    retired      INTEGER NOT NULL DEFAULT 0 CHECK (retired IN (0, 1)),
    updated_at   TEXT    NOT NULL,
    PRIMARY KEY (kind, key)
);

CREATE INDEX content_course ON content (course_id, kind, position);

-- +goose Down
DROP TABLE content;
