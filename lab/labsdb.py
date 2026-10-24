"""Teaching-lab database (`.opsforge/labs.db`, docs/PLAN.md Phase 10): migrations, pack import, progress, unlocks
and badge awards. SQLite through the stdlib; no Incus calls (lab/labserver.py drives the lab).

Content (packs, curricula, levels, labs, badges) is rewritten from the pack files by `import_pack`; rows removed from
the files are retired, not deleted, because learner rows (attempts, events, awards) refer to them.
"""

import json
import sqlite3
from datetime import datetime, timezone
from pathlib import Path

import labs

MIGRATIONS = Path(__file__).resolve().parent / "migrations"
LOCAL_USER = "learner"


def now():
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def _seconds_between(start, end):
    parse = lambda s: datetime.strptime(s, "%Y-%m-%dT%H:%M:%SZ")  # noqa: E731
    return (parse(end) - parse(start)).total_seconds()


def migrations():
    """[(version, path)] of lab/migrations/NNNN_<name>.sql, in order."""
    return sorted((int(p.name.split("_")[0]), p) for p in MIGRATIONS.glob("[0-9][0-9][0-9][0-9]_*.sql"))


def connect(path):
    """A connection with every migration applied. `path` may be ':memory:' (tests)."""
    if path != ":memory:":
        Path(path).parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(path, timeout=10, check_same_thread=False)
    conn.row_factory = sqlite3.Row
    conn.execute("PRAGMA foreign_keys = ON")
    if path != ":memory:":
        conn.execute("PRAGMA journal_mode = WAL")
    migrate(conn)
    return conn


def migrate(conn):
    current = conn.execute("PRAGMA user_version").fetchone()[0]
    for version, path in migrations():
        if version > current:
            conn.executescript(f"BEGIN;\n{path.read_text()}\nPRAGMA user_version = {version};\nCOMMIT;")


def local_user(conn):
    conn.execute("INSERT OR IGNORE INTO users (name) VALUES (?)", (LOCAL_USER,))
    conn.commit()
    return conn.execute("SELECT id FROM users WHERE name = ?", (LOCAL_USER,)).fetchone()[0]


def add_event(conn, user, kind, subject=None, data=None, at=None):
    conn.execute("INSERT INTO events (user_id, kind, subject, data, at) VALUES (?, ?, ?, ?, ?)",
                 (user, kind, subject, json.dumps(data or {}, sort_keys=True), at or now()))


def catalog_ids(conn, except_pack=None):
    """Lab and curriculum ids of the other imported packs, for labs.errors."""
    return {
        kind: {r[0] for r in conn.execute(f"SELECT id FROM {kind} WHERE retired = 0 AND pack_id IS NOT ?",
                                          (except_pack,))}
        for kind in ("labs", "curricula")
    }


def import_pack(conn, doc, source):
    """Upserts a validated pack; returns {"labs": {"added", "updated", "unchanged", "retired"}, "changed": bool}.
    Importing the same files again changes nothing (not even the import time)."""
    pack = doc["pack"]
    pid = pack["id"]
    taken = conn.execute(
        f"SELECT id, pack_id FROM labs WHERE pack_id != ? AND id IN ({','.join('?' * len(doc['labs']))})",
        (pid, *doc["labs"]),
    ).fetchall()
    if taken:
        raise ValueError(", ".join(f"lab {r['id']} already belongs to pack {r['pack_id']}" for r in taken))
    digest = labs.content_hash(doc)
    old = conn.execute("SELECT content_hash FROM packs WHERE id = ?", (pid,)).fetchone()
    summary = {"labs": {"added": [], "updated": [], "unchanged": [], "retired": []}, "changed": False}
    if old and old["content_hash"] == digest:
        summary["labs"]["unchanged"] = sorted(doc["labs"])
        return summary
    stamp = now()
    with conn:
        conn.execute(
            "INSERT INTO packs (id, title, version, author, description, hint_penalty, source, content_hash, "
            "imported_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (id) DO UPDATE SET title = excluded.title, "
            "version = excluded.version, author = excluded.author, description = excluded.description, "
            "hint_penalty = excluded.hint_penalty, source = excluded.source, content_hash = excluded.content_hash, "
            "imported_at = excluded.imported_at",
            (pid, pack["title"], str(pack["version"]), pack.get("author"), pack.get("description"),
             pack.get("hint_penalty", labs.DEFAULT_HINT_PENALTY), str(source), digest, stamp),
        )
        existing = {r["id"]: r for r in conn.execute("SELECT id, content_hash, retired FROM labs WHERE pack_id = ?",
                                                     (pid,))}
        for lid, lab in doc["labs"].items():
            h = labs.content_hash(lab)
            row = existing.get(lid)
            if row and row["content_hash"] == h and not row["retired"]:
                summary["labs"]["unchanged"].append(lid)
                continue
            summary["labs"]["updated" if row else "added"].append(lid)
            conn.execute(
                "INSERT INTO labs (id, pack_id, title, summary, xp, minutes, body, content_hash, updated_at, retired) "
                "VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0) ON CONFLICT (id) DO UPDATE SET title = excluded.title, "
                "summary = excluded.summary, xp = excluded.xp, minutes = excluded.minutes, body = excluded.body, "
                "content_hash = excluded.content_hash, updated_at = excluded.updated_at, retired = 0",
                (lid, pid, lab["title"], lab["summary"], lab.get("xp", labs.DEFAULT_XP), lab.get("minutes"),
                 json.dumps(lab), h, stamp),
            )
        for lid, row in existing.items():
            if lid not in doc["labs"] and not row["retired"]:
                summary["labs"]["retired"].append(lid)
                conn.execute("UPDATE labs SET retired = 1 WHERE id = ?", (lid,))
        conn.execute("DELETE FROM prereqs WHERE lab_id IN (SELECT id FROM labs WHERE pack_id = ?)", (pid,))
        conn.executemany("INSERT INTO prereqs (lab_id, requires_lab_id) VALUES (?, ?)",
                         [(lid, req) for lid, lab in doc["labs"].items() for req in lab.get("requires", [])])

        conn.execute("UPDATE curricula SET retired = 1 WHERE pack_id = ?", (pid,))
        for position, (cid, cur) in enumerate(doc["curricula"].items()):
            conn.execute(
                "INSERT INTO curricula (id, pack_id, title, summary, position, retired) VALUES (?, ?, ?, ?, ?, 0) "
                "ON CONFLICT (id) DO UPDATE SET title = excluded.title, summary = excluded.summary, "
                "position = excluded.position, retired = 0",
                (cid, pid, cur["title"], cur.get("summary"), cur.get("position", position)),
            )
            conn.execute("DELETE FROM levels WHERE curriculum_id = ?", (cid,))
            for lpos, level in enumerate(cur["levels"]):
                level_id = conn.execute(
                    "INSERT INTO levels (curriculum_id, position, title, summary, xp_required) VALUES (?, ?, ?, ?, ?)",
                    (cid, lpos, level["title"], level.get("summary"), level.get("xp_required")),
                ).lastrowid
                conn.executemany("INSERT INTO level_labs (level_id, lab_id, position) VALUES (?, ?, ?)",
                                 [(level_id, lid, i) for i, lid in enumerate(level["labs"])])

        conn.execute("UPDATE badges SET retired = 1 WHERE pack_id = ?", (pid,))
        for position, badge in enumerate(doc["badges"]):
            conn.execute(
                "INSERT INTO badges (id, pack_id, title, description, icon, rule, position, retired) "
                "VALUES (?, ?, ?, ?, ?, ?, ?, 0) ON CONFLICT (id) DO UPDATE SET title = excluded.title, "
                "description = excluded.description, icon = excluded.icon, rule = excluded.rule, "
                "position = excluded.position, retired = 0",
                (badge["id"], pid, badge["title"], badge["description"], badge.get("icon"),
                 json.dumps(badge["rule"], sort_keys=True), position),
            )
    summary["changed"] = True
    return summary


def get_lab(conn, lab_id):
    """A lab's row with its parsed body and its pack's hint penalty, or None."""
    row = conn.execute("SELECT labs.*, packs.hint_penalty FROM labs JOIN packs ON packs.id = labs.pack_id "
                       "WHERE labs.id = ?", (lab_id,)).fetchone()
    return None if row is None else dict(row) | {"body": json.loads(row["body"])}


def best_passes(conn, user):
    """{lab id: {"xp", "content_hash" of the latest pass, "hints", "seconds"}} of every lab the user passed."""
    best = {}
    for r in conn.execute("SELECT lab_id, xp, content_hash, hints_used, seconds FROM attempts "
                          "WHERE user_id = ? AND status = 'passed' ORDER BY finished_at, id", (user,)):
        prev = best.get(r["lab_id"])
        best[r["lab_id"]] = {"xp": max(r["xp"], prev["xp"] if prev else 0), "content_hash": r["content_hash"],
                             "hints": r["hints_used"], "seconds": r["seconds"]}
    return best


def total_xp(conn, user):
    return sum(p["xp"] for p in best_passes(conn, user).values())


def history(conn, user):
    """What badge rules are evaluated against (labs.RULES)."""
    passes = [{"lab": r["lab_id"], "hints": r["hints_used"], "seconds": r["seconds"] or 0, "xp": r["xp"]}
              for r in conn.execute("SELECT * FROM attempts WHERE user_id = ? AND status = 'passed'", (user,))]
    curricula = {}
    for r in conn.execute("SELECT levels.curriculum_id, level_labs.lab_id FROM level_labs "
                          "JOIN levels ON levels.id = level_labs.level_id "
                          "JOIN curricula ON curricula.id = levels.curriculum_id WHERE curricula.retired = 0"):
        curricula.setdefault(r["curriculum_id"], []).append(r["lab_id"])
    return {"passes": passes, "curricula": curricula, "xp": total_xp(conn, user)}


def running_attempt(conn, user):
    row = conn.execute("SELECT * FROM attempts WHERE user_id = ? AND status = 'running'", (user,)).fetchone()
    return None if row is None else dict(row)


def catalog(conn, user):
    """Curricula -> levels -> labs, with the user's progress: each lab's status (new, running, passed), whether
    it was passed on an older revision (stale), and whether its level and prerequisites let it start (locked)."""
    passed = best_passes(conn, user)
    xp = sum(p["xp"] for p in passed.values())
    running = running_attempt(conn, user)
    prereqs = {}
    for r in conn.execute("SELECT * FROM prereqs"):
        prereqs.setdefault(r["lab_id"], []).append(r["requires_lab_id"])
    rows = conn.execute(
        "SELECT c.id AS cid, c.title AS ctitle, c.summary AS csummary, l.id AS level_id, l.position AS lpos, "
        "l.title AS ltitle, l.summary AS lsummary, l.xp_required, labs.id AS lab_id, labs.title, labs.summary, "
        "labs.xp, labs.minutes, labs.content_hash, labs.body FROM curricula c "
        "JOIN levels l ON l.curriculum_id = c.id JOIN level_labs ll ON ll.level_id = l.id "
        "JOIN labs ON labs.id = ll.lab_id WHERE c.retired = 0 AND labs.retired = 0 "
        "ORDER BY c.position, c.id, l.position, ll.position"
    ).fetchall()
    curricula = []
    for r in rows:
        if not curricula or curricula[-1]["id"] != r["cid"]:
            curricula.append({"id": r["cid"], "title": r["ctitle"], "summary": r["csummary"], "levels": []})
        levels = curricula[-1]["levels"]
        if not levels or levels[-1]["position"] != r["lpos"]:
            levels.append({"position": r["lpos"], "title": r["ltitle"], "summary": r["lsummary"],
                           "xp_required": r["xp_required"], "labs": []})
        body = json.loads(r["body"])
        p = passed.get(r["lab_id"])
        levels[-1]["labs"].append({
            "id": r["lab_id"], "title": r["title"], "summary": r["summary"], "xp": r["xp"], "minutes": r["minutes"],
            "layout": body.get("layout"), "needs": body.get("needs", []), "concepts": body.get("concepts", []),
            "status": "running" if running and running["lab_id"] == r["lab_id"] else "passed" if p else "new",
            "best_xp": p["xp"] if p else 0, "stale": bool(p) and p["content_hash"] != r["content_hash"],
            "requires": prereqs.get(r["lab_id"], []),
        })
    for cur in curricula:
        previous_done = True
        for level in cur["levels"]:
            level["unlocked"] = (xp >= level["xp_required"]) if level["xp_required"] is not None else previous_done
            for lab in level["labs"]:
                missing = [req for req in lab["requires"] if req not in passed]
                lab["locked"] = not level["unlocked"] or bool(missing)
                lab["missing"] = missing
            previous_done = all(lab["id"] in passed for lab in level["labs"])
    return curricula


def lab_locked(conn, user, lab_id):
    """True if no curriculum offers the lab unlocked (labs outside every curriculum are always open)."""
    entries = [lab for cur in catalog(conn, user) for level in cur["levels"] for lab in level["labs"]
               if lab["id"] == lab_id]
    return bool(entries) and all(lab["locked"] for lab in entries)


def start_attempt(conn, user, lab_id):
    lab = get_lab(conn, lab_id)
    if lab is None or lab["retired"]:
        raise ValueError(f"no lab {lab_id}")
    if running_attempt(conn, user):
        raise ValueError("another lab is running: finish or abandon it first")
    with conn:
        attempt = conn.execute(
            "INSERT INTO attempts (user_id, lab_id, content_hash, started_at, status) VALUES (?, ?, ?, ?, 'running')",
            (user, lab_id, lab["content_hash"], now()),
        ).lastrowid
        add_event(conn, user, "lab.started", lab_id, {"attempt": attempt})
    return attempt


def get_attempt(conn, attempt):
    row = conn.execute("SELECT * FROM attempts WHERE id = ?", (attempt,)).fetchone()
    return None if row is None else dict(row)


def passed_steps(conn, attempt):
    return {r[0] for r in conn.execute("SELECT DISTINCT step_id FROM step_results WHERE attempt_id = ? AND passed = 1",
                                       (attempt,))}


def shown_hints(conn, attempt):
    """{step id: number of hints shown} in this attempt."""
    shown = {}
    for r in conn.execute("SELECT data FROM events WHERE kind = 'hint.shown' AND json_extract(data, '$.attempt') = ?",
                          (attempt,)):
        data = json.loads(r[0])
        shown[data["step"]] = max(shown.get(data["step"], 0), data["index"] + 1)
    return shown


def record_check(conn, attempt, step_id, passed, detail):
    a = get_attempt(conn, attempt)
    with conn:
        conn.execute("INSERT INTO step_results (attempt_id, step_id, passed, detail, at) VALUES (?, ?, ?, ?, ?)",
                     (attempt, step_id, int(passed), json.dumps(detail, sort_keys=True), now()))
        add_event(conn, a["user_id"], "step.checked", a["lab_id"], {"attempt": attempt, "step": step_id,
                                                                    "passed": bool(passed)})


def show_hint(conn, attempt, step_id, index):
    """Records hint `index` of a step as shown; a hint counts against the pass once, however often it is opened."""
    a = get_attempt(conn, attempt)
    if shown_hints(conn, attempt).get(step_id, 0) > index:
        return
    with conn:
        conn.execute("UPDATE attempts SET hints_used = hints_used + 1 WHERE id = ?", (attempt,))
        add_event(conn, a["user_id"], "hint.shown", a["lab_id"], {"attempt": attempt, "step": step_id, "index": index})


def finish(conn, attempt, at=None):
    """Marks a running attempt passed, with its time and XP; returns the badges this awarded."""
    a = get_attempt(conn, attempt)
    lab = get_lab(conn, a["lab_id"])
    at = at or now()
    xp = labs.pass_xp(lab["xp"], a["hints_used"], lab["hint_penalty"])
    seconds = _seconds_between(a["started_at"], at)
    with conn:
        conn.execute("UPDATE attempts SET status = 'passed', finished_at = ?, seconds = ?, xp = ? WHERE id = ?",
                     (at, seconds, xp, attempt))
        add_event(conn, a["user_id"], "lab.passed", a["lab_id"],
                  {"attempt": attempt, "xp": xp, "hints": a["hints_used"], "seconds": seconds}, at)
    return award_badges(conn, a["user_id"])


def abandon(conn, attempt):
    a = get_attempt(conn, attempt)
    with conn:
        conn.execute("UPDATE attempts SET status = 'abandoned', finished_at = ? WHERE id = ?", (now(), attempt))
        add_event(conn, a["user_id"], "lab.abandoned", a["lab_id"], {"attempt": attempt})


def award_badges(conn, user):
    """Awards every badge whose rule the user's history now meets (including rules added after the passes)."""
    hist = history(conn, user)
    new = []
    with conn:
        for b in conn.execute("SELECT * FROM badges WHERE retired = 0 AND id NOT IN "
                              "(SELECT badge_id FROM user_badges WHERE user_id = ?) ORDER BY position", (user,)):
            rule = json.loads(b["rule"])
            if rule["type"] in labs.RULES and labs.earned(rule, hist):
                conn.execute("INSERT INTO user_badges (user_id, badge_id, awarded_at) VALUES (?, ?, ?)",
                             (user, b["id"], now()))
                add_event(conn, user, "badge.awarded", b["id"])
                new.append({"id": b["id"], "title": b["title"], "description": b["description"], "icon": b["icon"]})
    return new


def badges(conn, user):
    return [dict(r) for r in conn.execute(
        "SELECT b.id, b.title, b.description, b.icon, ub.awarded_at FROM badges b LEFT JOIN user_badges ub "
        "ON ub.badge_id = b.id AND ub.user_id = ? WHERE b.retired = 0 OR ub.awarded_at IS NOT NULL "
        "ORDER BY b.pack_id, b.position", (user,))]
