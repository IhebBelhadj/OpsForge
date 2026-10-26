# Changelog

OpsForge follows semantic versioning from `v1.0.0`; the API contract's versions are in
[contract/README.md](contract/README.md). Earlier history is in [docs/PLAN.md](docs/PLAN.md) (phase by phase).

## Unreleased

- **Split (2026-10-26):** OpsForge Academy (`academy/`, `web/`, `labs/`, `docs/courses/`) moved to its own repository,
  `opsforge-academy`; this repository is the lab and its API only. `lab/academy.py` → `lab/contract.py`,
  `tests/test_academy.py` → `tests/test_api.py`; `make check` needs Python only.
- Namespaces created by lab work are labelled `app.kubernetes.io/managed-by=opsforge-api` (was `opsforge-academy`).
  The API serves clients on other machines (`api serve --host <addr> --tls-cert --tls-key`); no client reads this
  checkout.
- SSH ControlMaster sockets are `.opsforge/ssh/cm-<host>` (was a 40-character hash that overflowed the Unix socket
  path limit in deeper checkouts).
- Planned before `v1.0.0`: sandboxes in the API (docs/PLAN.md Phase 11, split step 7).
