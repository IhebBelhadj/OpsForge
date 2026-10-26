# OpsForge

A multi-site WAN lab on one Linux host. OpsForge provisions a distributed environment spread over simulated
distant sites (think multi-cloud / multi-region), connected through a simulated internet with configurable
latency, jitter, packet loss and bandwidth. Use it to build, run and break distributed systems under realistic
network conditions, with a minimal RAM footprint.

Nodes are [Incus](https://linuxcontainers.org/incus/) system containers: each behaves like its own machine
(network stack, systemd, SSH) while sharing the host kernel, so a multi-site cluster idles at well under 2 GB.

> **Status:** Phase 11 of [the plan](docs/PLAN.md): sites, gateways, a shaped WAN core, Ansible provisioning over
> SSH, a WireGuard + OSPF mesh between sites, a K3s cluster stretched across them, or one cluster per site linked by
> Skupper (`topologies/multi-cluster.yml`), WAN-aware monitoring (VictoriaMetrics + Grafana, site-pair probes),
> scripted fault-injection scenarios, optional CI/CD (Gitea Actions, a registry and Flux GitOps across the WAN),
> geo-aware DNS (`www.global.lab` resolves to the nearest healthy site's ingress), and an HTTP + WebSocket API
> (`opsforge api serve`, [contract/](contract/README.md)) for tools built on top of the lab.

**OpsForge Academy**, the learning platform built on this lab (courses, quizzes, guided labs, challenges), is its own
repository (`opsforge-academy`): it needs only this API's URL and token, never this checkout, and may run on
another machine (`opsforge api serve --host <addr> --tls-cert ... --tls-key ...`: off loopback the API requires TLS;
or keep it on loopback behind an SSH tunnel). The token opens shells on this host as the user running the API:
treat it like that user's password.

## Requirements

- Linux host (developed on Arch Linux; `scripts/host-setup.sh` uses pacman)
- ~3 GB free RAM and ~35 GB free disk (the lab uses 5–20 GB depending on phase); the optional CI node adds ~0.6 GB
  RAM idle (up to 1 GiB while building); the multi-cluster layout needs ~5 GB free RAM

## Quick start

```bash
sudo ./scripts/host-setup.sh   # one-time: Incus, kernel modules, sysctls, Docker coexistence
newgrp incus-admin             # or log out and back in
./opsforge preflight           # check the host is ready
./opsforge smoke               # launch a throwaway container and test networking

./opsforge up                  # create the lab from topology.yml (~1 min once images are cached); re-run after editing it
./opsforge provision           # configure it with Ansible over SSH (ansible/site.yml); safe to re-run
./opsforge status              # instances, memory, live link conditions
./opsforge verify              # latency, VPN, internet, bandwidth, SSH, DNS, PKI and K3s checks
./opsforge kubectl get nodes -L topology.kubernetes.io/region   # K3s from the host (tunnel started for you)
incus exec wan-core -- wget -qO- http://203.0.113.20/   # site b's ingress, answered by the demo pod in b
./opsforge ssh b-w1            # SSH in through wan-core and site b's gateway, like a remote VPC
./opsforge ssh b-w1 getent hosts a-cp1   # a-cp1.a.lab, resolved by a-gw across the WAN
incus exec b-w1 -- ping 203.0.113.10   # ~40 ms: site b to site a's public IP
incus exec b-w1 -- ping 10.10.0.11     # ~40 ms: a-cp1's private address, through the WireGuard VPN
./opsforge link a-b --rtt 200ms --loss 2%   # change a link live
./opsforge forward b-w1:8080   # tunnel a lab service to 127.0.0.1:8080 (--list, --stop)
./opsforge capture wan-core icmp -d 5 --show   # tcpdump inside an instance, pcap in .opsforge/captures/
./opsforge partition c         # cut site c off the other sites; ./opsforge heal puts the links back
./opsforge degrade a-c --rtt 300ms   # other faults heal undoes: mtu a-b 1400 (black hole), flap a-b --period 10s
./opsforge scenario --list     # scripted experiments with expectations; ./opsforge scenario pmtu-blackhole-a-b
./opsforge forward mon:3000    # Grafana on http://127.0.0.1:3000: WAN matrix, lab hosts, K3s
./opsforge forward ci:3000 --local 13000   # Gitea on http://127.0.0.1:13000 (user opsforge, .opsforge/gitea-admin)
incus exec wan-core -- wget -qO- http://203.0.113.30/app   # the webapp Flux deployed, served in site c
./opsforge scenario ci-rollout # push a commit: build, registry, Flux rollout to every site
export OPSFORGE_TOPOLOGY=topologies/multi-cluster.yml   # another layout: one K3s cluster per site (down first)
./opsforge kubectl --cluster c get pods -A   # each cluster has its own API on the host (6443, 6444, 6445)
./opsforge scenario cluster-failover   # lose the cluster serving a service: Skupper serves it from the next one
./opsforge scenario geo-dns-failover   # a site's ingress goes dark: its clients resolve the next-nearest site
./opsforge down                # delete the lab (keeps cached images and the image cache; --purge drops them)
./opsforge gc                  # delete images left by older opsforge builds
./opsforge snapshot provisioned   # snapshot every instance (btrfs CoW, <1 s); --list, --delete NAME
./opsforge restore provisioned    # roll the whole lab back to it (~15 s)
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
- The host is the **Ansible controller**. It reaches gateways' public IPs through `wan-core` (via `incus exec`,
  so no host route or sudo is needed) and the nodes by `ProxyJump` through their gateway. Keys, host keys and
  the lab CA live in `.opsforge/` (gitignored, kept across `down`; delete it to reset identities).
- Each gateway serves DNS for its site's zone (`a.lab`, `b.lab`, …) and forwards the others across the WAN.
- **K3s** runs in unprivileged containers: the server in site a, agents in b and c, joined across the VPN, with
  flannel's VXLAN inside the WireGuard tunnels. Each gateway forwards its public 80/443 to its site's Traefik, so
  each site has its own entry point, like a regional load balancer. Images come through `opsforge-cache` (zot,
  outside the lab, kept across `down`), which wan-core serves at `203.0.113.2:5000` like a CDN.
- **`mon`** (site b, outside the cluster) scrapes every host across the VPN. Each gateway runs a blackbox exporter
  probing the other sites, so the WAN dashboard shows RTT, loss and HTTP time for every site pair as measured from
  its own end, and follows `opsforge link`/faults within one probe interval.
- **`ci`** (optional, site a) runs Gitea with Actions, a runner that builds images with buildah, and a registry
  (TLS with the lab CA). A push to `opsforge/webapp` builds and pushes the image, then commits its tag to
  `opsforge/deploy`; Flux in the cluster polls that repository across the WAN and rolls the change out, each site
  pulling the image from site a. The `webapp` replicas (one per region) share a PostgreSQL in region a, so each
  response shows what the WAN costs: `Database: 1 ms` in a, ~75 ms in b, ~250 ms in c. Remove the `ci` line from
  `topology.yml` to run without it.
- **Multi-cluster** (`OPSFORGE_TOPOLOGY=topologies/multi-cluster.yml`): the same sites, but each runs its own K3s
  cluster, like regions of different clouds. Skupper links them over the VPN; each link costs its WAN RTT, so a
  service offered by several clusters is served from the nearest one that is up. The webapp's replicas in b and c
  reach PostgreSQL in a through Skupper. Scenarios use `${node:<site>}` and friends, so they run on either layout.

## Measured footprint

| Phase | What runs | RAM (idle) | Disk |
|---|---|---|---|
| 0 | 1 × Debian 13 container (`opsforge smoke`) | 113 MiB | 101 MiB image, 441 MiB pool |
| 1 | `wan-core` + 3 gateways (Alpine) + 3 Debian 13 nodes | 312 MiB (routers 6–9 MiB each) | +5.8 MiB router image, 504 MiB pool |
| 2 | Phase 1 + sshd, dnsmasq, Ansible-provisioned | 1042 MiB as reported, of which 921 MiB reclaimable page cache; 101 MiB anon + kernel | node image 126 MiB, router image 21 MiB, 1.5 GiB pool |
| 3 | Phase 2 + WireGuard mesh + BIRD (OSPF) on the gateways | 945 MiB as reported; 111 MiB anon + kernel (gateways 6 MiB each) | router image 22.6 MiB, 1.9 GiB pool |
| 4 | Phase 3 + K3s (server + 2 agents, Traefik on each, demo pods) + zot image cache | 1182 MiB anon + kernel (a-cp1 757, agents ~172 each, cache 55) | cache image 67 MiB, 4.1 GiB pool |
| 5–6 | Phase 4 + `mon` (VictoriaMetrics + Grafana) + node/blackbox exporters | 1635 MiB anon + kernel (mon 326, gateways 28 each) | 6.5 GiB pool (Grafana 1.4 GiB) |
| 7 | Phase 6 + `ci` (Gitea, runner, registry) + Flux + webapp/PostgreSQL | 2217 MiB anon + kernel (ci 234, mon 406, agents ~300) | 8.95 GiB pool (ci ~1.4 GiB) |
| 8 | Multi-cluster layout: 3 K3s servers (2 GiB limit each) + Skupper, with mon and ci | 4450 MiB anon + kernel (servers ~1.2 GiB each, mon 467, ci 188) | 12 GiB pool |

Incus reports memory including page cache, which the kernel reclaims under pressure; after provisioning most of
it is apt and Python files read once.

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

## Host updates (Arch)

Arch removes the running kernel's modules when it upgrades the kernel, so modules the lab loads on demand (tc,
nftables, later K3s's) fail until you reboot. Update fully and reboot together, never `pacman -S` a single
package on a stale system (a partial upgrade breaks tools such as `shellcheck`):

```bash
sudo pacman -Syu && sudo reboot
./opsforge preflight   # warns if the kernel and its modules disagree, or a dev tool is broken
```

## Development

```bash
make check   # shellcheck, yamllint, Python syntax, ansible-lint, topology validation, pytest (no Go or Node needed)
```

See [docs/PLAN.md](docs/PLAN.md) for the architecture and roadmap.
