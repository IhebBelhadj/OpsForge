# OpsForge Academy: plan

Phase 11 of [docs/PLAN.md](../PLAN.md). Phase 10 proved the idea with a stdlib prototype (`opsforge serve`): labs
with live checks, a mimic panel, badges. This phase builds it as a **production application**: courses structured
like Udemy's (sections of **lectures**, **multiple-choice quizzes**, **labs** and LeetCode-style **coding
challenges**), a VS Code-grade **editor** and terminal, and a first catalogue teaching **Kubernetes from the basics to
an advanced level**, entirely on the OpsForge lab.

| File | What it decides |
|---|---|
| [architecture.md](architecture.md) | System shape: Go platform service (modular monolith, ports and adapters, DI), the Python lab toolchain behind a JSON contract, data, API, security, operations. |
| [frontend.md](frontend.md) | React + Vite + TypeScript app: shadcn/ui and borrowed components (Aceternity, Magic UI, ...), Monaco editor, xterm.js terminal, state, routing, theming. |
| [design.md](design.md) | Screens and interactions, borrowed from Udemy (catalogue, course page, player), LeetCode (quiz, coding challenge) and Killercoda (lab); wireframes; which component builds what. |
| [content-model.md](content-model.md) | Course packs on disk, item types (lecture, quiz, lab, challenge), the compiled bundle, database schema, API resources. |
| [kubernetes-syllabus.md](kubernetes-syllabus.md) | Four Kubernetes courses, section by section, with every lecture, quiz, lab and challenge and what it needs from the lab. |
| [testing.md](testing.md) | TDD workflow and the test pyramid per layer (Go, web, Python, live acceptance); CI gates. |
| [implementation.md](implementation.md) | Milestones with acceptance criteria, spikes, repository layout, risks. |

## Goals

- **Courses, not loose labs.** Catalogue → course page → player, with per-item and per-course progress, "Continue
  where you left off", course completion and badges.
- **Four item types**, each with its own screen: lecture (article), quiz (multiple choice, scored on the server), lab
  (guided steps checked on the live cluster), challenge (write a manifest in the editor; it is dry-run, applied and
  tested like a LeetCode submission).
- **A real editor**: Monaco (the editor inside VS Code) with Kubernetes-aware YAML (completion, hover docs,
  validation from the cluster's own OpenAPI schema), tabs, a file tree, diff view, command palette; files shared with
  the terminal.
- **Production engineering**: separation of concerns, modules with explicit ports, dependency injection, an
  OpenAPI contract with generated server and client code, migrations, structured logs and metrics, tests written
  first, CI gates.
- **Kubernetes from zero to advanced**, leaning on what makes this lab different from a cloud sandbox: real regions,
  a real WAN, failures you cause yourself.
- **Extensible content**: courses are files in packs; the platform imports a compiled, validated bundle; learner
  state lives only in the database.

## Non-goals (this phase)

- Video, payments, instructor tools, reviews, forums, certificates with external validity.
- Several learners at once: one lab cluster on one host. The model keeps users, sessions and ownership checks so a
  multi-user mode can come later without a rewrite (architecture.md "Later").
- Microservices: the modules run in one process (architecture.md explains the boundaries that allow splitting later).

## Decisions (summary; reasons in the linked files)

| # | Decision | Where |
|---|---|---|
| 1 | Platform backend in **Go**: one static binary (~20 MB RSS), goroutines for terminals/WebSockets/SSE, strict types | architecture.md |
| 2 | **Modular monolith**, hexagonal (domain, application services, ports, adapters), constructor injection wired by **Wire** | architecture.md |
| 3 | **SQLite** now (one host), via **sqlc** (type-safe SQL, no runtime ORM) and **goose** migrations; SQL kept portable to PostgreSQL | architecture.md |
| 4 | **OpenAPI 3.1 first**: `oapi-codegen` generates the Go server interfaces, `openapi-typescript` + `openapi-fetch` the web client | architecture.md |
| 5 | The **Python `opsforge` CLI stays the lab toolchain**: it compiles packs into a validated bundle and runs setup/checks/teardown (`ScenarioRun`); Go calls it through a versioned JSON contract | architecture.md |
| 6 | Web app: **React 19 + Vite + TypeScript**, **TanStack Router/Query**, **Tailwind v4 + shadcn/ui**; components from Aceternity UI, Magic UI and shadcn blocks instead of hand-made ones; built into the Go binary (`go:embed`) | frontend.md |
| 7 | Editor: **Monaco** (`@monaco-editor/react`, self-hosted) + **monaco-yaml**; escalation path `@codingame/monaco-vscode-api` (VS Code services) if needed; full VS Code (code-server) rejected for RAM | frontend.md |
| 8 | Terminal: **xterm.js** (what VS Code uses) over a WebSocket to a Go pty | frontend.md |
| 9 | Courses replace Phase 10's curricula/levels; labs stay reusable units; Phase 10's progress is migrated | content-model.md |
| 10 | Layouts from Udemy/LeetCode/Killercoda, identity from Phase 10 (tokens, type, mimic panel); no logos or brand colours copied | design.md |
| 11 | **Tests first** for every service and endpoint; every lab verified live before it ships | testing.md |
| 12 | The Phase 10 stdlib server (`lab/labserver.py`, `lab/labsdb.py`, `web/`) is removed once the platform reaches parity (M2), not kept as legacy | implementation.md |

## Size

Four Kubernetes courses (26 sections): 56 lectures, 25 quizzes (≈ 250 questions), 29 lab slots (26 new labs,
3 existing reused) and 20 challenges; plus *Cloud networking basics* from the existing labs. Delivered in
milestones (implementation.md): **M0** foundations, **M1** platform core, **M2** player and parity (Phase 10 server
removed), **M3** editor and challenges, **M4-M7** one Kubernetes course each, **M8** hardening.
