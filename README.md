# OpsForge

A multi-site WAN lab on one Linux host. OpsForge provisions a distributed environment spread over simulated
distant sites (think multi-cloud / multi-region), connected through a simulated internet with configurable
latency, jitter, packet loss and bandwidth. Use it to build, run and break distributed systems under realistic
network conditions, with a minimal RAM footprint.

Nodes are [Incus](https://linuxcontainers.org/incus/) system containers: each behaves like its own machine
(network stack, systemd, SSH) while sharing the host kernel, so a multi-site cluster idles at well under 2 GB.

> **Status:** Phase 0 of [the plan](docs/PLAN.md): host setup and CLI skeleton.

## Requirements

- Linux host (developed on Arch Linux; `scripts/host-setup.sh` uses pacman)
- ~3 GB free RAM and ~35 GB free disk (the lab uses 5–20 GB depending on phase)

## Quick start

```bash
sudo ./scripts/host-setup.sh   # one-time: Incus, kernel modules, sysctls, Docker coexistence
newgrp incus-admin             # or log out and back in
./opsforge preflight           # check the host is ready
./opsforge smoke               # launch a throwaway container and test networking
```

## Measured footprint

| Phase | What runs | RAM (idle) | Disk |
|---|---|---|---|
| 0 | 1 × Debian 13 container (`opsforge smoke`) | 113 MiB | 101 MiB image, 441 MiB pool |

## Topology

Everything is defined in [`topology.yml`](topology.yml): sites (each a private VPC behind a gateway with a
public IP), WAN links between sites, and nodes.

```yaml
links:
  a-b: { rtt: 40ms, jitter: 5ms, loss: 0.1%, rate: 100mbit }
  a-c: { rtt: 120ms, jitter: 15ms, loss: 1%, rate: 20mbit }
```

Validate it with `./opsforge validate`.

## Development

```bash
make check   # shellcheck, yamllint, Python syntax, topology validation
```

See [docs/PLAN.md](docs/PLAN.md) for the architecture and roadmap.
