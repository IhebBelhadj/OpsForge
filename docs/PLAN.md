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

### Wiring (as built in Phase 1)
- Per site: a VPC bridge `of-<site>` (gateway `.1`, nodes `.11`, `.12`, … in `topology.yml` order) and a
  point-to-point uplink bridge `of-<site>-wan` between the gateway and `wan-core`. Gateways hold their public
  IP as a `/32` with `wan-core` (first host of `transit`) as an on-link next hop, so they can never reach each
  other directly: every inter-site packet crosses `wan-core`. A single shared transit bridge would have let
  gateways ARP each other and bypass the shaping.
- No lab bridge has a host address (`ipv4.address=none`): the host neither routes between VPCs nor into them.
  Instances use the `opsforge` profile (root disk only), never `incusbr0` from the default profile.
- `wan-core` is the "internet": its `eth0` sits on `incusbr0` for real internet access (masqueraded). It drops
  forwarding to private ranges (other VPCs, the host's LANs, `incusbr0`) like the real internet does, and
  answers DNS on its transit address by DNAT to the `incusbr0` resolver, so no DNS daemon is needed.
- Routers (`wan-core`, gateways) run one generated boot script (`/etc/local.d/opsforge.start`, log in
  `/var/log/opsforge.log`) from a cached image `opsforge-router-1` (Alpine + `iproute2-tc`, `nftables`,
  `iperf3`, built once by `opsforge up`). Nodes get a static systemd-networkd file.
- Images `opsforge-router-<n>` (Alpine) and `opsforge-node-<n>` (Debian) are built once by `up` and ship
  sshd + python3, so Ansible manages every instance from its first boot (no Incus connection plugin needed).
  Their SSH host keys are stripped; `up` pushes a per-instance key generated on the host (kept in `.opsforge/`
  across `down`), so `known_hosts` is written in advance and host-key checking stays strict.
- The tc/nft modules this needs beyond `REQUIRED_MODULES` (`cls_u32`, `nft_ct`, `nft_nat`, …) are autoloaded
  by the kernel on first use, including from inside unprivileged containers.

### Management access (as built in Phase 2)
- The host is the Ansible controller and enters the lab like an operator on the internet: SSH to a gateway's
  public IP through `incus exec wan-core -- nc` (the host has no route into the lab and needs no sudo), then
  `ProxyJump` through the gateway (the site's bastion) to the nodes. `opsforge up` renders the SSH config and
  inventory (`.opsforge/ssh/config`, `ansible/inventory/hosts.yml`); `opsforge ssh <host>` uses them.
- The management path is wan-core's own traffic, so it is **not shaped**: provisioning is fast and does not
  disturb experiments. Traffic between sites (DNS forwarding, later VPN/K3s) is shaped as usual.
- Infrastructure (opsforge) owns addressing and firewalls, like Terraform security groups; Ansible owns the
  services. Gateways accept SSH from anywhere on the WAN (key-only) and DNS from the other gateways only.
- DNS: each gateway runs dnsmasq for its zone `<site>.lab` and forwards the other zones to the other gateways'
  public IPs across the WAN, so a cross-site lookup pays the link's RTT (then is cached). Nodes resolve through
  their gateway with every site's zone as a search domain (own site first), so short names work too. Names
  resolve to private VPC addresses, which become reachable from other sites in Phase 3.

### Link-condition model (important detail)
- netem only shapes **egress**. On `wan-core`, each interface facing site X gets an HTB root with one
  class per peer site, classified by **source** gateway IP, each with a netem leaf. Unclassified traffic
  (real internet, `wan-core` itself) is not shaped.
- `topology.yml` specifies **RTT**; the generator applies `rtt/2` delay in each direction so `ping`
  matches the configured value. Loss is applied per direction (documented: round-trip loss ≈ 2×).
- `jitter` is the RTT standard deviation (what `ping` reports as `mdev`): each direction gets `jitter/√2`
  with a normal distribution.
- Bandwidth (`rate`) is applied per direction by netem's `rate`, which also keeps packets in order (plain
  netem jitter reorders packets and wrecks TCP). Links without a rate get 10gbit for the same reason.
- `opsforge link` changes are live only: the state lives in `wan-core:/etc/opsforge/links.json` and survives a
  `wan-core` restart, but `opsforge down && up` resets to `topology.yml`.

### Memory budget (Incus, idle; to be verified by measurement each phase)

| Node | Role | Limit | Expected idle |
|---|---|---|---|
| wan-core | Alpine, tc/nftables | 64 MiB | ~15–25 MB |
| a-gw / b-gw / c-gw | Alpine, NAT/FW/WireGuard, dnsmasq | 128 MiB each (Ansible's Python modules OOM at 64) | ~10–20 MB each |
| a-cp1 | K3s server + Prometheus + Grafana | 1.5 GiB | ~700 MB–1 GB |
| b-w1, c-w1 | K3s agent | 768 MiB each | ~250–400 MB each |
| ci (optional) | Gitea + Woodpecker + registry | 1 GiB | ~500–800 MB |
| **Total (no CI)** | | | **≈ 1.3–1.9 GB** |


## 4. Repository layout (target)

```
topology.yml              # single source of truth: sites, links, nodes, profiles
opsforge                  # CLI entry point (Python 3): up/down/status/verify/link/ssh/partition/heal/flap
lab/
  render.py               # topology.yml -> incus resources, tc/nft scripts (inline), later ansible inventory
  topology.schema.json    # topology.yml validation
  host/                   # files installed on the host by scripts/host-setup.sh
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
- [x] Define `topology.yml` and its schema:
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
- [x] `lab/render.py` creates: one isolated Incus network per VPC (no host NAT/DHCP leakage), one uplink
      network per site (see §3 Wiring), gateways, `wan-core`, and node containers from minimal images with
      CPU/memory limits. `opsforge render` prints everything it generates.
- [x] Gateways: SNAT VPC → public IP, default-deny inbound (ping allowed), port forwards declared per node
      with `expose: ["<public>[:<private>][/tcp|udp]"]`.
- [x] `wan-core`: HTB + netem per site pair as described in §3.
- [x] `opsforge link <a-b> --rtt/--jitter/--loss/--rate` re-applies conditions live, without recreating anything.
- [x] `opsforge up/down/status/verify`; `verify` runs the acceptance checks below against the live link state.
- [x] Measured (2026-10-19): idle lab 312 MiB (`wan-core` 9 MiB, gateways 6 MiB each, Debian nodes ~96 MiB
      each); router image 5.8 MiB, pool 504 MiB used; `up` takes ~10 s with cached images.

**Done when** (all verified 2026-10-19 by `./opsforge verify`, plus a manual `expose` test):
- `ping` b-w1 → a-gw public IP ≈ 40 ms RTT, c-w1 → a ≈ 120 ms, with configured jitter.
- `iperf3` between sites tops out at the configured rate (UDP: TCP over 1% loss at 120 ms cannot fill
  20 Mbit/s, per the Mathis formula, which is realistic but not a test of the shaper).
- Nodes in different VPCs **cannot** reach each other's private IPs (only via public IP / port forward).
- `opsforge down && opsforge up` reproduces the lab from scratch.

### Phase 2: Controller-driven provisioning
- [x] `render.py` emits the Ansible inventory: `lab` → `gateways`, `nodes`; `site_<site>`; one group per node
      role (`k3s_server`, `k3s_agent`); per-host `lab_site`, `lab_address` (VPC), `lab_public_ip` (gateways).
- [x] Ansible connects over SSH through wan-core and the gateways (ProxyJump), see §3 "Management access". The
      Incus connection plugin turned out unnecessary: the images ship sshd + python3 and `up` pushes the keys.
- [x] Roles: `base` (key-only sshd, minimal packages, own name → VPC address in `/etc/hosts`, journald capped at
      32 MiB, apt timers masked), `dns` (dnsmasq on each gateway; `*.a.lab`, `*.b.lab`, … resolvable across
      sites; LLMNR/mDNS off), `pki` (ECDSA CA on the controller in `.opsforge/pki/`, per-node key generated on
      the node, cert with name/FQDN/IP SANs, CA trusted system-wide).
      **Changed from the plan:** no chrony: containers share the host clock and cannot set it (§2). No swap is
      enforced by Incus (`limits.memory.swap=false`) rather than by a role.
- [x] Retire `bootstrap.sh` / `start.env.sh` from the v2 path (removed with v1).
- [x] CLI: `opsforge provision [ansible-playbook args]`, `opsforge ssh <host> [cmd]`; `verify` adds SSH, DNS
      (12 names per node) and PKI checks.
- [x] Measured (2026-10-19), provisioned and idle: Incus reports 1042 MiB, of which 921 MiB is reclaimable page
      cache left by apt/Ansible (nodes ~290 MiB, gateways 38 MiB); non-reclaimable (anon + kernel) is 101 MiB
      (nodes 26–30 MiB, gateways 5 MiB, wan-core 2 MiB). Images: node 126 MiB, router 21 MiB; pool 1.5 GiB used.
      From scratch: `up` ~50 s (incl. SSH ready), first `provision` ~1 min, second run ~20 s.

**Done when** (all verified 2026-10-19 after `down && up && provision`): a second `ansible-playbook site.yml`
run reports `changed=0`; every node resolves every other node by name; `ssh b-w1` works from the host via the CLI.

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
