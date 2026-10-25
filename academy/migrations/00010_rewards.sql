-- rewards (docs/courses/content-model.md "Database"): the badges a learner was awarded, once each. XP is not stored
-- (it is read from the passes); an award stays when its badge is retired.

-- +goose Up
CREATE TABLE user_badges (
    user_id    INTEGER NOT NULL REFERENCES users (id),
    badge_id   TEXT    NOT NULL,
    awarded_at TEXT    NOT NULL,
    PRIMARY KEY (user_id, badge_id)
);

-- +goose Down
DROP TABLE user_badges;
