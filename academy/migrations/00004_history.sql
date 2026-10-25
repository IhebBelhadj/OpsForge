-- history (docs/courses/architecture.md, "Domain events"): the learner's history, append-only. Every domain event
-- lands here (kind "<thing>.<what happened>", subject what it happened to, data a JSON object); badge rules are
-- evaluated against it and the event stream replays it after a reconnect (Last-Event-ID = id).

-- +goose Up
CREATE TABLE events (
    id      INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users (id),
    kind    TEXT    NOT NULL,
    subject TEXT    NOT NULL,
    data    TEXT    NOT NULL,
    at      TEXT    NOT NULL
);

CREATE INDEX events_user ON events (user_id, id);
CREATE INDEX events_user_kind ON events (user_id, kind, subject);

-- +goose Down
DROP TABLE events;
