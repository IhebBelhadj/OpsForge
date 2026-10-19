# OpsForge v2: Multi-site WAN Lab — Plan

## 1. Goal

Give a developer a single command that provisions a **distributed environment spread over simulated
distant sites**, so they can build and break systems under realistic WAN conditions (latency, jitter,
loss, limited bandwidth, NAT, partitions) as they would in a multi-cloud / multi-region deployment —
on one laptop, with the smallest possible RAM footprint.

### Non-goals
- Simulating the real internet's routing at scale (BGP tables, many ISPs). Optional extension only.
- Running heavy enterprise CI tooling by default (Jenkins, SonarQube, Nexus).
- GUIs inside nodes. Everything is CLI/headless (the current boxes already are; this is not where RAM goes).

## 2. Key decisions

| Decision | Choice | Why |
|---|---|---|
| Node substrate | **Incus system containers** (Linux host) | Each node is a full "machine" (own network stack, systemd, sshd) sharing the host kernel. Idle cluster ≈ 1.2–1.8 GB vs ≈ 3–4 GB for tuned VMs. Starts in seconds. |
| Fallback substrate | KVM via libvirt (free page reporting + KSM) | For non-Linux-container needs: clock-skew scenarios, different kernels. Only Phase 1 differs. |
| Node OS | Debian 13 minimal (Alpine for router/gateways) | Small, systemd available for K3s; no snapd/cloud-init/multipathd. |
| Kubernetes | **K3s**, embedded SQLite, bundled extras disabled | Lightest conformant distro; SQLite avoids etcd's latency sensitivity. |
| Control-plane placement | Single site; workers stretched across sites | etcd/control planes over a lossy WAN is an anti-pattern; multi-cluster comes later. |
| Provisioning | Ansible from the host (controller), inventory generated | Manages "remote" machines the way real ops does; idempotent. |
| Monitoring | Prometheus (short retention) or VictoriaMetrics + Grafana, blackbox probes | WAN health must be visible. |
| CI/CD | Optional profile: Gitea/Woodpecker + `registry:2` + Flux | Fraction of Jenkins/Sonar/Nexus RAM; GitOps pull across the WAN is realistic. |

### What containers can and cannot simulate
- ✅ Latency, jitter, loss, reordering, bandwidth caps, MTU issues, NAT, firewalls, WireGuard/IPsec,
  partitions, link flapping, dynamic routing (FRR).
- ❌ **Clock skew between nodes** (shared host clock) → use `libfaketime` per process or a single KVM node.
- ❌ Per-node kernel versions; a few kernel-wide sysctls are shared (most net sysctls are per-namespace,
  incl. TCP congestion control).
- ⚠️ netem precision degrades at multi-Gbit rates or under heavy host CPU load; fine at WAN-like rates.

## 3. Target architecture

Each site is modelled as a cloud VPC: a private LAN behind a **site gateway** (NAT, firewall, VPN
endpoint) with a "public" IP on a shared **WAN transit** network. A **WAN core router** connects the
gateways and is the single place where link conditions are applied.

```
   site-a VPC 10.10.0.0/24        site-b VPC 10.20.0.0/24        site-c VPC 10.30.0.0/24
   ┌────────────────────┐         ┌────────────────────┐         ┌────────────────────┐
   │ a-cp1  k3s server  │         │ b-w1   k3s agent   │         │ c-w1   k3s agent   │
   │        + monitoring│         │                    │         │                    │
   └─────────┬──────────┘         └─────────┬──────────┘         └─────────┬──────────┘
         a-gw (NAT/FW/WG)             b-gw (NAT/FW/WG)             c-gw (NAT/FW/WG)
         203.0.113.10                 203.0.113.20                 203.0.113.30
             └───────────────┬───────────────┴──────────────┬───────────────┘
                        wan-core router (netem + HTB per site pair)
                        "internet" transit 203.0.113.0/24
```

### Link-condition model (important detail)
- netem only shapes **egress**. On `wan-core`, each interface facing site X gets an HTB root with one
  class per peer site, classified by **source** gateway IP, each with a netem leaf.
- `topology.yml` specifies **RTT**; the generator applies `rtt/2` delay in each direction so `ping`
  matches the configured value. Loss is applied per direction (documented: round-trip loss ≈ 2×).
- Bandwidth (`rate`) is applied per direction.

### Memory budget (Incus, idle; to be verified by measurement each phase)

| Node | Role | Limit | Expected idle |
|---|---|---|---|
| wan-core | Alpine, tc/nftables | 64 MiB | ~15–25 MB |
| a-gw / b-gw / c-gw | Alpine, NAT/FW/WireGuard, dnsmasq | 64 MiB each | ~10–20 MB each |
| a-cp1 | K3s server + Prometheus + Grafana | 1.5 GiB | ~700 MB–1 GB |
| b-w1, c-w1 | K3s agent | 768 MiB each | ~250–400 MB each |
| ci (optional) | Gitea + Woodpecker + registry | 1 GiB | ~500–800 MB |
| **Total (no CI)** | | | **≈ 1.3–1.9 GB** |


## 4. Repository layout (target)

```
topology.yml              # single source of truth: sites, links, nodes, profiles
opsforge                  # CLI entry point (Python 3): up/down/status/ssh/link/partition/heal/flap
lab/
  render.py               # topology.yml -> incus resources, tc/nft scripts, ansible inventory
  schema.json             # topology.yml validation
  templates/              # tc, nftables, wireguard, dnsmasq templates
ansible/
  inventory/              # generated (gitignored)
  site.yml                # top-level playbook
  roles/ base dns pki gateway wan_core wireguard k3s_server k3s_agent monitoring ci
scenarios/                # fault-injection scenarios with expected outcomes
docs/PLAN.md
```

## 5. Phases

Each phase ends with: acceptance checks passing, a measured RAM snapshot (`incus list -c nsm`,
`free -h`) recorded in the README, and the `CLAUDE.md` files updated.

### Phase 0: Groundwork
- [x] `scripts/host-setup.sh` (root, idempotent): installs Incus + dev tools, maps subuid/subgid, initialises
      Incus via `lab/host/incus-preseed.yaml`: btrfs pool on a sparse loop file (copy-on-write clones; the
      root fs is ext4) and `incusbr0` on `10.99.0.0/24`.
- [x] Persist host kernel modules (`br_netfilter`, `overlay`, `nf_conntrack`, `wireguard`, `sch_netem`,
      `sch_htb`) and sysctls (inotify limits, keyring limits, ip_forward).
- [x] Docker coexistence: `opsforge-docker-user.service` inserts `DOCKER-USER` accepts for `incusbr+`/`of-+`
      and re-applies them on every Docker restart.
- [x] Remove the v1 Vagrant/VirtualBox setup.
- [x] Skeleton `opsforge` CLI (`validate`, `preflight`, `smoke`; other commands stubbed with their phase),
      `topology.yml` + JSON Schema + address-plan checks, `make check`.
- [x] Run `sudo ./scripts/host-setup.sh`, re-login, then `./opsforge preflight` and `./opsforge smoke` green.
- [x] Record the smoke container's idle RAM in the README (Debian 13: 113 MiB idle; image 101 MiB; pool 441 MiB used).
- [x] `make check` green. The Makefile falls back to the `koalaman/shellcheck` image when the local binary is
      broken (it was, after an Arch partial upgrade).

**Done when:** `./opsforge preflight` passes, `./opsforge smoke` launches `images:debian/13`, gets an address
and reaches the internet through Docker's firewall, and `make check` runs green.

### Phase 1: Topology and simulated WAN
- [ ] Define `topology.yml` and its schema:
  ```yaml
  sites:
    a: { vpc: 10.10.0.0/24, public_ip: 203.0.113.10 }
    b: { vpc: 10.20.0.0/24, public_ip: 203.0.113.20 }
    c: { vpc: 10.30.0.0/24, public_ip: 203.0.113.30 }
  links:                     # rtt is round-trip; rate/loss are per direction
    a-b: { rtt: 40ms,  jitter: 5ms,  loss: 0.1%, rate: 100mbit }
    a-c: { rtt: 120ms, jitter: 15ms, loss: 1%,   rate: 20mbit }
    b-c: { rtt: 90ms,  jitter: 10ms, loss: 0.5%, rate: 50mbit }
  nodes:
    a-cp1: { site: a, role: k3s-server, cpu: 2, mem: 1536MiB }
    b-w1:  { site: b, role: k3s-agent,  cpu: 1, mem: 768MiB }
    c-w1:  { site: c, role: k3s-agent,  cpu: 1, mem: 768MiB }
  profiles:
    ci: false
  ```
- [ ] `render.py` creates: one isolated Incus network per VPC (no host NAT/DHCP leakage), the transit
      network, gateways, `wan-core`, and node containers from minimal images with CPU/memory limits.
- [ ] Gateways: SNAT VPC → public IP, default-deny inbound, explicit port forwards when declared.
- [ ] `wan-core`: HTB + netem per site pair as described in §3.
- [ ] `opsforge link <a-b> --rtt/--loss/--rate` re-applies conditions live, without recreating anything.

**Done when:**
- `ping` b-w1 → a-gw public IP ≈ 40 ms RTT, c-w1 → a ≈ 120 ms, with configured jitter.
- `iperf3` between sites tops out at the configured rate.
- Nodes in different VPCs **cannot** reach each other's private IPs (only via public IP / port forward).
- `opsforge down && opsforge up` reproduces the lab from scratch.

### Phase 2: Controller-driven provisioning
- [ ] `render.py` emits the Ansible inventory (groups by site and role).
- [ ] Ansible connects over SSH through the gateways (ProxyJump) — the same experience as managing remote
      hosts; fall back to the Incus connection plugin for bootstrap.
- [ ] Roles: `base` (chrony, sshd, minimal packages, no swap, journald size cap), `dns` (dnsmasq on each
      gateway; `*.a.lab`, `*.b.lab`, … resolvable across sites), `pki` (small CA, per-node certs).
- [ ] Retire `bootstrap.sh` / `start.env.sh` from the v2 path.

**Done when:** a second `ansible-playbook site.yml` run reports `changed=0`; every node resolves every
other node by name; `ssh b-w1` works from the host via the CLI.

### Phase 3: Site-to-site VPN
- [ ] WireGuard tunnels between gateways (full mesh for 3 sites) giving routed private reachability
      between VPCs — the multi-cloud "VPN between VPCs" pattern.
- [ ] MTU set deliberately on tunnels (e.g. 1420) and clamped (TCP MSS) so MTU problems are visible and
      reproducible on demand.
- [ ] `topology.yml` toggle: `interconnect: vpn | none`.

**Done when:** b-w1 reaches a-cp1's private IP; `tcpdump` on `wan-core` shows only UDP/51820 between
sites; link conditions still apply to tunnel traffic.

### Phase 4: Stretched K3s (single cluster)
- [ ] `k3s_server` on a-cp1: SQLite datastore, `--disable traefik,servicelb,metrics-server`,
      `--flannel-backend=vxlan` over the site VPN with flannel MTU lowered for the double encapsulation
      (VPN + VXLAN). Experiment flag: `wireguard-native` with interconnect `none` and public node IPs.
- [ ] `k3s_agent` on workers; node labels `topology.kubernetes.io/region=<site>` / `zone`.
- [ ] Kubelet tuning for small nodes: `system-reserved`/`kube-reserved`, eviction thresholds, image
      garbage collection thresholds; `--node-status-update-frequency` left default but documented.
- [ ] Incus container config for K3s, captured in an Incus profile `k3s-node`. **Try unprivileged first**
      (`security.nesting`, `/dev/kmsg`, required kernel modules); fall back to `security.privileged` only for
      what concretely breaks, and document why. The user cares about host isolation: a privileged container's
      root is effectively host root. Alternatives if that matters more than RAM: K3s nodes as Incus VMs
      (+250–400 MB each).
- [ ] Sample workload spread across regions (topology spread constraints).

**Done when:**
- All nodes `Ready`; sample Deployment runs pods in all three regions.
- `opsforge partition c`: c-w1 → `NotReady`, pods rescheduled after the eviction timeout;
  `opsforge heal` → node recovers cleanly without manual intervention.

### Phase 5: WAN-aware observability
- [ ] Prometheus (`--storage.tsdb.retention.time=48h`, 30 s scrape) or VictoriaMetrics single-node;
      Grafana with provisioned datasources and dashboards from the repo.
- [ ] node_exporter on all nodes and gateways; blackbox exporter probing every site pair
      (ICMP RTT, TCP connect, HTTP).
- [ ] Dashboards: site-to-site latency/loss matrix, node health, K3s overview.

**Done when:** changing a link's RTT with `opsforge link` appears on the matrix within one scrape interval.

### Phase 6: Fault injection and scenarios
- [ ] CLI: `partition <site>`, `heal`, `flap <link> --period`, `degrade <link> --loss --rate`,
      `mtu <link> <n>`.
- [ ] Optional Toxiproxy for application-protocol faults.
- [ ] `scenarios/`: scripted experiments with expected outcomes, e.g.
  - Partition site b during sustained writes → verify no data loss / measure recovery time.
  - Raise a-c RTT to 300 ms → measure API latency impact on cross-site calls.
  - Path-MTU black hole on a-b → reproduce the classic "small requests work, large ones hang" bug.
- [ ] Clock-skew scenarios via `libfaketime` (documented limitation of containers).

**Done when:** each scenario runs from one command and prints pass/fail against its expectation.

### Phase 7: Lightweight CI/CD (optional profile `ci: true`)
- [ ] Gitea (+ Actions) or Woodpecker, and `registry:2`, on a `ci` node in site a.
- [ ] Flux on the cluster pulls manifests from Gitea (GitOps across the WAN); replaces the webhook stub.
- [ ] A small reference workload (web app + database) deployed through the pipeline.

**Done when:** a push to Gitea builds an image, pushes to the registry, and Flux rolls it out across sites.

### Phase 8: Multi-cluster (realistic multi-cloud)
- [ ] `clusters:` section in `topology.yml`; one K3s cluster per site (or per "cloud").
- [ ] Interconnect with Submariner or Skupper (lighter) or Cilium Cluster Mesh (heavier: replaces flannel).
- [ ] Scenarios: cross-cluster service failover, geo-aware DNS, loss of an entire "cloud".

**Done when:** a service survives the total loss of the cluster it was primarily served from.

### Later (deferred by the user): Windows nodes
- Windows can only be an **Incus VM** (needs its own kernel), attached to a site network like any node, so it gets
  the same WAN conditions. Prefer **Windows Server Core** (no GUI, ~1.5–2 GB RAM, ~10–15 GB disk); Windows 11
  (4 GB RAM, 64 GB disk) does not fit this host alongside the lab.
- Needs VM packages on the host: `qemu-base`, `edk2-ovmf`, `swtpm`, `cdrtools`, `distrobuilder` (repacks the ISO
  with VirtIO drivers); a pool cap of ~40 GiB; `type: container | vm` and `os` fields per node in `topology.yml`.
- Managed via built-in OpenSSH + `ansible.windows`. K3s has no Windows agents (that would need RKE2), so a Windows
  node is a standalone machine, not a cluster member.

## 6. Risks

| Risk | Mitigation |
|---|---|
| K3s inside LXC may need privileged containers | Try unprivileged first; dedicated Incus profile; VM fallback. |
| `br_netfilter` (loaded by host-setup) is system-wide: bridged traffic on all bridges, incl. Docker's, hits iptables | Accepted trade-off (Docker supports it); told to the user. |
| Docker on host interferes with bridge forwarding | Phase 0 check + `DOCKER-USER` rules. |
| Host kernel updates (Arch) break module availability until reboot | `opsforge up` preflight checks required modules. |
| netem imprecision under host load | Keep WAN rates realistic; preflight warns if host load is high; measure in Phase 1 acceptance. |
| Double encapsulation MTU issues | Explicit MTU math in templates; Phase 3/4 acceptance tests large transfers. |
| Linux-only host requirement | Document; KVM/libvirt or a Linux VM as the path for other OSes. |

## 7. Open questions
- Must the lab run on macOS/Windows hosts for other developers? (Affects substrate priority.)
- How many sites by default — 3, or 2 to start with?
- Is CI part of the default experience or opt-in only?
