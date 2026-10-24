-- identity (docs/courses/content-model.md "Database"): the users and their browser sessions. A session row keeps the
-- SHA-256 of the cookie's token, never the token, so reading the database gives no session.

-- +goose Up
CREATE TABLE users (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL UNIQUE,
    created_at TEXT    NOT NULL
);

CREATE TABLE sessions (
    token_hash TEXT    PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users (id),
    csrf       TEXT    NOT NULL,
    created_at TEXT    NOT NULL,
    expires_at TEXT    NOT NULL
);

CREATE INDEX sessions_expires_at ON sessions (expires_at);

-- +goose Down
DROP TABLE sessions;
DROP TABLE users;
