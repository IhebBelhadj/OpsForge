# OpsForge

A multi-site WAN lab on one Linux host. OpsForge provisions a distributed environment spread over simulated
distant sites (think multi-cloud / multi-region), connected through a simulated internet with configurable
latency, jitter, packet loss and bandwidth. Use it to build, run and break distributed systems under realistic
network conditions, with a minimal RAM footprint.

Nodes are [Incus](https://linuxcontainers.org/incus/) system containers: each behaves like its own machine
(network stack, systemd, SSH) while sharing the host kernel, so a multi-site cluster idles at well under 2 GB.

> **Status:** Phase 1 of [the plan](docs/PLAN.md): sites, gateways and a shaped WAN core. Nodes are plain
> Debian machines for now; provisioning (Phase 2) and Kubernetes (Phase 4) come next.

## Requirements

- Linux host (developed on Arch Linux; `scripts/host-setup.sh` uses pacman)
- ~3 GB free RAM and ~35 GB free disk (the lab uses 5–20 GB depending on phase)

## Quick start

```bash
sudo ./scripts/host-setup.sh   # one-time: Incus, kernel modules, sysctls, Docker coexistence
newgrp incus-admin             # or log out and back in
./opsforge preflight           # check the host is ready
./opsforge smoke               # launch a throwaway container and test networking

./opsforge up                  # create the lab from topology.yml (~10 s once images are cached)
./opsforge status              # instances, memory, live link conditions
./opsforge verify              # latency, isolation, internet and bandwidth checks
incus exec b-w1 -- ping 203.0.113.10   # ~40 ms: site b to site a's public IP
./opsforge link a-b --rtt 200ms --loss 2%   # change a link live
./opsforge down                # delete everything (keeps cached images; --purge drops the router image)
```

## How it is wired

```
 VPC of-a 10.10.0.0/24          VPC of-b 10.20.0.0/24          VPC of-c 10.30.0.0/24
 a-cp1 .11                      b-w1 .11                       c-w1 .11
   │                              │                              │
 a-gw .1 / 203.0.113.10         b-gw .1 / 203.0.113.20         c-gw .1 / 203.0.113.30
   │ of-a-wan                     │ of-b-wan                     │ of-c-wan
   └──────────────────── wan-core 203.0.113.1 ───────────────────┘
                 netem/HTB per site pair · DNS · eth0 → incusbr0 → real internet
```

- Each **gateway** NATs its VPC behind the site's public IP and drops unsolicited inbound traffic except ping
  and the ports a node declares with `expose`.
- **wan-core** is the only path between sites, so it is where latency, jitter, loss and bandwidth are applied.
  It also gives the lab internet access, but never routes to private addresses: VPCs cannot reach each other
  (or your LAN) except through public IPs and port forwards, as between real clouds.
- Nothing on the host gets an address on the lab networks.

## Measured footprint

| Phase | What runs | RAM (idle) | Disk |
|---|---|---|---|
| 0 | 1 × Debian 13 container (`opsforge smoke`) | 113 MiB | 101 MiB image, 441 MiB pool |
| 1 | `wan-core` + 3 gateways (Alpine) + 3 Debian 13 nodes | 312 MiB (routers 6–9 MiB each) | +5.8 MiB router image, 504 MiB pool |

## Topology

Everything is defined in [`topology.yml`](topology.yml): sites (each a private VPC behind a gateway with a
public IP), WAN links between sites, and nodes. `rtt` is round-trip, `jitter` is the RTT standard deviation
(`ping`'s mdev), `loss` and `rate` apply per direction.

```yaml
links:
  a-b: { rtt: 40ms, jitter: 5ms, loss: 0.1%, rate: 100mbit }
  a-c: { rtt: 120ms, jitter: 15ms, loss: 1%, rate: 20mbit }
```

Validate it with `./opsforge validate`; see what `up` would create with `./opsforge render`.
Expose a node's port on its site's public IP with `expose: ["8080:80/tcp"]` on the node.

## Development

```bash
make check   # shellcheck, yamllint, Python syntax, topology validation
```

See [docs/PLAN.md](docs/PLAN.md) for the architecture and roadmap.
