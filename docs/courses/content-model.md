# Content model, database and API

Rules carried over from Phase 10: **pack files are the source of truth**, the database mirrors them and holds learner
state, importing the same content twice changes nothing, edited content flags progress made on the old revision
("updated since you completed it"), removed content is retired, never deleted.

## Flow

```
labs/<pack>/  ──(author edits)──▶  opsforge academy compile  ──▶  bundle (JSON, contract 1)  ──▶  academy import
   YAML + Markdown                  Python: schema + cross-ref       validated against              Go: idempotent upsert,
                                     checks, content hashes          bundle.schema.json             retire, report
```

`academy serve` imports at start (as `opsforge serve` did) and on `POST /api/v1/admin/import` (dev).

## Pack layout

```
labs/<pack>/
  pack.yml                      # id, title, version, hint_penalty (unchanged)
  courses/<course>/
    course.yml                  # metadata + ordered sections and items
    lectures/<id>.md            # Markdown with YAML front matter
    quizzes/<id>.yml
    challenges/<id>/
      challenge.yml             # statement, difficulty, tests, limits
      starter/                  # files copied into the learner's workspace
      solution/                 # reference files, revealed after acceptance
  labs/<lab>.yml                # Phase 10 format + additions below; reusable across courses
  labs/<lab>/files/             # optional: files copied into the lab workspace (manifests to edit)
  assets/                       # images and SVG diagrams for lectures (served read-only)
  badges.yml
```

Phase 10's `curricula/` folder is converted by hand in M2 (the two curricula become course files) and then dropped
from the schema.

## course.yml

```yaml
title: Kubernetes Fundamentals
subtitle: Run, expose and debug workloads on a cluster spread over three regions.
level: beginner                 # beginner | intermediate | advanced
requires: []                    # course ids recommended first (shown, not enforced)
layout: stretched               # optional; sections and labs may narrow it
needs: [k3s]                    # must include what its labs and challenges need (checked by compile)
outcomes: [Read the cluster's state with kubectl, Debug a pod that will not start]
requirements: [Basic shell use (cd, ls, a text editor). No Kubernetes experience.]
description: |
  Markdown for the course page.
badge: k8s-fundamentals         # optional: awarded on completion (badges.yml)
sections:
  - title: Kubernetes and your cluster
    items:
      - lecture: containers-to-orchestration
      - quiz: architecture-check
      - lab: meet-your-cluster          # pack-level lab id
      - challenge: first-pod
```

Item ids are unique within a course; a lab id may appear in several courses. Durations come from each item
(`minutes`), summed per section and course.

## Lecture (`lectures/<id>.md`)

```markdown
---
title: Pod lifecycle
minutes: 10
resources:
  - { title: "Kubernetes docs: Pod lifecycle", url: "https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/" }
---
A pod moves through **phases** ...

![The phases of a pod](../../../assets/pod-phases.svg)

> [!NOTE]
> Restarts happen inside the pod: the pod stays, its containers restart.
```

GitHub-flavoured Markdown, GitHub-style alerts (`[!NOTE]`, `[!WARNING]`, `[!TIP]`), fenced code (` ```bash ` blocks
get Run/Paste/Copy), ` ```mermaid ` diagrams, images confined to the pack's `assets/`. No raw HTML.

## Quiz (`quizzes/<id>.yml`)

```yaml
title: Pods check
minutes: 5
pass: 70                         # percent (default 70)
xp: 20                           # first pass (default 20)
shuffle: true                    # questions and choices (default true)
time_limit: 20m                  # optional (final exams)
draw: 25                         # optional: questions drawn per attempt (final exams)
questions:
  - id: crashloop
    type: single                 # single | multiple
    prompt: |
      A pod's container exits with code 1 every few seconds. What does `kubectl get pods` show after a few restarts?
    choices: [Running, Pending, CrashLoopBackOff, ImagePullBackOff]
    answer: 2                    # index, or a list for `multiple` (all-or-nothing)
    explain: The kubelet restarts the container with a growing delay and reports the wait as CrashLoopBackOff.
    see: pod-lifecycle           # optional lecture id in the same course
```

## Challenge (`challenges/<id>/challenge.yml`)

```yaml
title: Zero-downtime rollout
difficulty: medium               # easy | medium | hard
minutes: 25
xp: 150
needs: [k3s]
statement: |
  Markdown: what to build, constraints, an example of the expected `kubectl get` output.
files: [web.yaml]                # starter/ files opened in the editor, in this order
time_limit: 120s                 # pods must be Ready within this after apply
tests:                           # one entry = one "test case" on the Result tab
  - name: 3 replicas ready
    probe:
      steps: [{ run: { cmd: "./opsforge kubectl -n ${ns} get deploy web -o jsonpath='{.status.readyReplicas}'" }, name: ready }]
      expect: { ready.value: "== 3" }
  - name: never fewer than 3 ready during a rollout
    action:                      # optional: done by the grader before the probe
      - run: { cmd: "./opsforge kubectl -n ${ns} set env deploy/web ROLL=1" }
    probe:
      steps: [{ wait_until: { cmd: "...", timeout: 90s }, name: rollout }]
      expect: { rollout.ok: "== 1" }
hints: [ ... ]                   # same hint/XP rules as labs
```

- **Run**: `apply` with `dry_run: server` for every workspace file (plus schema validation in the browser).
- **Submit**: create namespace `ch-<attempt>`, apply the files, wait for readiness within `time_limit`, run each
  test's `action` then `probe` (toolchain `run`/`probe`), record per-test verdicts, delete the namespace.
- Verdicts: `accepted`, `wrong_answer`, `invalid_manifest`, `time_limit_exceeded`, `error` (toolchain failure,
  not counted).
- Grader actions and probes follow the lab rule: they may change only the challenge namespace.

## Labs

Phase 10's format, plus:

| Key | Meaning |
|---|---|
| `namespace: auto` | setup creates `lab-<attempt>` and teardown deletes it; `${ns}` in texts and commands |
| `files: [web.yaml, ...]` | copied from `labs/<lab>/files/` into the workspace at start |
| step `open: web.yaml` | the step opens this file in the Editor tab |
| `cluster_changes: [...]` | anything outside the namespace (a node taint), shown before starting; teardown must undo it |
| `reset: setup \| snapshot` | Reset lab: teardown + setup (default), or restore snapshot `academy-baseline` (labs that break the cluster) |

## Compiled bundle

One JSON document (`academy/api/bundle.schema.json`), produced by `opsforge academy compile`:

```
{ contract: 1, compiled_at, packs: [{ id, title, version, hint_penalty, content_hash,
    courses: [{ id, ..., sections: [{ title, layout, needs, items: [{ id, type, ref, title, minutes, content_hash }] }] }],
    lectures: [{ course, id, title, minutes, markdown, resources, content_hash }],
    quizzes: [{ course, id, ..., questions (with answers), content_hash }],
    challenges: [{ course, id, ..., statement, starter: {path: text}, solution: {path: text}, tests, content_hash }],
    labs: [{ id, ..., body, files: {path: text}, content_hash }],
    badges: [...], assets: [{ path, sha256, bytes }] }],
  errors: [] }
```

Assets are referenced, not inlined: `academy` serves them from the pack folder after checking the hash.

## Database (Go service, goose migrations, sqlc)

Each module owns its tables (architecture.md). Types: ids are text (`<course>/<item>` for items), JSON columns are
text decoded in Go, times are RFC 3339 UTC text.

```
identity     users(id, name, created_at)  sessions(id, user_id, csrf, created_at, expires_at)

catalog      content(kind, key, pack_id, course_id, position, title, content_hash, doc, retired, updated_at)
               -- one row per entry: pack, course, item, lecture, quiz, challenge, lab, badge; PK (kind, key);
               -- doc = the entry's bundle document; keys: ids, "<course>/<id>" for items, lectures, quizzes,
               -- challenges; other tables refer to an entry by (kind, key)

progress     item_progress(user_id, item_id, status, content_hash, completed_at, last_opened_at)
             notes(id, user_id, item_id, body, created_at, updated_at)

quiz         quiz_attempts(id, user_id, item_id, content_hash, started_at, deadline, submitted_at,
                           questions, answers, score, passed)

labsession   attempts(id, user_id, lab_id, item_id, content_hash, namespace, workspace, started_at, finished_at,
                      status, hints_used, seconds, xp)       -- one running per user (partial unique index)
             step_results(id, attempt_id, step_id, passed, detail, at)
             hints_shown(attempt_id, step_id, idx, at)

challenge    submissions(id, user_id, item_id, content_hash, kind (run|submit), files, verdict, results,
                         runtime_ms, created_at)

rewards      events(id, user_id, kind, subject, data, at)                   -- append-only history
             user_badges(user_id, badge_id, awarded_at)

operations   operations(id, user_id, kind, status, result, created_at, finished_at)   -- 202 + SSE long ops
```

- **Import** (`contentimport`): the bundle is flattened into entries and compared with the catalogue (`Diff`):
  only added, changed (document, position, parent) or returning entries are written, missing ones retired, in one
  transaction. One table instead of one per kind keeps that comparison generic; a pack's Phase 10 curricula stay in
  its document until M2 turns them into courses.
- **Completion**: lectures by `item_progress`; quizzes by a passed `quiz_attempts` row; labs by a passed `attempts`
  row for that lab (a lab passed in one course completes it in every course); challenges by an `accepted`
  submission. Stale when the completion's `content_hash` differs from the item's current one.
- **Legacy import** (`academy migrate-legacy .opsforge/labs.db`): Phase 10's attempts, step results, events, badges
  and awards; hints become `hints_shown` rows from `hint.shown` events.

## Rewards

- XP: labs and challenges give `xp` less hints (at least half), quizzes `xp` on first pass, lectures nothing; best
  result per item counts.
- Badge rule types (registry in `rewards`, each a small strategy type with its own tests): `labs_passed`,
  `lab_passed`, `xp` (Phase 10), plus `course_complete`, `quiz_score` (quiz, min %), `quizzes_perfect` (min count),
  `challenges_accepted` (min, difficulty), `streak_days` (min).
- A new rule is evaluated against the whole history at import (retroactive awards, as in Phase 10).

## API resources (`/api/v1`, OpenAPI 3.1 in `academy/api/openapi.yaml`)

| Method and path | Purpose |
|---|---|
| `POST /sessions` · `GET /me` | sign the local user in (cookie); user, CSRF token, XP, badge counts, running lab, resume item |
| `GET /courses` · `GET /courses/{course}` | catalogue; course page with sections, items, statuses, requirement check against the lab |
| `GET /courses/{course}/items/{item}` | the item (lecture Markdown, quiz without keys, lab view, challenge without solution) |
| `POST /courses/{course}/items/{item}/complete` | complete a lecture |
| `GET/PUT /courses/{course}/items/{item}/notes` | notes |
| `POST /courses/{course}/items/{item}/quiz-attempts` · `POST /quiz-attempts/{id}/submit` | start (draw, deadline) and submit a quiz; per-question results |
| `POST /labs/{lab}/attempts` (202) · `POST /attempts/{id}/checks` (202) · `POST /attempts/{id}/hints` · `POST /attempts/{id}/reset` (202) · `POST /attempts/{id}/abandon` (202) | lab session |
| `POST /courses/{course}/items/{item}/runs` (202) · `POST .../submissions` (202) · `GET .../submissions` · `GET .../solution` | challenge |
| `GET /workspaces/{ws}/tree` · `GET/PUT/DELETE /workspaces/{ws}/files/{path}` · `POST /workspaces/{ws}/moves` | editor files |
| `POST /terminals` → `{session}` · `WS /ws/terminal/{session}` | terminal sessions |
| `GET /schemas/kubernetes/{version}/{kind}.json` | JSON Schemas for monaco-yaml |
| `GET /lab/state` · `GET /events` (SSE) | lab map; live events |
| `GET /operations/{id}` | long operation status and result |
| `GET /badges` | badges with awards |
| `POST /admin/import` | re-import packs (dev) |
