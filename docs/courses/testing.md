# Testing and TDD

## Workflow: tests first, outside in

Every feature follows the same loop; a pull request without the first two steps is not reviewed.

1. **Contract.** Change `academy/api/openapi.yaml` (and `bundle.schema.json` or the toolchain contract if they move).
   Regenerate the server interface and the web client. A **contract test** for the new endpoint fails (request and
   response validated against the spec by `kin-openapi`).
2. **Service test.** Write the application-service test with fakes for every port (table-driven, one row per rule
   in the acceptance criteria, including the failure rows: another lab running, step out of order, quiz past its
   deadline, path escaping the workspace). It fails.
3. **Implement** the domain and service until the test passes; then the adapter (repository against a real temp
   SQLite, the toolchain adapter against a fake `opsforge` script), each with its own tests written first.
4. **Wire the handler**; the contract test passes.
5. **UI**: a component test (Vitest + Testing Library + MSW returning spec-valid fixtures) for each state of the
   screen (loading, empty, error, success), then the Playwright journey if the feature is user-visible end to end.
6. **Live acceptance** for anything that touches the lab: the lab or challenge is run on the real lab and the run is
   recorded (Definition of done, implementation.md).

Red → green → refactor, with refactors only on green. Bugs start as a failing test that reproduces them.

## Test pyramid

| Layer | Tool | What | Runs |
|---|---|---|---|
| Domain and services (Go) | `go test`, table-driven, hand-written fakes (no mocking framework), `testing/synctest` for timers | All business rules: unlocks, staleness, scoring, hint costs, one running lab, verdicts, retroactive badges, import idempotency | every commit, `-race`, < 10 s |
| Repositories (Go) | real SQLite in `t.TempDir()`, goose migrations applied | Queries, constraints (partial unique index), migration up from every released version | every commit |
| Toolchain adapter (Go) | a fake `opsforge` executable (test binary re-exec) emitting fixture JSON, slow and failing variants | Contract parsing, timeouts, cancellation, version mismatch, stderr problems | every commit |
| HTTP (Go) | `httptest` + the real router + services with fake ports; `kin-openapi` response validation | Every endpoint and status code against the spec; Host/Origin/CSRF middleware; problem+json mapping; SSE framing; WebSocket upgrade and pty echo | every commit |
| Toolchain (Python) | pytest (existing 149 tests) + new tests for `opsforge api` commands | Compile (schema, cross-references, hashes, determinism), probe early stop, apply diagnostics; golden bundle fixture shared with the Go import tests | every commit |
| Shared fixtures | `testdata/bundle.golden.json`, `testdata/state.*.json` | The same documents feed Python (producer) and Go (consumer) tests: contract drift fails both | every commit |
| Web components | Vitest, Testing Library, MSW (handlers generated from OpenAPI), `vitest-axe` | Each screen state; quiz navigator logic; editor workspace model (dirty, external change, save conflict) with Monaco mocked; terminal hook with a fake WebSocket | every commit |
| End to end | Playwright against `academy serve --lab=fake` (fake `LabController`, seeded bundle), Chromium + Firefox, desktop and 390 px | Journeys: start a course → read → pass a quiz → pass a lab → accept a challenge → completion badge; keyboard-only run; reconnecting terminal | every PR |
| Visual | Playwright screenshots of key screens, light and dark | Unintended layout changes | every PR (reviewed diffs) |
| Live acceptance | `opsforge academy check <lab|challenge>` on the running lab; a scripted run of a whole course | Lab texts, timings and checks are true on the real cluster; teardown leaves `verify` green | per lab/challenge before merge; full run per milestone |

## Fakes, not mocks

- Ports are small interfaces declared where they are consumed, so fakes are a few lines: `fakeLabController` with
  scripted responses and a call log, `fakeClock`, `seqIDs`, `memWorkspace`.
- No `gomock`/`mockery`: tests assert outcomes (state, events, returned errors), not call sequences, except where the
  call is the outcome (teardown must run after a failed setup: asserted through the fake's log).

## Determinism

- Time, randomness (quiz draws and shuffles) and ids are injected (`Clock`, `Random` seeded per test, `IDGen`).
- No sleeps in tests: `testing/synctest` for timeouts and back-offs in Go, fake timers in Vitest, Playwright's
  auto-waiting with the fake lab completing operations immediately or on a test-controlled signal.

## Coverage and quality gates (CI)

| Gate | Threshold |
|---|---|
| Go | `golangci-lint` (incl. `depguard` layering rules, `errcheck`, `gosec`), `go vet`, `go test -race`; coverage ≥ 85 % in `domain`/`service` packages, ≥ 70 % overall |
| Web | `tsc --noEmit`, ESLint (strict, a11y), Prettier, knip, Vitest coverage ≥ 80 % in `features/*` logic, bundle-size check (initial ≤ 250 KB gzip) |
| Python | existing `make check` (shellcheck, yamllint, ansible-lint, pytest) |
| Contract | spec lint (`redocly lint`), generated code up to date (`go generate` / `pnpm gen` leave no diff) |
| E2E | Playwright suite green on Chromium and Firefox |
| Migrations | up from empty and from every released schema version; down is not supported (forward-only, backups instead) |

Coverage is a floor for review, not a target: a rule without a test is rejected even if the percentage holds.

## CI layout (`.github/workflows/check.yml`, extended)

Jobs in parallel: `python` (existing), `go` (lint, test, build), `web` (lint, typecheck, unit, build),
then `e2e` (needs `go` + `web`: builds `academy` with the embedded web app, runs Playwright with the fake lab).
None needs Incus; live acceptance runs on this host, recorded in the milestone's PLAN entry.
