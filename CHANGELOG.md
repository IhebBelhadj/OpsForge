# Changelog

OpsForge follows semantic versioning from `v1.0.0`; the API contract's versions are in
[contract/README.md](contract/README.md). Earlier history is in [docs/PLAN.md](docs/PLAN.md) (phase by phase).

## Unreleased

- **Sandboxes (2026-10-26, split step 7, OpsForge's side):** a client's lab work happens in a sandbox, one per
  lab or challenge attempt: `POST /v1/sandboxes` (requirements checked against the running lab: 409
  `lab-down`/`unmet-requirements`), its files, terminal, probes, actions and reset under `/v1/sandboxes/{id}/`,
  `DELETE` (teardown, namespace `sb-<id>`, heal, workspace). OpsForge cleans up: a failed setup at once, idle
  sandboxes after `expires_after`, lab work a killed API left halfway after a restart; `opsforge api sandbox list |
  delete <id>` for the lab's owner. **Removed** (contract still unreleased): `/v1/runs`, `/v1/probes`,
  `/v1/workspaces`, `/v1/terminal` and the `namespace` inputs. Jobs carry `sandbox`; problems may carry `unmet`.
  Host steps have `opsforge` on the PATH (`lab/bin`); in a sandbox they run in its workspace and `opsforge kubectl`
  defaults to its namespace.

- **Split (2026-10-26):** OpsForge Academy (`academy/`, `web/`, `labs/`, `docs/courses/`) moved to its own repository,
  `opsforge-academy`; this repository is the lab and its API only. `lab/academy.py` → `lab/contract.py`,
  `tests/test_academy.py` → `tests/test_api.py`; `make check` needs Python only.
- Namespaces created by lab work are labelled `app.kubernetes.io/managed-by=opsforge-api` (was `opsforge-academy`).
  The API serves clients on other machines (`api serve --host <addr> --tls-cert --tls-key`); no client reads this
  checkout.
- SSH ControlMaster sockets are `.opsforge/ssh/cm-<host>` (was a 40-character hash that overflowed the Unix socket
  path limit in deeper checkouts).
- Planned before `v1.0.0`: sandboxes in the API (docs/PLAN.md Phase 11, split step 7).
