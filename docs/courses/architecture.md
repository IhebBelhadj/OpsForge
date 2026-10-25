# Architecture

## System context

```
                ┌──────────────────────────── host (127.0.0.1) ────────────────────────────┐
 browser ──────▶│  academy (Go binary)                                                     │
  React SPA     │   ├─ /            embedded web build (go:embed)                          │
  (same origin) │   ├─ /api/v1/*    REST (OpenAPI 3.1)                                     │
                │   ├─ /api/v1/events   SSE: lab state, progress, awards, workspace changes  │
                │   └─ /ws/terminal/*   WebSocket ⇄ pty (bash in the attempt's workspace)    │
                │        │                                                                  │
                │        ├── SQLite  .opsforge/academy.db   (learner state + imported content)│
                │        ├── files   .opsforge/workspaces/<attempt>/  (editor + terminal)    │
                │        └── exec    ./opsforge academy <cmd> --json  (lab toolchain, Python) │
                │                          │                                                │
                │                          └── incus / ssh / kubectl → the lab (wan-core, gateways, K3s) │
                └──────────────────────────────────────────────────────────────────────────┘
 author ── edits labs/<pack>/ ── `opsforge academy compile` → bundle.json ── imported by academy
```

Two programs, one boundary:

- **`academy`** (Go, new, `academy/`): the platform. Owns users, content as imported, progress, quizzes, badges,
  lab sessions, workspaces, terminals, the HTTP API and the web app. Knows nothing about Incus, netem or K3s
  internals.
- **`opsforge`** (Python, existing): the lab. Owns topology, provisioning, faults, and now the **content toolchain**
  (pack validation and compilation) and **check execution** (scenario steps, probes). It already has these
  (`lab/labs.py`, `ScenarioRun`); keeping them in one place means scenarios and labs never diverge.

The boundary is a **versioned JSON CLI contract** (below), not shared code or a shared database.

### Why Go for the platform

| Need | Go | Alternatives considered |
|---|---|---|
| RAM on a host where the lab is the budget | one process ≈ 15-30 MB RSS | Spring Boot 250-400 MB; NestJS/Node 80-120 MB; FastAPI 60-90 MB |
| Terminals, WebSockets, SSE, subprocesses | goroutines, `creack/pty`, `coder/websocket`, `os/exec` with contexts | all possible, Go is the most direct |
| Production structure (modules, interfaces, DI) | interfaces at the consumer, constructor injection, Wire | Spring/Nest have DI frameworks but cost RAM; Python lacks static guarantees |
| Deployment | one static binary with the web app embedded; `go test -race` | runtimes to install |
| Toolchain on this host | Go 1.27 installed (no pacman needed) | Java 17 and Node 22 installed too |

## Platform service: modules

A **modular monolith**: each module is a Go package tree with its own domain types, application service,
ports (interfaces it needs) and adapters (implementations), and talks to other modules only through their service
interfaces or domain events. Splitting a module into a service later means replacing an in-process interface with a
client, nothing else.

| Module | Responsibility | Depends on (ports) |
|---|---|---|
| `identity` | users, local session (cookie), CSRF token; one local user now | `UserRepo`, `Clock` |
| `catalog` | courses, sections, items, lectures, quizzes, challenges, labs **as imported**; content queries; retired content | `ContentRepo` |
| `contentimport` | runs `opsforge academy compile`, validates the bundle against its JSON Schema, upserts idempotently, reports changes | `Toolchain`, `ContentRepo`, `EventBus` |
| `progress` | item status, course progress, resume point, stale revisions, notes | `ProgressRepo`, `catalog.Service`, `EventBus` |
| `quiz` | quiz attempts: draw/shuffle, timing, server-side scoring, explanations | `QuizRepo`, `catalog.Service`, `Random`, `Clock`, `EventBus` |
| `labsession` | lab attempts: start (setup), check (probe), hint, reset, finish/abandon (teardown + heal); one running lab; busy lock | `AttemptRepo`, `LabController`, `Workspace`, `catalog.Service`, `EventBus`, `Clock` |
| `challenge` | challenge submissions: run (validate + server dry-run), submit (apply in a scratch namespace + tests), results, reference solution after pass | `SubmissionRepo`, `LabController`, `Workspace`, `EventBus` |
| `workspace` | per-attempt file trees (list, read, write, rename, delete), path confinement, size limits, change notifications | `FileStore`, `Watcher` |
| `terminal` | pty sessions bound to an attempt's workspace, resize, idle timeout, one shell per tab | `PtySpawner` |
| `labstate` | live lab state for the mimic panel (instances, links, faults), cached and pushed | `LabController`, `EventBus` |
| `schemas` | Kubernetes JSON Schemas for the editor, generated from the cluster's `/openapi/v3`, cached per server version | `LabController` (kubectl raw) |
| `rewards` | XP, badge rules over the event history, awards (retroactive) | `RewardRepo`, `EventBus` |
| `history` | the `EventBus`: appends every domain event to the `events` table, pushes it to the user's event streams, runs the in-process handlers (progress notices a completed course) | `Repo`, `Sender` (hub) |
| `operations` | long operations (202): runs the work in the background, stores status and result, pushes each change; restart marks running ones interrupted | `Repo`, `Sender` (hub) |
| `httpapi` | transport only: generated OpenAPI handlers → services; SSE hub; WebSocket upgrade; auth/CSRF/Host/Origin middleware; problem+json errors | services |

Dependency direction: `httpapi` → services → domain; adapters implement ports; **domain imports nothing from
adapters or transport**. Enforced by `go-arch-lint` (or `depguard` rules in golangci-lint) in CI.

### Layers inside a module (example: `labsession`)

```
academy/internal/labsession/
  domain.go        Attempt, Step, CheckResult, errors (ErrAnotherLabRunning, ErrStepOrder) — pure
  service.go       type Service interface { Start; Check; Hint; Reset; Abandon } + implementation
  ports.go         AttemptRepo, LabController, Workspace interfaces (declared where consumed)
  service_test.go  table-driven tests with fakes (written first)
  sqlite/          AttemptRepo on sqlc queries (+ repo tests on a real temp database)
```

### Dependency injection

- **Constructor injection everywhere**: every service takes its ports as interface arguments; no globals, no
  `init()` side effects, no service locator.
- **Wire** (`github.com/google/wire`) generates the object graph in `academy/cmd/academy/wire_gen.go` from provider
  sets per module; compile-time, no reflection. Tests build services by hand with fakes.
- Configuration is one typed `config.Config` (env + flags, validated at start), injected like any dependency.

### Domain events

An in-process `EventBus` (`history.Service.Publish`): the event is appended to the `events` table (the learner's
history, which badge rules read), pushed to the user's event streams (`platform.Hub`, non-blocking: a stream that
falls behind is closed and replays on reconnect), then handed to the subscribed handlers in the publisher's
goroutine. Kinds are `<thing>.<what happened>` (Phase 10's names, so migrated history reads the same):
`lab.started`, `step.checked`, `hint.shown`, `lab.reset`, `lab.passed`, `lab.abandoned`, `lab.failed`,
`quiz.submitted`, `lecture.completed`, `course.completed`, later `challenge.accepted`, `badge.awarded`. Completion
is not derived from events: progress reads each module's rows through a `Source` port (a lab item added later shows
as done when its lab was passed).

## The lab toolchain contract (Go → Python)

`opsforge academy <command> --json`, one JSON document on stdin, one on stdout, exit 0 on success, a problem
document on stderr otherwise. Versioned by `contract: 1` in every document; `academy` refuses a different major.

| Command | In | Out |
|---|---|---|
| `compile [packs...]` | – | the bundle: packs, courses, items, lectures (Markdown), quizzes (with keys), challenges, labs, badges, content hashes, validation errors (bundle schema: `academy/api/bundle.schema.json`) |
| `state` | – | lab up, layout, topology name, sites, links (live/desired/condition), nodes, fault, placeholders (`${node:a}` → value) |
| `run` | `{phase: setup\|teardown, steps, namespace?}` | `{metrics, log, error}`; a namespace (`lab-<attempt>`, `ch-<attempt>`, nothing else) is created before setup and deleted after teardown, even after a failing step, and fills `${ns}` |
| `probe` | `{steps, expect, namespace?}` | `{passed, results: [{metric, want, got, passed, checked}], log, error}` (stops at the first failing named step; results in `expect` order) |
| `apply` | `{manifest, namespace, dry_run: server\|none}` | `{ok, diagnostics: [{path, message}], output}` (kubectl through `opsforge kubectl`) |
| `heal` | – | `{healed}` |
| `openapi` | `{cluster}` | the cluster's OpenAPI v3 documents (for `schemas`) |
| `snapshot-restore` | `{name}` | `{ok, seconds}` (Reset lab for `reset: snapshot` labs) |

- Inputs and outputs of `run`, `probe` and `heal` are `$defs` of `academy/api/toolchain.schema.json`: the CLI checks
  its input against them, the Go adapter its output. A step that cannot run is `error` in the result (the check
  failed), not a problem document (the call failed).
- The Go side wraps it in **`LabController`** (port) with one adapter `opsforgecli` (spawns the CLI with a context
  and timeout, parses, maps errors) and one adapter `fake` for tests and the e2e suite.
- Placeholders (`${node:c}`, `${ns}`, ...): the bundle keeps texts raw (they depend on the running topology, not on
  the pack). For display, `academy` (`labstate`) resolves them with the table `state` returns (`placeholders`: every
  placeholder of the running topology with its value, computed by `scenario.substitute` itself, so there is no second
  implementation to drift), plus `${ns}` from the learner's attempt. For execution (`run`, `probe`, `apply`), the
  toolchain resolves them itself.

## Data

- **SQLite** (`.opsforge/academy.db`, WAL, foreign keys on) through **`modernc.org/sqlite`** (pure Go, no cgo).
  One host, one writer, tiny data: SQLite is the right tool; SQL stays portable (no SQLite-only functions in
  queries; JSON columns are text decoded in Go) so PostgreSQL is a driver + migration-dialect change.
- **sqlc** generates typed query methods from `academy/internal/<module>/sqlite/queries.sql`; no runtime ORM.
  Chosen over GORM/ent: explicit SQL, compile-time checked, no reflection, easy to review.
- **goose** migrations in `academy/migrations/` (embedded), applied at start; never edited once released.
- Each module owns its tables; cross-module reads go through services (a module's repo never queries another
  module's tables).
- Schema: content-model.md. Phase 10's `labs.db` is imported once by `academy migrate-legacy` (attempts, step
  results, events, badges, awards), then deleted with the Phase 10 server.

## HTTP API

- **OpenAPI 3.1 first**: `academy/api/openapi.yaml` is written before handlers. `oapi-codegen` (strict server,
  `net/http` 1.22 routing) generates the server interface; `httpapi` implements it by calling services.
  `openapi-typescript` generates `web/src/api/schema.d.ts`; `openapi-fetch` is the typed client.
- Resources: content-model.md "API". Errors are RFC 9457 `application/problem+json` with stable `type` URIs
  (`/problems/another-lab-running`, ...), mapped from domain errors in one place.
- **SSE** `/api/v1/events` (one stream per tab, hand-written handler, excluded from oapi-codegen): `history` (each
  history event, with its id: the browser's reconnect sends Last-Event-ID and the missed ones are replayed) and
  `operation` (a long operation's new status). Later: `lab-state` (labstate), `workspace` (file watcher),
  `check-progress`. Replaces Phase 10's 2.5 s polling.
- **WebSocket** `/ws/terminal/{session}`: binary frames for pty output, JSON control frames (input, resize).
- Long operations (setup, checks with `wait_until`, submissions) return `202` with an operation id; progress and
  the result arrive on SSE; `GET /api/v1/operations/{id}` for reconnects.

## Security (local, but a shell is one request away)

- Binds 127.0.0.1 only by default. Middleware rejects any `Host` other than `127.0.0.1:<port>`/`localhost:<port>`
  (DNS rebinding) and any state-changing request or WebSocket upgrade whose `Origin` differs (cross-site WebSocket).
- Session cookie `HttpOnly; SameSite=Strict` + CSRF token header on mutations (the local user is created at first
  start; the cookie makes a future multi-user mode an extension, not a redesign). The page calls `GET /api/v1/me`
  (user + CSRF token), and on 401 `POST /api/v1/sessions` (the one open endpoint, still Origin-checked); the token
  is stored only as its SHA-256. Requests without Origin and Sec-Fetch-Site (tools on the machine) pass the origin
  check; the CSRF token still applies to them.
- Workspace paths are confined (`filepath.Rel` + no symlinks out), size-capped (10 MB per workspace, 1 MB per file).
- Quiz keys and challenge reference solutions never leave the server before they are earned.
- Lecture Markdown is rendered without raw HTML (sanitised on the client too).
- `kubectl` runs only through the toolchain with namespaces fixed by the platform, never with learner-supplied flags
  from the API (the terminal is the place for free commands).

## Operations

- `academy serve` (default `127.0.0.1:8090`), `academy migrate-legacy` (`-legacy-db`, default `.opsforge/labs.db`;
  again imports only rows Phase 10 recorded since), `academy version`. Re-import: `POST /api/v1/admin/import`.
  `opsforge serve` becomes a thin wrapper that builds/starts `academy` (one command for the user).
- **Logs**: `log/slog` JSON (request id, module, attempt id). **Metrics**: Prometheus `/metrics` on a separate
  loopback port (request latency, check durations, toolchain errors), scrapeable by the lab's VictoriaMetrics.
- **Health**: `/healthz` (process), `/readyz` (DB + toolchain contract version).
- Graceful shutdown: stops accepting, lets running checks finish (with a cap), closes ptys, checkpoints WAL.
- Build: `make academy` → `pnpm build` (web) then `go build` with the web `dist/` embedded; version stamped with
  `-ldflags`.

## Later (kept possible, not built)

- Multi-user: real authentication, per-user lab sessions queued on the single lab or one lab per user on a bigger
  host; the `identity` module and ownership checks already exist.
- PostgreSQL: sqlc + goose dialect switch.
- Splitting `labsession`/`challenge` into a lab-runner service next to the lab: `LabController` becomes a gRPC
  client.
