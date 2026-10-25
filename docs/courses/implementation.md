# Implementation plan

## Repository layout (target)

```
academy/                         Go module (platform service)
  cmd/academy/                   main, Wire injectors (wire.go, wire_gen.go)
  api/                           openapi.yaml, bundle.schema.json, toolchain contract schemas
  internal/
    config/  httpapi/  identity/  catalog/  contentimport/  progress/  quiz/  labsession/
    challenge/  workspace/  terminal/  labstate/  schemas/  rewards/  operations/
    platform/                    shared kernel: ids, clock, errors, event bus, sqlite setup, logging
  migrations/                    goose SQL migrations (embedded)
  web/                           go:embed target (copied from web/dist at build)
  testdata/                      golden bundle, state fixtures, fake opsforge
web/                             React + Vite app (frontend.md "Structure")
lab/                             Python lab toolchain (existing) + academy.py (`opsforge api` helpers)
labs/                            content packs (courses, labs, quizzes, challenges)
docs/courses/                    this plan
```

`lab/labserver.py`, `lab/labsdb.py` and the current `web/` (Phase 10 static UI) are deleted in M2 after parity;
`lab/labs.py` stays (validation and compile).

## Spikes (before the milestone that needs them; each ends with a note in PLAN.md)

| Id | Question | Method | Needed by |
|---|---|---|---|
| S1 | Does K3s's embedded network-policy controller enforce policies in the unprivileged containers? | Default-deny + allow manifest on the live lab; probe both directions | M6 (NetworkPolicy lab) |
| S2 | metrics-server: RAM, and does it scrape kubelets in user-namespaced nodes? | Remove `metrics-server` from K3s `disable`, measure anon+kernel, `kubectl top`, an HPA | M7 (autoscaling lab) |
| S3 | Pod Security Admission levels enforced as expected in K3s 1.37 | Label a namespace `restricted`, try root and non-root pods | M7 |
| S4 | Snapshot reset: time and side effects of restoring `academy-baseline` (forwards, kubeconfigs, SSH masters) | `opsforge snapshot`/`restore` on the live lab, then `verify` | M7 (capstone) |
| S5 | Monaco + monaco-yaml under Vite with self-hosted workers, schemas from the cluster's `/openapi/v3` (size, conversion time, CRDs) | Prototype route in `web/`, real cluster schemas | M3 |
| S6 | Go pty + WebSocket terminal parity with the Phase 10 one (resize, bracketed paste, reconnect replay) | Prototype in `academy/internal/terminal` with tests | M1 |

## Milestones

Each milestone ends with: its acceptance criteria met, `make check` + CI green, a PLAN.md entry with measurements
(RAM of `academy`, build sizes), CLAUDE.md updated.

### M0: Foundations (scaffolding, no features)
- `academy/` Go module: config, slog, `/healthz` `/readyz`, graceful shutdown, Wire graph, goose + sqlc set up,
  golangci-lint with layering rules, Makefile targets (`academy`, `academy-test`, `gen`).
- `web/` replaced by a Vite + React + TS app with Tailwind v4 and shadcn initialised (theme tokens from Phase 10),
  TanStack Router/Query, ESLint/Prettier/Vitest/Playwright configured, `@fontsource` fonts.
- `openapi.yaml` skeleton + generation (`oapi-codegen`, `openapi-typescript`) wired into `make gen`; CI jobs `go`,
  `web`, `e2e` (one smoke spec).
- `opsforge academy` command group with `compile` and `state` (Python, tests first), golden bundle fixture.
- **Done when:** `make academy` builds one binary serving the empty app shell at 127.0.0.1:8090 with health
  endpoints; CI green on all jobs; generated code reproducible.

### M1: Platform core (backend first)
- Modules `identity`, `catalog`, `contentimport`, `progress`, `rewards`, `labstate`, `operations` with
  repositories, services and endpoints (tests first, testing.md).
- `labsession` on the `LabController` port with the `opsforgecli` adapter (`run`, `probe`, `heal`) and the fake;
  SSE hub; `terminal` (S6) and `workspace` modules.
- Security middleware (Host, Origin, cookie session, CSRF), problem+json.
- `academy migrate-legacy` from Phase 10's `labs.db`.
- **Done when:** the six existing labs can be started, checked, hinted, reset, abandoned and passed through the API
  against the live lab with the same results as Phase 10; import twice = no change; editing a lab flags passes; a
  badge added later is awarded from history (the Phase 10 acceptance, re-proved on the new stack); contract tests
  cover every endpoint.

### Repository split (between M1 and M2)
Academy moves to its own repository once M1's acceptance run passes and the contract is frozen at v1; the steps
and acceptance are in [PLAN.md](../PLAN.md) (Phase 11, "Split into two repositories"). From M2 on, paths in this plan
(`academy/`, `web/`, `labs/`) refer to the Academy repository; OpsForge keeps only the lab
toolchain behind the contract.

### M2: Player and parity (frontend), Phase 10 server removed
- Screens: catalogue, course page, player shell with curriculum sidebar, lecture view, quiz view and results, lab
  view (Killercoda layout, terminal tab, lab map with Animated Beam), badges, command palette, settings.
- The two Phase 10 curricula converted into courses (*Cloud networking basics*, a first *Kubernetes Fundamentals*
  draft holding the three existing cluster labs), with their first lectures and quizzes.
- Delete `lab/labserver.py`, `lab/labsdb.py`, the old `web/`; `opsforge serve` starts `academy`.
- **Done when:** every Phase 10 lab is passed end to end in the new UI on the live lab; Playwright journeys green
  on the fake lab; Lighthouse ≥ 90/100 on catalogue and course page; nothing of the Phase 10 server remains.

### M3: Editor and challenges
- `editor` feature (S5): Monaco + monaco-yaml, explorer, tabs, autosave, external-change handling, Validate,
  Apply, Diff with cluster; `schemas` module serving cluster schemas.
- Lab Editor tab (`files`, step `open`); `challenge` module and screen (Run, Submit, verdicts, submissions, solution
  diff); toolchain `apply` command.
- **Done when:** a challenge is solved in the browser editor with completion and validation from the live cluster's
  schemas, Run shows dry-run errors as editor markers, Submit returns per-test verdicts on the live lab and leaves no
  namespace behind; editor ready ≤ 1.5 s.

### M4: Course 1, Kubernetes Fundamentals
- All lectures, quizzes, labs and challenges of course 1 (kubernetes-syllabus.md), each meeting the Definition of
  done.
- **Done when:** a scripted full run of the course on the live lab passes every item and leaves `verify` green.

### M5: Course 2, Configuration and Workloads
### M6: Course 3, Networking and Scheduling across Regions (spike S1 first)
### M7: Course 4, Production Kubernetes (spikes S2, S3, S4 first)
Same acceptance as M4, per course. M7 also enables metrics-server if S2 holds (topology/Ansible change, RAM recorded).

### M8: Hardening
- Accessibility audit (keyboard-only journeys, screen reader pass on quiz and lab), visual regression baselines,
  performance budgets enforced in CI, error and empty states reviewed, `/metrics` dashboard in the lab's Grafana,
  backup/restore of `academy.db` documented, README and CLAUDE.md rewritten for the new stack.

## Definition of done (content items)

| Item | Done when |
|---|---|
| Lecture | Compiles; facts checked against the live lab where it shows output (commands run, output pasted from the run); diagrams render light and dark; reviewed for plain language. |
| Quiz | Compiles; every question has an explanation and, where possible, `see`; the answer key reviewed by a second read; no question answerable only by trivia. |
| Lab | `opsforge academy check <lab>` run on the live lab: every step passes after doing it and fails before (the "not yet" path answers fast); timings in the text match the run; teardown leaves `verify --quick` green; run recorded in the milestone's PLAN entry. |
| Challenge | The reference solution is accepted; at least two wrong solutions get the intended `wrong_answer` case; an invalid manifest gets `invalid_manifest`; no namespace left after submit. |

## Risks

| Risk | Mitigation |
|---|---|
| Scope: ~180 content items is months of writing | Course by course (M4-M7); a course ships only complete; templates and `academy check` make authoring a loop |
| Two backend languages (Go platform, Python lab) | One narrow, versioned JSON contract; shared golden fixtures tested on both sides; no shared database or code |
| RAM: lab workloads per lab, metrics-server | 300 MiB per-lab budget (syllabus), S2 measures metrics-server, `verify --mem` after each course run |
| Disk: node_modules, Go caches, images (host has ~33 GB free) | pnpm store dedup, `go clean -cache` in docs, `opsforge gc`; check free space in `preflight` |
| Borrowed components drift from our look or bloat the bundle | Copied into the repo (owned), restyled with tokens, bundle budget in CI, motion only via reduced-motion-aware wrappers |
| Monaco bundle size and workers in Vite | Lazy routes, S5 prototype first, CodeMirror fallback for inline snippets |
| Long checks over HTTP (Phase 10 held requests for minutes) | 202 + operations + SSE progress; cancellation on abandon |
| Labs that break the cluster leave it broken | `reset: snapshot` (S4), teardown rule, `verify` after every live run |
| Kubernetes upgrades change behaviour the texts describe | Schemas come from the live cluster; a full course run per milestone and per K3s upgrade |
