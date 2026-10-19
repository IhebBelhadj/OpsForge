# OpsForge

Lab that provisions a **distributed environment spread over simulated distant sites** (multi-cloud /
multi-region) on one Linux host, with realistic WAN conditions and minimal RAM. Read `docs/PLAN.md` before
structural changes; it has the phases, acceptance criteria and design decisions.

Current status: **Phase 0 done** (2026-10-19): host set up, `preflight`/`smoke`/`make check` green.
**Next: Phase 1** (topology → Incus networks, site gateways, `wan-core` router with netem/HTB; `opsforge up/down/link`).
Nothing is committed yet: the v1 removal and all v2 files are uncommitted on `master`.

## Layout / index

| Path | What it is |
|---|---|
| `docs/PLAN.md` | Roadmap: Incus system containers + per-site gateways + WAN core router (netem) + K3s. |
| `topology.yml` | Single source of truth: transit net, sites (VPC + public IP), links (rtt/jitter/loss/rate), nodes. |
| `lab/topology.schema.json` | JSON Schema for `topology.yml`; cross-reference/address checks live in `opsforge` (`topology_errors`). |
| `opsforge` | Python 3 CLI (stdlib + PyYAML + jsonschema). Implemented: `validate`, `preflight`, `smoke`. Others are stubs labelled with their phase (`PLANNED`). |
| `scripts/host-setup.sh` | One-time, idempotent **root** setup for Arch: packages, subuid/subgid, kernel modules, sysctls, Docker coexistence, Incus preseed, `incus-admin` group. |
| `lab/host/` | Files installed by host-setup: `modules-load.conf`, `sysctl.conf`, `incus-preseed.yaml`, Docker `DOCKER-USER` rules script + systemd unit. |
| `Makefile` | `make check` (shellcheck, yamllint, py_compile, ansible-lint when `ansible/` exists, topology validation), `make preflight`, `make smoke`. |

## Host facts that drive the design

- Arch Linux, 12 threads, 14 GB RAM, ext4 root (~59 GB free): RAM is the tight resource. Prefer the lightest
  option and measure (`./opsforge smoke` prints idle container memory). Measured: Debian 13 container idles at
  113 MiB. Incus storage pool `default` is btrfs on a sparse loop file capped at 25 GiB.
- **Partial-upgrade state**: the pacman DB is synced but ~729 updates are not installed. Don't `pacman -S` new
  packages until the user has run `pacman -Syu` (+ reboot); the local `shellcheck` is broken because of this, so
  the Makefile falls back to the `koalaman/shellcheck` Docker image.
- **incus-admin group and tmux**: the user's tmux server predates their `incus-admin` membership, so new panes
  lack it. If `incus` reports permission denied, the user must run `newgrp incus-admin` then `claude --continue`
  (or restart tmux). `sg` is not installed.
- `sudo` needs a password: never run privileged commands yourself. Put them in `scripts/host-setup.sh` (or a new
  root script) and ask the user to run it with `! sudo ...`.
- Docker is installed and running. Its iptables `FORWARD` DROP policy drops Incus bridge traffic unless
  `opsforge-docker-user.service` adds `DOCKER-USER` accepts for `incusbr+` and `of-+`.

## Conventions

- **Network naming/addressing**: lab Incus networks are prefixed `of-` (interface names ≤ 15 chars, so site ids
  ≤ 6 chars). Never reuse reserved ranges: `10.99.0.0/24` (incusbr0), `172.17.0.0/16` (docker0),
  `10.42.0.0/16`/`10.43.0.0/16` (K3s). These are enforced in `RESERVED_NETS` in `opsforge`; keep it in sync
  with `lab/host/incus-preseed.yaml`.
- Link semantics: `rtt` is round-trip (applied rtt/2 per direction); `loss`/`rate` are per direction.
- Values duplicated between files must stay in sync: modules (`lab/host/modules-load.conf` ↔ `REQUIRED_MODULES`),
  sysctls (`lab/host/sysctl.conf` ↔ `REQUIRED_SYSCTLS`).
- Shell: bash, `set -euo pipefail`, tabs, must pass shellcheck. YAML must pass `yamllint` (`.yamllint`).
- After finishing a phase: tick it in `docs/PLAN.md`, record a measured RAM snapshot, update these CLAUDE.md files.
- Commit messages: `feat(<scope>): <message>`. Default branch: `master`.

## Verify

```bash
make check            # lint + topology validation (needs host-setup's dev tools)
./opsforge preflight  # host readiness
./opsforge smoke      # Phase 0 acceptance: container boots, gets IP, reaches the internet
```
