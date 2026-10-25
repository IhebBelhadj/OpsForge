-- Phase 10's labs.db (lab/migrations/0001_init.sql) with a learner's rows, for the migrate-legacy tests.
-- Teaching labs (docs/PLAN.md Phase 10). Content tables are rewritten by `opsforge labs import` from labs/<pack>/;
-- learner tables (attempts onwards) are only appended to or updated, never rebuilt from files.

CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE packs (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    version TEXT NOT NULL,
    author TEXT,
    description TEXT,
    hint_penalty REAL NOT NULL,
    source TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    imported_at TEXT NOT NULL
);

-- Retired rows (removed from their pack's files) are kept: attempts and awards still refer to them
CREATE TABLE curricula (
    id TEXT PRIMARY KEY,
    pack_id TEXT NOT NULL REFERENCES packs (id),
    title TEXT NOT NULL,
    summary TEXT,
    position INTEGER NOT NULL,
    retired INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE levels (
    id INTEGER PRIMARY KEY,
    curriculum_id TEXT NOT NULL REFERENCES curricula (id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    title TEXT NOT NULL,
    summary TEXT,
    xp_required INTEGER,
    UNIQUE (curriculum_id, position)
);

CREATE TABLE labs (
    id TEXT PRIMARY KEY,
    pack_id TEXT NOT NULL REFERENCES packs (id),
    title TEXT NOT NULL,
    summary TEXT NOT NULL,
    xp INTEGER NOT NULL,
    minutes INTEGER,
    -- The whole lab file (briefing, steps, checks, setup/teardown, layout, needs) as JSON
    body TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    retired INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE level_labs (
    level_id INTEGER NOT NULL REFERENCES levels (id) ON DELETE CASCADE,
    lab_id TEXT NOT NULL REFERENCES labs (id),
    position INTEGER NOT NULL,
    PRIMARY KEY (level_id, lab_id)
);

CREATE TABLE prereqs (
    lab_id TEXT NOT NULL REFERENCES labs (id),
    requires_lab_id TEXT NOT NULL REFERENCES labs (id),
    PRIMARY KEY (lab_id, requires_lab_id)
);

CREATE TABLE badges (
    id TEXT PRIMARY KEY,
    pack_id TEXT NOT NULL REFERENCES packs (id),
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    icon TEXT,
    rule TEXT NOT NULL,
    position INTEGER NOT NULL,
    retired INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE attempts (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users (id),
    lab_id TEXT NOT NULL REFERENCES labs (id),
    -- The lab revision this attempt was made on (labs.content_hash at start)
    content_hash TEXT NOT NULL,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    status TEXT NOT NULL CHECK (status IN ('running', 'passed', 'abandoned')),
    hints_used INTEGER NOT NULL DEFAULT 0,
    seconds REAL,
    xp INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX attempts_user_lab ON attempts (user_id, lab_id);
-- One lab runs at a time on the single lab
CREATE UNIQUE INDEX attempts_one_running ON attempts (user_id) WHERE status = 'running';

CREATE TABLE step_results (
    id INTEGER PRIMARY KEY,
    attempt_id INTEGER NOT NULL REFERENCES attempts (id),
    step_id TEXT NOT NULL,
    passed INTEGER NOT NULL,
    -- What was expected and what was found, shown to the learner
    detail TEXT NOT NULL,
    at TEXT NOT NULL
);
CREATE INDEX step_results_attempt ON step_results (attempt_id);

-- Append-only log of everything a learner does (lab.started, step.checked, hint.shown, lab.passed, badge.awarded, ...)
CREATE TABLE events (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users (id),
    kind TEXT NOT NULL,
    subject TEXT,
    data TEXT NOT NULL DEFAULT '{}',
    at TEXT NOT NULL
);
CREATE INDEX events_user_kind ON events (user_id, kind);

CREATE TABLE user_badges (
    user_id INTEGER NOT NULL REFERENCES users (id),
    badge_id TEXT NOT NULL REFERENCES badges (id),
    awarded_at TEXT NOT NULL,
    PRIMARY KEY (user_id, badge_id)
);

INSERT INTO users (id, name, created_at) VALUES (1, 'learner', '2026-10-24T12:22:59Z');
INSERT INTO packs VALUES ('core', 'Core', '1', NULL, NULL, 0.1, 'labs/core', 'p', '2026-10-24T12:22:59Z');
INSERT INTO labs (id, pack_id, title, summary, xp, body, content_hash, updated_at) VALUES
    ('regions-and-latency', 'core', 'R', 's', 100, '{}', 'e406eb6eb5d93569', '2026-10-24T12:22:59Z'),
    ('private-networks', 'core', 'P', 's', 100, '{}', '72aa9f03d131214c', '2026-10-24T12:22:59Z');
INSERT INTO badges (id, pack_id, title, description, rule, position) VALUES
    ('first-route', 'core', 'First route set', 'd', '{"type": "labs_passed", "min": 1}', 0);
INSERT INTO attempts (id, user_id, lab_id, content_hash, started_at, finished_at, status, hints_used, seconds, xp) VALUES
    (1, 1, 'regions-and-latency', 'e406eb6eb5d93569', '2026-10-24T12:49:14Z', '2026-10-24T12:50:24Z', 'passed', 1, 70.0, 90),
    (2, 1, 'private-networks', '72aa9f03d131214c', '2026-10-24T12:50:36Z', '2026-10-24T12:50:50Z', 'abandoned', 0, NULL, 0);
INSERT INTO step_results (attempt_id, step_id, passed, detail, at) VALUES
    (1, 'ping', 0, '{"results": []}', '2026-10-24T12:49:30Z'),
    (1, 'ping', 1, '{"results": []}', '2026-10-24T12:50:24Z'),
    (2, 'nat', 0, '{"results": []}', '2026-10-24T12:50:45Z');
INSERT INTO events (id, user_id, kind, subject, data, at) VALUES
    (1, 1, 'lab.started', 'regions-and-latency', '{"attempt": 1}', '2026-10-24T12:49:14Z'),
    (2, 1, 'hint.shown', 'regions-and-latency', '{"attempt": 1, "step": "ping", "index": 0}', '2026-10-24T12:49:40Z'),
    (3, 1, 'step.checked', 'regions-and-latency', '{"attempt": 1, "step": "ping", "passed": true}', '2026-10-24T12:50:24Z'),
    (4, 1, 'lab.passed', 'regions-and-latency', '{"attempt": 1, "xp": 90, "hints": 1, "seconds": 70.0}', '2026-10-24T12:50:24Z'),
    (5, 1, 'badge.awarded', 'first-route', '{}', '2026-10-24T12:50:24Z'),
    (6, 1, 'lab.started', 'private-networks', '{"attempt": 2}', '2026-10-24T12:50:36Z'),
    (7, 1, 'lab.abandoned', 'private-networks', '{"attempt": 2}', '2026-10-24T12:50:50Z');
INSERT INTO user_badges VALUES (1, 'first-route', '2026-10-24T12:50:24Z');
