-- legacy (internal/legacy): the Phase 10 rows `academy migrate-legacy` imported, by kind (attempt, event) and their
-- id in labs.db, with the id they got here; a later run imports only the rows not listed.

-- +goose Up
CREATE TABLE legacy_rows (
    kind      TEXT    NOT NULL CHECK (kind IN ('attempt', 'event')),
    legacy_id INTEGER NOT NULL,
    new_id    INTEGER NOT NULL,
    PRIMARY KEY (kind, legacy_id)
);

-- +goose Down
DROP TABLE legacy_rows;
