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
| Control-plane placement | Single site; workers stretched across sites (`topology.yml`); or one cluster per site (`topologies/multi-cluster.yml`, Phase 8) | etcd/control planes over a lossy WAN is an anti-pattern. |
| Multi-cluster interconnect | Skupper 2 (application-level links, cost = RTT), over the VPN | No CNI change or CIDR planning; cost-based failover between clusters. Submariner/Cilium Cluster Mesh heavier. |
| Geo-aware DNS | Each gateway's dnsmasq + a sh health-check loop (`opsforge-geo`), order by link RTT (Phase 9) | No extra daemon or RAM (PowerDNS/CoreDNS GSLB plugins, k8gb heavier); every site is its own resolver, so no client-subnet logic. |
| Provisioning | Ansible from the host (controller), inventory generated | Manages "remote" machines the way real ops does; idempotent. |
| Monitoring | Prometheus (short retention) or VictoriaMetrics + Grafana, blackbox probes | WAN health must be visible. |
| CI/CD | Optional `ci` node: Gitea + Actions (runner building with buildah) + `registry:2` + Flux | Fraction of Jenkins/Sonar/Nexus RAM; GitOps pull across the WAN is realistic. |

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
  `/var/log/opsforge.log`) from a cached image `opsforge-router-<n>` (Alpine + `iproute2`, `nftables`,
  `iperf3`, WireGuard tools, BIRD, built once by `opsforge up`). Nodes get a static systemd-networkd file.
- With `interconnect: vpn` (Phase 3) the gateways also form a WireGuard full mesh (`wg-<peer>`) running OSPF;
  the tunnels' UDP crosses `wan-core` between public IPs, so it is shaped like any inter-site traffic.
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
  resolve to private VPC addresses, which are reachable from other sites with `interconnect: vpn` (Phase 3).

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
  `wan-core` restart and an incremental `up` (even one that rebuilds `wan-core`; new links start from
  `topology.yml`), but `opsforge down && up` resets to `topology.yml`.

### Memory budget (Incus, idle; to be verified by measurement each phase)

| Node | Role | Limit | Expected idle |
|---|---|---|---|
| wan-core | Alpine, tc/nftables | 64 MiB | ~15–25 MB |
| a-gw / b-gw / c-gw | Alpine, NAT/FW/WireGuard, dnsmasq, node + blackbox exporters | 128 MiB each (Ansible's Python modules OOM at 64) | ~10–20 MB each (measured 28 MiB) |
| a-cp1 | K3s server + Traefik | 1.5 GiB | ~500–700 MB (measured 757 MiB) |
| mon | VictoriaMetrics + Grafana, own node in site b (survives partitioning site a) | 768 MiB (512 thrashed during a provision, Phase 7) | ~200–300 MB (measured 326 MiB; 406 with Phase 7's series) |
| b-w1, c-w1 | K3s agent + Traefik | 768 MiB each | ~250–400 MB each (measured ~172 MiB) |
| ci (optional) | Gitea + Actions runner + registry; builds with buildah | 1 GiB | ~500–800 MB (measured 234 MiB idle) |
| opsforge-cache | zot pull-through registry (outside the lab, Phase 4) | 256 MiB | ~30–60 MB (measured 55 MiB) |
| **Total (no CI)** | | | **≈ 1.5–2.1 GB (measured 1.6 GiB, Phase 5)** |
| **Total (with CI)** | | | **measured 2.2 GiB (Phase 7: ci 234, Flux + webapp ~50 MiB per agent)** |
| **Total, multi-cluster** (`topologies/multi-cluster.yml`: 3 servers at 2 GiB, Skupper, CI) | | | **measured 4.3 GiB (Phase 8: servers ~1.2 GiB each)** |


## 4. Repository layout (target)

```
topology.yml              # single source of truth: sites, links, nodes, profiles
opsforge                  # CLI entry point (Python 3): up/down/status/verify/link/ssh/partition/heal/flap/scenario
lab/
  render.py               # topology.yml -> incus resources, tc/nft scripts (inline), later ansible inventory
  topology.schema.json    # topology.yml validation
  host/                   # files installed on the host by scripts/host-setup.sh
ansible/
  inventory/              # generated (gitignored)
  site.yml                # top-level playbook
  roles/ base dns pki k3s exporters monitoring ci  # routers (wan-core, gateways, WireGuard) are rendered by opsforge
scenarios/                # fault-injection scenarios with expected outcomes
labs/                     # teaching-lab packs: curricula, labs, badges (Phase 10)
web/                      # `opsforge serve` interface (static HTML/CSS/JS, no build step)
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

### Phase 2.5: Hardening (before the VPN)
Closes gaps found in a plan review (2026-10-20). Cheap now, expensive once WireGuard and K3s sit on top.
- [x] Unit tests for `render.py` and `topology_errors` (`tests/`, `make test`, part of `make check`): render is pure,
      so addressing, netem maths and the plan are covered without Incus.
- [x] Repo CI (`.github/workflows/check.yml`): lint, topology validation and tests; needs no Incus.
- [x] **K3s feasibility spike** (`scripts/k3s-spike.sh`, 2026-10-20): **K3s v1.36.5 runs in an unprivileged
      Debian 13 container** (`security.nesting=true`, `/dev/kmsg` device, host-loaded modules `overlay`,
      `br_netfilter`, `nf_conntrack`, `ip_tables`, `iptable_nat`, `xt_conntrack`, and kubelet feature gate
      `KubeletInUserNamespace=true`). Node Ready in ~40 s, coredns + local-path Running after image pulls.
      Single-node server idles at 414 MiB anon + 22 MiB kernel (plus ~790 MiB reclaimable page cache), so the
      1.5 GiB limit holds with room. Not yet tested: flannel/vxlan across the VPN, agents, privileged-only features.
- [x] **Desired vs. live link state:** `opsforge link <l> ... --save` also writes the link's line in
      `topology.yml` (comments and layout kept); `link <l> --reset` / `link --reset` revert one / every link to
      `topology.yml`; `status` flags links whose live state differs from the file. Phase 6's
      `partition`/`heal`/`flap` must restore the *pre-fault* live state, not the file.
- [x] **Memory budget enforcement:** the budget is the *non-reclaimable* figure (anon + kernel from each
      instance's `memory.stat`), because page cache is reclaimable and Incus counts it. `verify --mem [MIB]` fails
      above the ceiling (default `MEM_CEILING_MIB` = 150; measured 2026-10-20: 106 MiB provisioned and idle).
- [x] **Found and fixed while testing:** after a host reboot every gateway's dnsmasq crashed (OpenRC starts it
      before `opsforge.start` adds the addresses it listens on); now `bind-dynamic`. `opsforge provision --check`
      (options first) failed in argparse; leading options now pass through to ansible-playbook.
- [x] **Incremental `up`** (2026-10-22): `up` reconciles a running lab with `topology.yml` instead of requiring
      `down && up`. Each instance records `render.fingerprint()` (address, NICs, pushed files; not limits) in
      `user.opsforge.spec`. Instances no longer in the topology are deleted, then their `of-*` networks; an image
      change recreates; a fingerprint change stops the instance, syncs its NIC devices, re-pushes its files and
      starts it (a fresh network namespace, so boot scripts and networkd leave no stale address or route); limit
      and tag changes are set live; stopped instances are started. Live link conditions are kept (new links come
      from the file). Adding a site touches wan-core and every gateway (their firewalls list the peers' public
      IPs), not the nodes. Measured: adding site `d` + a node took 20 s, removing it 23 s; `provision` then
      `verify` green, live `a-b` edit kept, second `provision` `changed=0`.
      **Found while testing:** `incus network create` reads YAML from stdin when stdin is not a terminal, so `up`
      hung forever when run from a script; every `incus` call now gets `/dev/null` as stdin.
      Specs are computed from `topology.yml` before wan-core's files get the live link conditions, so an `up` after
      `opsforge link` edits is a no-op instead of restarting wan-core.
- [x] **Snapshot/restore** (2026-10-22): `opsforge snapshot [NAME]` snapshots every lab instance under one name
      (default `YYYYmmdd-HHMMSS`), live, so each disk is crash-consistent; `--list`, `--delete NAME`.
      `opsforge restore NAME` stops forwards and every instance, restores each (disk, config, NICs, so also
      wan-core's live link conditions), boots them together and waits for routers and SSH like `up`. It refuses
      when an instance lacks the snapshot (created after it) rather than mix states, and notes instances whose
      spec differs from `topology.yml` (`up` applies it). Snapshots are per instance, so `down` deletes them.
      Measured on the provisioned lab: snapshot 0.45 s and no measurable pool growth (btrfs CoW); restore 13 s,
      undoing a file write, a stopped dnsmasq and a `link` edit, then `verify --mem --disk` green (105 MiB,
      1.54 GiB).
- [x] `opsforge gc [--dry-run]` deletes images left by older builds: every alias an `opsforge-*` one no longer in
      `render.IMAGES`, and no instance created from it. Unaliased images are Incus' remote-image cache (it expires
      them itself) and are left alone. First run freed 147 MiB (`opsforge-router-2`, `opsforge-node-2`).
- [x] **Packet evidence:** `opsforge capture <host|wan-core> [filter]` runs tcpdump in the instance and saves a
      pcap under `.opsforge/captures/`, so "done when" claims about tcpdump are reproducible. `-d SECONDS`
      (default 10, 0 = until Ctrl-C), `-c COUNT`, `-i IFACE` (default `any`), `--show [brief|full]` decodes it with
      the instance's own tcpdump (the host needs none). tcpdump is now in both images (`opsforge-router-3`,
      `opsforge-node-3`), so wan-core, which Ansible never manages, and unprovisioned nodes can capture too.
- [x] **Host drift:** `preflight` warns when the running kernel has no `/usr/lib/modules/<release>` (Arch removed
      it on upgrade: on-demand module loads fail until a reboot) and when a dev tool is installed but broken
      (`--version` fails, e.g. `shellcheck` after a partial upgrade). The `pacman -Syu` + reboot step is in the README.
- [x] **Host access to lab services:** `opsforge forward <host>:<port> [--local <port>]` opens an SSH `-L` tunnel
      over the existing management path (wan-core → gateway → node), so the host reaches a lab service without a
      route into the lab or sudo. Binds `127.0.0.1` only (ports below 1024 move up by 10000: 80 -> 10080);
      `--to <addr>` targets another address as seen from that host. Each tunnel is a background `ssh -L` with its
      own control socket in `.opsforge/forwards/`; `forward --list` / `--stop [<host>:<port>]` manage them and
      `down` stops them all. Phase 4's kubeconfig and Phase 5's Grafana use it.
- [x] **Disk budget:** the pool is capped at 25 GiB and images, snapshots, K3s images and TSDB data all land on
      it. `status` shows pool used/total; `verify --disk [GIB]` fails above a ceiling (like `--mem`);
      `preflight` warns above 80 % of the pool. Each phase's RAM snapshot also records pool usage. Pool total
      only: btrfs without quotas reports no per-instance usage. Default ceiling `DISK_CEILING_GIB` = 3; measured
      2026-10-22: 1.55 GiB (provisioned lab + images).

**Done when:** `make check` is green in CI, the K3s spike result is written down, `link --reset` followed by
`verify` is green after arbitrary `link` edits, the lab survives a host reboot with `verify` green,
`opsforge forward` reaches a port on a node from `127.0.0.1`, and `verify --disk` reports pool usage.

### Phase 3: Site-to-site VPN (done 2026-10-22)
- [x] WireGuard tunnels between gateways (full mesh) giving routed private reachability between VPCs, the
      multi-cloud "VPN between VPCs" pattern. Generated by `render` and pushed before boot like the rest of the
      router config (it is addressing, so opsforge owns it), so the VPN works straight after `up`, before
      `provision`. One interface per peer, `wg-<peer>`, holding the gateway's VPC address with the peer's as
      point-to-point peer. Each needs its own UDP port: a gateway listens on `51820 + the peer's index` in
      `topology.yml`'s sites; the firewall accepts each port from that one peer's public IP. Keys are X25519
      keys made by the host's `openssl` (`.opsforge/wg/<site>.pem`, kept across `down`; no host package needed).
      Router image `opsforge-router-4` adds `iproute2` (BusyBox `ip` cannot create WireGuard links),
      `wireguard-tools-wg` and `bird`; WireGuard and BIRD work in unprivileged containers as-is.
      **Found while testing:** with both ends configured with an `Endpoint`, gateways booting together initiate
      simultaneously; each one's answer to the other discards its own pending handshake, and the pair retries in
      lockstep every 5 s until WireGuard's jitter separates them: 4–37 s per tunnel, random. Now the site listed
      first initiates and the other side has no `Endpoint` (it learns it from the first authenticated packet),
      like a cloud VPN's initiator/responder: every tunnel is up 4–9 s after a restart. Cost: when only the
      responder restarts, it waits for the initiator's 15 s "sent data, got nothing" rehandshake (measured 18 s).
- [x] MTU 1420 on the tunnels and TCP MSS clamped to the route MTU (`chain mss` on the gateways, priority
      mangle), so TCP across the VPN never depends on path-MTU discovery. Measured with a 20 MB transfer b-w1 →
      a-cp1: clamp on 20 MB in 11 s; clamp off 20 MB in 14 s (PMTU discovery works: the gateway answers
      "fragmentation needed"); clamp off **and** that ICMP dropped (`nft add chain inet opsforge pmtu_blackhole
      '{ type filter hook output priority filter; }'` + `icmp type destination-unreachable icmp code
      frag-needed drop` on both gateways) 0 bytes, stalled, the classic PMTU black hole. Flush the nodes'
      route cache (`ip route flush cache`) between runs: a learned 1420 path MTU is cached for 10 minutes and
      hides the black hole. Restore with `nft -f /etc/opsforge/firewall.nft`.
- [x] `topology.yml` toggle `interconnect: vpn | none` (default `none`). Switching touches only the gateways
      (`up`, ~20 s). With `none` the gateways have no `wg-*` interfaces and the Phase 1 isolation check passes.
- [x] **Dynamic routing around failed links** (`routing: ospf | static`, default `ospf`): BIRD 3 (Alpine
      `bird`, run by OpenRC's `supervise-daemon`) runs OSPF over the `wg-*` interfaces (`type ptp`, hello 2 s,
      dead 10 s), each VPC announced as a stub; `AllowedIPs = 0.0.0.0/0`, so routing, not cryptokey routing,
      picks the tunnel. Cost = the link's RTT in ms from `topology.yml` (live `opsforge link` edits do not change
      it), 1 between sites without a link. Rerouted traffic is re-encapsulated by the transit gateway, so it is
      shaped hop by hop with no change to the netem model. `static` puts one route per peer VPC on its tunnel.
      Measured with a-c at 100 % loss: a-gw's route to c moves to `wg-b` after 8.5 s (dead interval 10 s);
      a-cp1 → c-w1 at 134–138 ms (a-b 40 + b-c 90). After `link --reset a-c` the adjacency is Full in 3 s and
      the route is back on `wg-c` after 14–33 s (OSPF LSA/SPF pacing, not the tunnel: its session survives the
      outage), at 116–122 ms.
- [x] `verify` checks, with `interconnect: vpn`: latest handshake < 180 s per tunnel, OSPF adjacencies Full,
      and each link's RTT pinged between private node addresses (through the tunnel); with `none`, the isolation
      check. The bandwidth check now retries iperf3 once when the tool itself fails (its TCP control connection
      shares the flooded, lossy link and occasionally times out); a measured rate out of range never retries.

**Done when:** b-w1 reaches a-cp1's private IP; `opsforge capture wan-core` shows only UDP 5182x between
sites; link conditions still apply to tunnel traffic (RTT through the tunnel ≈ the link's RTT); a large
transfer (≥ 10 MB over TCP) succeeds with MSS clamping and stalls without it when ICMP "fragmentation needed" is
dropped (with ICMP working, PMTU discovery rescues it); after `link` takes a-b down and back up, the tunnel
recovers without intervention. With `routing: ospf` and a-c down (100 % loss), a-cp1 → c-w1 still answers at
≈ a-b + b-c RTT (130 ms) within the OSPF dead interval (set to 10 s), and returns to ≈ 120 ms after the link is
restored. **Met 2026-10-22:** all of the above as measured in the items; during cross-VPC pings
`capture wan-core net 10.0.0.0/8` saw 0 packets; a-b at 100 % loss for 150 s then reset: tunnel back in 2 s.
`verify --mem --disk` green: 111 MiB anon + kernel (gateways 6 MiB each, +2 MiB for WireGuard + BIRD),
pool 1.88 GiB (router image 22.6 MiB).

### Phase 4: Stretched K3s (single cluster) (done 2026-10-22)
- [x] **CA use:** K3s generates and keeps its own cluster CAs (`/var/lib/rancher/k3s/server/tls` on the server);
      the lab CA (`.opsforge/pki/`) signs node certificates only and K3s never sees it. Rotating the lab CA: delete
      `.opsforge/pki/` and run `provision` (every node certificate is re-signed). Losing it costs nothing but that
      re-sign. The cluster CAs live in the server container: `down` (or deleting a-cp1) loses them with the
      cluster, and the next `provision` creates a new cluster and a new `.opsforge/kubeconfig`.
- [x] One Ansible role `k3s` (server and agent differ by `lab_role`, as `dns` does by group), `ansible/roles/k3s`:
      K3s **v1.37.1+k3s1**, binary downloaded once to the controller (`.opsforge/k3s/`, sha256-checked) and copied
      over the management path, like an air-gapped install; join token generated once in `.opsforge/k3s/token`.
      Server on a-cp1: SQLite, `disable: [servicelb, metrics-server]`, flannel **vxlan** over the site VPN; agents
      join `https://<server's private IP>:6443` across the tunnels. Flannel's underlay MTU is set to the tunnels'
      (`flannel-conf` `Backend.MTU` 1420; flannel subtracts its 50 bytes itself, so pods get **1370**). Measured:
      20 MB pod (b-w1) → pod (a-cp1) over TCP in 17.9 s with 0 fragments created on b-gw. Flannel does not resize
      an existing `flannel.1`: an MTU change needs the interface deleted (or a rebuilt node).
      **Not done:** the `wireguard-native` experiment flag (interconnect `none`, public node IPs): nodes sit behind
      their gateway's NAT and share its public IP, so it needs one public port set per node; deferred to Phase 8,
      where per-site clusters make it natural.
- [x] Node labels `topology.kubernetes.io/region=<site>`, `zone=<site>-1` (set at registration).
- [x] Kubelet tuning (`ansible/roles/k3s/defaults`): `kube-reserved` = what K3s itself uses (server 512Mi, agent
      256Mi), `system-reserved` 64Mi, `eviction-hard` memory.available<64Mi / nodefs and imagefs < 10 %, image GC at
      80/70 %; agents' allocatable is 384Mi of 768Mi. `node-status-update-frequency` left at 10 s (documented in
      the config): a cut-off node turns NotReady after ~50 s. `default-(not-ready|unreachable)-toleration-seconds`
      lowered from 300 to 60 so partition experiments reschedule in about 2 minutes. kube-proxy gets
      `conntrack-max-per-core=0` (it cannot set the host-wide `nf_conntrack_max` from a container).
- [x] Incus profile **`k3s-node`**, created by `up` and added to every `k3s-*` node: **unprivileged**
      (`security.privileged=false`), `security.nesting=true`, `/dev/kmsg` (unix-char), and `linux.kernel_modules`
      (overlay, br_netfilter, nf_conntrack, the iptables/xtables modules kube-proxy and flannel use, vxlan) loaded
      by Incus on the host. Kubelet runs with `KubeletInUserNamespace=true`. Nothing needed privileges: agents,
      flannel vxlan across the VPN, hostPorts and the ingress all work unprivileged. Profiles are part of the
      instance fingerprint, so `up` assigns them like NICs.
- [x] Sample workload (`ansible/roles/k3s/templates/demo.yaml.j2`, namespace `demo`): `traefik/whoami`, one replica
      per region, topology spread on `region` (maxSkew 1, `DoNotSchedule`, `nodeTaintsPolicy: Honor`), each response
      naming its node. **Found while testing:** spreading happens at scheduling only. Installed with the server,
      all three pods landed on a-cp1 before the agents joined; `minDomains: 3` fixed that but kept the evicted pod
      of a partitioned region Pending (the tainted region is not counted, so the minimum is 0 and a second pod in a
      or b breaks maxSkew). Now a last play deploys it once every node is Ready, without `minDomains`. Kubernetes
      never rebalances by itself (a rollout restart does not either: old pods count while new ones are placed);
      after a heal: `opsforge kubectl -n demo delete pods -l app=whoami` (all at once).
- [x] **kubectl from the host:** `provision` saves the server's kubeconfig as `.opsforge/kubeconfig` (server
      `https://127.0.0.1:6443`, a SAN K3s has by default; `tls-san` also lists the node's name and private IP);
      `opsforge kubectl ...` starts `opsforge forward a-cp1:6443` if it is not running and runs the host's
      `kubectl` with it.
- [x] **Per-site ingress:** servicelb disabled; K3s' bundled Traefik reconfigured by a `HelmChartConfig` as a
      DaemonSet on every node (the server too, so site a has one) with hostPorts 80/443 and a ClusterIP Service;
      each node `expose`s 80/443 on its gateway (`topology.yml`). The demo Service has `trafficDistribution:
      PreferSameZone` and Traefik's `service.nativelb` annotation, so Traefik goes through the ClusterIP and
      kube-proxy keeps the request in the zone (site) it entered. The client IP is preserved (`X-Real-Ip` is
      wan-core's). Measured: each public IP, fetched from wan-core (the internet) or from the host with `forward
      <other site's gateway>:80 --to <public IP>`, is answered by the pod in that site. Traefik ~40 MiB per node.
- [x] **Image pull-through cache** `opsforge-cache` (outside the lab: no role tag, no profile; `down` keeps it,
      `down --purge` deletes it): Alpine + zot v2.1.22 (image `opsforge-cache-1`, 67 MiB; zot's release binary links
      glibc, `gcompat` provides it), static `10.99.0.2` on `incusbr0`, 256 MiB limit, ~55 MiB idle. One zot syncs
      `docker.io`, `ghcr.io`, `registry.k8s.io` and `quay.io` on demand, each stored under its own prefix; K3s'
      `registries.yaml` mirrors each upstream to `http://203.0.113.2:5000` with a `rewrite` adding the prefix.
      wan-core DNATs `203.0.113.2:5000` to it (reachable from every site, unshaped; `.2` is reserved like
      wan-core's `.1`). Created only when the topology has K3s nodes. **Found while testing:** zot refuses Docker
      schema-2 manifests (most of docker.io) without `http.compat: ["docker2s2"]`, answering 503, and containerd
      then silently pulls from the upstream instead: only OCI images (Traefik) were cached. zot still asks the
      upstream whether a tag moved (a manifest HEAD) on each pull by tag; layers come from the cache. apt-cacher-ng
      not added (provision from scratch already takes ~2 minutes).
- [x] `opsforge partition <site>` / `opsforge heal`, needed by this phase's acceptance (moved from Phase 6):
      `partition` saves the live link state to `wan-core:/etc/opsforge/pre-fault.json` (first fault only) and
      sets 100 % loss on every link of the site; `heal` puts the saved state back. The site keeps its internet
      access and the management path (wan-core's own traffic is not shaped), so `ssh` and `verify` still work.
- [x] `verify` checks, once K3s is installed: every K3s node Ready with its region label, a running demo pod in
      every region, and each site's public IP (port 80, fetched from wan-core) served by a pod in that site.
      `verify --mem` counts the cache too.

**Done when:**
- All nodes `Ready`; sample Deployment runs pods in all three regions.
- `opsforge kubectl get nodes` works from the host; `curl` to each site's public IP (via `forward`) is served by a
  pod in that site.
- After `down && up && provision`, no image is pulled from an upstream registry (the cache's log shows only hits).
- `opsforge partition c`: c-w1 → `NotReady`, pods rescheduled after the eviction timeout;
  `opsforge heal` → node recovers cleanly without manual intervention.

**Met 2026-10-22:** `verify` green including the K3s checks; `down && up && provision` takes 2.0–2.6 minutes and a
second `provision` reports `changed=0`. Second rebuild after the cache was warm: 98 registry requests, all 200,
11 cache hits, 0 layers fetched upstream. `partition c`: c-w1 NotReady at 53 s, its pod running in b at 114 s,
sites a and b keep serving their ingress; `heal`: c-w1 Ready 13 s later and the stranded pod removed. Snapshot
(0.8 s) and restore (13.6 s) of the running cluster: every pod back within a minute. `verify --mem --disk`:
**1182 MiB** anon + kernel idle (a-cp1 757, b-w1 175, c-w1 170, cache 55, gateways 7 each, wan-core 3), pool
4.13 GiB. Incus' own figures show the K3s nodes at their limits: page cache, reclaimable; no evictions or
memory pressure seen.

### Phase 5: WAN-aware observability (done 2026-10-23)
- [x] Monitoring node **`mon`** (role `monitoring`, site b, 512 MiB) outside the cluster and away from the control
      plane's site, so partitioning site a does not blind it. **VictoriaMetrics** (Debian's package, 1.112) rather
      than Prometheus for RAM: retention 2 d, scrape 30 s, WAN probes 15 s; `-search.latencyOffset=0s` (by default
      queries hide the last 30 s, which made a link change take ~40 s to show) and `-memory.allowedBytes=64MiB`.
      **Grafana** 13.2.4 from Grafana's apt repository with provisioned datasource and dashboards (`ansible/roles/
      monitoring/files/dashboards`, read-only in the UI), anonymous read-only access, no calls home, no
      preinstalled app plugins. Reach it with `./opsforge forward mon:3000` (admin password in
      `.opsforge/grafana-admin`). `topology_errors`: one monitoring node at most, and it needs `interconnect: vpn`.
      **Found while setting up:** Grafana's package is 380 MB (1.4 GiB installed) and its CDN serves this host at
      0.2–0.7 MB/s; it is now downloaded once to `.opsforge/cache/` (checked against the repository's SHA-512) and
      copied over the management path. `APT::Keep-Downloaded-Packages "false"` on every node (base role): apt-get had
      kept 430 MB of `.deb`s on mon.
- [x] node_exporter on every node and gateway (role `exporters`; Debian/Alpine packages, collectors that only show
      the shared host disabled), and a **blackbox exporter on each gateway**, so each site pair is measured from its
      own end: `wan_icmp` (peer gateway's private address, through the VPN, so OSPF reroutes show), `wan_tcp` (TCP
      connect to the peer's public IP:22) and `wan_http` (GET of the peer site's public ingress, where a node exposes
      80). Gateways' firewalls (rendered by opsforge) accept 9100/9115 from `mon` over the tunnels. K3s:
      kube-state-metrics v2.19.0 (NodePort 30808), kubelets, cAdvisor and the API server scraped with a read-only
      ServiceAccount token copied to mon; only the series the dashboards use are kept. 33 targets.
- [x] Dashboards: **WAN** (RTT, loss and HTTP matrices src × dst, per-pair RTT and TCP connect, failing probes),
      **Lab hosts** (exporters up, memory, CPU, network) and **K3s overview** (nodes Ready as the control plane sees
      them, kubelets reachable from mon, pods per node and phase, working set per node and namespace, API requests,
      restarts).
- [x] `verify` checks, once mon is provisioned: every scrape target up, a successful ICMP probe for every ordered
      site pair, Grafana healthy.

**Done when:** changing a link's RTT with `opsforge link` appears on the matrix within one scrape interval.
**Met 2026-10-23:** scenario `matrix-follows-link` (a-b to 200 ms, both directions): seen after 11.7–17.0 s, reset
seen after 16 s, within the 30 s scrape interval; the bound is one probe interval (15 s, each direction scheduled
independently) plus ~1 s of ingestion. Fresh `down && up && provision` 7.7 min (Grafana's install), a no-op
`provision` 57 s with `changed=0`. `verify --mem --disk`: **1635 MiB** anon + kernel idle (a-cp1 746, b-w1 223, c-w1
190, mon 326 of which Grafana ~175 and VictoriaMetrics ~67, cache 64, gateways 28 each with two exporters), pool
6.47 GiB.

### Phase 6: Fault injection and scenarios (done 2026-10-23)
- [x] CLI, all undone by `heal` (they share `partition`'s pre-fault state on wan-core): `degrade <link> --rtt
      --jitter --loss --rate --mtu`; `mtu <link> <bytes>`, a path-MTU black hole (wan-core drops packets longer than
      that between the two sites' public IPs and sends no ICMP; also a link property, `link --mtu`, schema `mtu`);
      `flap <link> --period 10s [--duration 60s]`, foreground, restores the link when it stops (measured: a 10 s
      flap at 4 s period, 56 % ping loss, link and state clean afterwards).
- [x] Scenario runner `opsforge scenario <name>` (`--list`): `scenarios/<name>.yml` has `setup`, `fault`, `measure`
      and `teardown` step lists and `expect` (metric → condition or list of conditions, e.g. `">= base.p50_ms +
      300"`). Steps: `sleep`, `partition`, `heal`, `degrade`, `mtu`, `flap`/`stop_flap` (background), `run` (host or
      instance shell), `ping`, `http` (latency percentiles), `transfer`, `wait_until` (shell or PromQL condition) and
      `promql`; `${ip:<host>}` / `${public:<site>}` placeholders. Teardown always runs after `heal`, even when a step
      fails; a scenario refuses to start while a fault is in place. Results go to `.opsforge/results/<name>-<time>.json`
      and are compared with the previous run. Format and checks in `lab/scenario.py` (unit-tested).
- [ ] Toxiproxy (optional): not added; `degrade`/`mtu`/`flap` cover the network faults so far.
- [x] Scenarios, all passing on a fresh lab (2026-10-23):
  - `partition-b-writes`: a Job in b writes 120 ConfigMaps through the API server in a, retrying each; b is
    partitioned for 60 s. **No write lost** (120/120), writes stalled 67–72 s, then resumed. 216 s.
  - `latency-a-c`: a-c from 120 to 300 ms; HTTP from c to a's ingress p50 **257 → 625 ms** (two extra round trips
    of +180 ms: TCP handshake and request), back to 252 ms after heal.
  - `pmtu-blackhole-a-b`: `mtu a-b 1400`; small requests 100 % OK, a 2 MB response **stalls** at 512 bytes, works
    again after heal. MSS clamping cannot help: the hole is below the tunnel MTU.
  - `clock-skew`: libfaketime (`faketime` on every node) +400 days: the node certificate (365 d) fails `openssl
    verify`, and curl to K3s' API fails with 60 (expired serving certificate).
  - `matrix-follows-link`: Phase 5's acceptance as a repeatable check.

**Done when:** each scenario runs from one command and prints pass/fail against its expectation. **Met.**

### Phase 7: Lightweight CI/CD (optional `ci` node) (done 2026-10-23)
- [x] **Opt-in by role**, like `monitoring`: a node `ci: { site: a, role: ci, cpu: 2, memory: 1GiB }` in
      `topology.yml` (the planned `profiles.ci` flag was dropped as redundant). `topology_errors`: one ci node at
      most; it needs a k3s-server (Flux deploys to it) and `interconnect: vpn` (every site pulls from its registry).
- [x] **Gitea 28.2** (binary from dl.gitea.com, sha256-checked, cached in `.opsforge/cache/` and copied over the
      management path like K3s) with **Actions**: SQLite, HTTP on 3000 (the VPN encrypts between sites), offline
      mode, registration off. Every secret Gitea would otherwise write into `app.ini` comes from `.opsforge/ci/`
      (**found:** with OAuth2 off it still wants `oauth2.JWT_SECRET` and crash-looped on the read-only file).
      Ansible creates the admin `opsforge` (CLI, password in `.opsforge/gitea-admin`), the bot `ci-bot` (write on the
      deploy repo only) and the public repos `opsforge/webapp` and `opsforge/deploy` through the API.
      **Chose Gitea Actions over Woodpecker:** Gitea is needed either way; Actions adds one process (the runner)
      instead of a server and an agent.
- [x] **Runner** `gitea-runner` 5.2 (act_runner's new name), label `lab`, **host mode**: jobs run as root on the ci
      node, no Docker. Images are built with **buildah** (Debian package), `--isolation chroot --network host`, which
      works in the unprivileged container without nesting (spike: `netavark` must be installed even with host
      networking). Base images come through the zot cache (`/etc/containers/registries.conf.d/50-opsforge.conf`).
      No `uses:` actions: in host mode they need Node.js and a download from github.com; the workflow checks out
      with `git`. Topology-dependent values (registry, deploy repo, bot) are Actions variables set by Ansible, the
      bot's password an Actions secret, so the seeded repository is static.
- [x] **Registry:** Debian's `docker-registry` (distribution 2.8, ~30 MiB) on :5000 with **TLS from the node's lab
      certificate** (`ci.a.lab`), no auth (reachable from the VPCs only, never exposed). Every node already trusts the
      lab CA, so containerd and buildah need no insecure-registry settings; sites b and c pull across the WAN.
- [x] **Flux 2.9** (source + kustomize controllers only, ~60 MiB) rendered once by the Flux CLI on the controller
      (`flux install --export`, cached under `.opsforge/k3s/`) and applied as a K3s manifest, with a GitRepository
      polling `http://ci.a.lab:3000/opsforge/deploy.git` every 30 s and a Kustomization (prune). K3s' deploy
      controller retried the GitRepository until Flux's CRDs existed: no ordering needed. **The pipeline commits the
      new tag** (`newTag` in `webapp/kustomization.yaml`) instead of Flux's image-automation controllers: two fewer
      controllers and no Gitea write credentials in the cluster. This replaces the planned "webhook stub".
- [x] **Reference workload** `webapp` (`ansible/roles/ci/files/webapp`, Go + `lib/pq`, static binary FROM scratch,
      ~7 MB to pull per site): one replica per region (served at `/app` by each site's ingress, same zone-preferring
      Service as the demo) and one PostgreSQL 18 in the control plane's region (local-path PVC). Each response gives
      the version (commit), node, region (the pod's `topology.kubernetes.io/region` label, copied from the node by
      Kubernetes 1.37) and the database round trip: 1–15 ms in a, 75–90 ms in b, 250–300 ms in c.
      The repositories are seeded on the first provision only (while empty): then Gitea is the source of truth.
      **Found while testing:** after the first push two webapp pods ran in c and none in a: topology spread counted
      the old revision's pods while placing the new ones. `matchLabelKeys: [pod-template-hash]` spreads each revision
      on its own; three rollouts in a row then kept one pod per region.
- [x] `verify` checks, once ci is provisioned: Gitea healthy, the runner online, every K3s node reaching the registry
      with a trusted certificate, the last webapp run successful, Flux's GitRepository and Kustomization Ready, and each
      site's `/app` served in its own region with a working database. Scenario **`ci-rollout`**: a push from the host
      (through `forward ci:3000 --local 13000`, like a developer on the internet), then build, then each site serving
      the pushed version from a replica in that site.
- [x] **Found and fixed:** mon (512 MiB) thrashed at its limit during a provision (100 % memory pressure, an OOM
      kill, even `incus exec` hung for minutes): Phase 7's series raised its idle use to ~400 MiB and Debian's
      node-exporter collectors ran their apt timer (a Python apt-cache load every 15 min) on top. mon is now 768 MiB
      and that timer is masked on every Debian node.

**Done when:** a push to Gitea builds an image, pushes to the registry, and Flux rolls it out across sites.
**Met 2026-10-23:** `ci-rollout` passes: build 3–9 s (warm module and build caches; the first build on a fresh ci node 26 s,
golang:1.27-alpine coming from the zot cache, which survives `down`), every site serving the new version 53–59 s after the push (Flux's
30 s poll, then the pull and readiness). Fresh `down && up && provision` 6.5 min, green first time (the first build
runs while K3s installs); a no-op `provision` 71 s with `changed=0`. `verify --mem --disk`: **2068–2217 MiB** anon +
kernel idle (ci 234, mon ~400, agents ~300 with Flux and the webapp), pool 8.8–9.0 GiB (the ci node ~1.4 GiB:
buildah, Gitea, git, the golang base image).

### Phase 8: Multi-cluster (realistic multi-cloud) (done 2026-10-23)
- [x] **`cluster:` per node** (schema; K3s nodes only, default `lab`): nodes sharing a cluster form one K3s cluster,
      so the Phase 4 stretched cluster is simply every node in `lab`. `render.k3s_clusters()` gives each cluster its
      server, nodes, sites and the host's API port (6443 + its index); the inventory carries it as `lab_clusters` and
      each K3s node's `lab_cluster`. Everything that assumed one cluster is keyed by it: the K3s token (the default
      cluster keeps `.opsforge/k3s/token`, others `token-<cluster>`), `.opsforge/kubeconfig-<cluster>` (contexts named
      after the cluster), `opsforge kubectl --cluster <name>` (default: the first; its own forward port), the demo
      (one replica per region of each cluster), `verify` (nodes, demo, Flux per cluster), mon's scrape config (one job
      per cluster and kind with that cluster's token; `job` label unchanged, `cluster` label added) and Flux (each
      cluster applies `clusters/<name>` of the deploy repository). `topology_errors`: exactly one server per cluster;
      several clusters need `interconnect: vpn`.
- [x] **Second layout** `topologies/multi-cluster.yml`: same sites and WAN, one cluster per site (`a-cp1`, `b-cp1`,
      `c-cp1`, all servers), chosen with `OPSFORGE_TOPOLOGY=topologies/multi-cluster.yml` (inherited by nested
      `./opsforge` calls, so scenarios follow it). `topology.yml` stays the stretched layout. `up` recreates a node whose
      K3s role or cluster changes (instance tag `user.opsforge.cluster`): K3s cannot turn an agent into a server, or
      move a node to another cluster, in place. Switching layouts: `down` then `up` (Gitea's repositories are only
      seeded while empty, and the deploy layout differs).
- [x] **Skupper 2.2.3** between clusters (one site per cluster in namespace `mesh`), application-level: no CNI change,
      the clusters keep the same pod/service CIDRs. The release's cluster-scope manifest is applied as a K3s manifest
      with three env additions: access types `local,nodeport`, default `nodeport`, and `SKUPPER_CLUSTER_HOST` = the
      server's private IP (servicelb is off, so no LoadBalancer; the CLI only offers route/loadbalancer, the controller
      has `nodeport`). Links therefore ride the VPN like all inter-site traffic (shaped). Each cluster links to every
      cluster listed before it (`render.cluster_links`), generated with `skupper link generate` on the peer's server and
      applied on its own; **link cost = the smallest RTT in ms between their sites** (b→a 40, c→a 120, c→b 90), so a
      routing key is served from the nearest cluster that has it. Images `IfNotPresent` (the release says `Always`).
      **Found:** Ansible passes `'\n'` in a Jinja string through literally, so `replace()` never matched the manifest's
      `env:` block (the site then reported "Unsupported value for LinkAccess: nodeport"); `regex_replace` reads it.
- [x] **CI across clusters:** the deploy repository is `webapp/` (base, still holding the image tag the pipeline
      edits), `postgres/` and `clusters/<name>/` (what each cluster's Flux applies: the base, a replica per region, and
      either the database or a Skupper Listener for it). PostgreSQL stays in the first cluster with a Connector; the
      other clusters' webapps use `postgres.mesh` through Skupper, transparently (only `DATABASE_URL` differs).
      Measured: database round trip from b 97 ms, from c 305 ms. ci-rollout: every site serves a pushed commit 46 s
      after the push (three Flux instances polling independently).
- [x] **Scenarios run on either layout:** `${node:<site>}` (the site's first K3s node), `${server:<site>}` (its
      cluster's server) and `${cluster:<site>}` placeholders; `layout: stretched | multi-cluster` marks the ones that only
      make sense on one (skipped, with a message, on the other: `partition-b-writes` needs one stretched cluster).
- [x] Scenario **`cluster-failover`** (multi-cluster): a backend in the clusters of a and b, only a Skupper Listener
      (and an ingress path) in c. c is served from b (cost 90 < 120); `partition b`: new connections fail for **2
      requests (~2–4 s)**, then are served from a; 100 % success through c's ingress during the partition; new
      connections back on b 34–39 s after heal. Traefik's pooled keep-alive connections stay on the cluster they were
      opened to (per-connection routing), so the scenario measures failover with new connections to the Listener.
      **Found:** the first runs failed every request for ~5 minutes (8.5 s each with a 10 s timeout). The Connector's
      `host` (`backend.scenario.svc.cluster.local`, 4 dots, under ndots 5) was looked up through every search domain
      first, the nodes' `a.lab b.lab c.lab` included, and the partitioned site's zone timed out at its gateway across
      the WAN: a partitioned site's DNS slows every other site's new connections. A trailing dot (FQDN) fixed it: the
      first request after the partition was already served from a. The webapp's PostgreSQL Connector has it too.
      Also: the scenario's setup waits for both backends to be available (a race made a run fail fast).
- [x] `verify`: per cluster as above, plus each Skupper site Ready and every expected link Ready (with its cost).
- [x] **Sizing found while testing:** a server per cluster carries the control plane plus Traefik, Flux, Skupper's
      controller and router, kube-state-metrics and the apps: **~1.2 GiB anon each**. At 1280 and then 1536 MiB the
      servers thrashed (memory pressure 46–70 %, a TLS handshake timeout on the API, a Skupper site stuck Pending,
      a link generation failing); at 2 GiB pressure is ~0. `verify --mem/--disk` ceilings are now per layout
      (stretched 2500 MiB / 10 GiB, multi-cluster 4800 MiB / 14 GiB).
- [x] Geo-aware DNS: built as Phase 9 (Skupper's cost-based routing serves services between clusters; GeoDNS
      steers clients between the sites' ingresses).
- [ ] Flannel `wireguard-native` with public node IPs (deferred here from Phase 4): not done; Skupper made it
      unnecessary for linking clusters.
- Not explained: through Skupper, curl's per-request timer reports about half the RTT for connections Skupper
  already holds (47 ms on the 90 ms b-c link; 200 ms at 400 ms), while the wall clock over 11 keep-alive requests
  matches one RTT each (957 ms, like 1091 ms over plain TCP with one more handshake). Treat `http` step latencies
  through Skupper as relative.

**Done when:** a service survives the total loss of the cluster it was primarily served from.
**Met 2026-10-23:** `cluster-failover` passes repeatedly (2 failed requests, then 100 % from a). Multi-cluster lab
from scratch (`down`, then `up` and `provision` with `OPSFORGE_TOPOLOGY=topologies/multi-cluster.yml`): every check of
`verify` green (3 clusters, 3 sites and 3 links Ready, 3 Flux, webapp in every region through Skupper, monitoring of
all three); every scenario passes, `partition-b-writes` skipped; a no-op `provision` 89 s with `changed=0`.
`verify --mem --disk`: **4450 MiB** anon + kernel (servers 1176–1245, mon 467, ci 188), pool 11.98 GiB.

### Phase 9: Geo-aware DNS (done 2026-10-23)
A global name, `www.global.lab` (`render.GEO_NAME`), that every site resolves to the nearest ingress that is up: the
GSLB / health-checked GeoDNS of a multi-region deployment. Works on both layouts (it only needs `expose: ["80"]`).
- [x] **Preference order per site, from `topology.yml`** (`render.geo_targets`): the own site's ingress first, then
      the other sites' by link RTT (`ospf_cost`, the same cost as OSPF and Skupper; ties in topology order). Sites
      without an HTTP ingress (`expose` of public 80/tcp) are skipped. Live `link` edits do not reorder it, like
      geography-based GeoDNS. Split horizon: the own site is answered with the ingress node's VPC address (gateways do
      no hairpin NAT), the others with their public IP, so clients reach them across the WAN like any internet client.
- [x] **Health checks on each gateway** (Ansible role `dns`; inventory `lab_geo` per gateway, `lab_geo_name`):
      `opsforge-geo`, a POSIX sh loop (busybox `wget`, ~1.4 MiB RSS) under OpenRC's `supervise-daemon`. Every 2 s it
      probes the sites in order (`GET /`, 2 s timeout, a second try so one lost SYN on a lossy link is not an outage)
      and stops at the first that answers, so a healthy lab costs one local request per site every 2 s. The answer is
      a dnsmasq `addn-hosts` file, rewritten and re-read (SIGHUP) only when it changes, and logged to syslog
      (`opsforge-geo`). With no site answering it falls back to the first. TTL 0 (dnsmasq's local default), so
      clients re-resolve on every lookup. Detection = one interval plus the failed probes: ~5 s for a black hole.
- [x] `topology_errors`: no site may be named `global` (the zone). `verify`'s DNS check resolves the name on every
      node and expects its site's first target.
- [x] Scenario `geo-dns-failover` (both layouts): c's ingress goes dark (an nft `prerouting` drop of port 80 at raw
      priority on c's node, before the hostPort DNAT: a dead load balancer, K3s untouched); c's clients must resolve
      b's public IP (b is 90 ms from c, a 120 ms) and get every request answered there, then come back.
- Not built: answers per client subnet (EDNS Client Subnet; each site's resolver already is the "region"),
  weighted/latency-measured routing, or a resolver reachable from outside the lab.

**Done when:** a client is sent to the next-nearest healthy site within seconds of its own site's ingress failing,
and back after it recovers.
**Met 2026-10-23 (stretched):** `geo-dns-failover` passes: resolved to b 4.9 s after the fault, 10/10 requests
answered through b (p50 197 ms: TCP handshake + request on the 90 ms b-c link), back to c 1.0 s after recovery.
`verify` green, a second `provision` `changed=0`; gateways 29.5 MiB anon + kernel each (unchanged within noise).
From scratch (stretched, 391 s): `verify` green at 2031 MiB anon + kernel, pool 8.84 GiB; every scenario passes
(`cluster-failover` skipped); failover 6.3 s this run; a second `provision` `changed=0`.
**Met 2026-10-23 (multi-cluster, from scratch):** build 435 s; `geo-dns-failover` resolved to b after 6.1 s, 10/10
requests through b (p50 187 ms), back after 0.3 s; every other scenario passes (`partition-b-writes` skipped); a
second `provision` `changed=0`; 3660 MiB anon + kernel, pool 11.82 GiB. `verify` right after the build once saw the
b→c `wan_icmp` probe down (the VPN still converging); it passed on the next run.

### Phase 10: Teaching labs and web interface (done 2026-10-24)
Guided exercises on the live lab: a briefing, ordered steps, and checks that read the lab's real state (the scenario
step types). One learner at a time on this host (the lab is single), served on 127.0.0.1 only.
- [x] **Content as packs of files** (`labs/<pack>/`: `pack.yml`, `curricula/*.yml`, `labs/<id>.yml`, `badges.yml`),
      validated by `lab/labpack.schema.json` plus cross-references (`labs.errors`). Files are the source of truth;
      `opsforge labs import` (and `serve` at start) upserts them into the database. A lab's content hash flags progress
      made on an older revision ("updated since you passed it") instead of discarding it. Lab ids are global.
- [x] **Learner state in SQLite** (`.opsforge/labs.db`, stdlib `sqlite3`; numbered migrations in `lab/migrations/`,
      applied version in `PRAGMA user_version`): users (one local user to start), packs, curricula, levels, labs,
      prerequisites, attempts, step results, an append-only event log, badges and awards.
- [x] **Progression**: a level unlocks when the previous level's labs are passed, or at its `xp_required`; a lab
      also needs its `requires`. A pass is worth the lab's `xp`, less the pack's `hint_penalty` per hint (at least
      half); a lab's best pass counts. Badges are declarative rules (`labs_passed`, `lab_passed`, `curriculum_complete`,
      `xp`) evaluated over every pass, so a rule added later is awarded from history.
- [x] **Extension points**: check types (`labs.CHECKS`: `probe` = scenario steps + `expect`, `quiz`, `confirm`), badge
      rule types (`labs.RULES`), packs (a folder), layouts (`layout`/`needs` per lab, matched against the topology).
- [x] **`opsforge serve`**: stdlib HTTP server (no build step, no new host packages, no lab RAM): JSON API, live
      topology state for the panel, checks run with `ScenarioRun`, and a browser terminal (xterm.js over a stdlib
      WebSocket to a pty running a shell in the repo). Only `Host`/`Origin` 127.0.0.1/localhost are served (a page
      on another origin can open a WebSocket to localhost).
- [x] **Interface** (`web/`): a railway signal-box mimic panel: sites as stations, WAN links as tracks with their live
      RTT, lamps white (as in `topology.yml`), amber (changed live) and red (partitioned). Catalog per curriculum and
      level, lab workspace (steps | panel over terminal), badges.
- [x] `core` pack: Cloud and Kubernetes curricula with first labs; `make check` validates the packs.

**Done when:** a lab can be started, worked through in the browser terminal and passed with checks against the live
lab; importing the packs twice changes nothing; editing a lab flags passes of the old revision; a badge rule added
after the fact is awarded from history.
**Met 2026-10-24 (stretched, live):** every core lab worked through on the running lab with the API the page uses,
the learner's commands run as written: 6/6 passed, 800 XP, 6/6 badges; `verify` green afterwards and a second
`provision` `changed=0`. Measured in Losing a site: NotReady 44-51 s after the partition, pod replaced 60 s after the
taint, heal + teardown ~25 s. Routing around failure: OSPF reroute ~10 s, detour 130 ms. Server: 44 MiB RSS on the
host (no lab RAM); database 170 KiB. Found and fixed on the way:
- Lab bodies went through sorted JSON, so `{run: ..., name: x}` came back as a `name` step: `scenario.step_type`
  now skips `name`, and bodies are stored in file order.
- A probe on an untouched lab sat through every later `wait_until` (61 s): probes stop at the first named step whose
  expectations fail; the rest show "not checked".
- The demo Deployment lacked `matchLabelKeys: [pod-template-hash]` (the Phase 7 lesson), so rolling it piled two
  pods in c and none in a: added to `demo.yaml.j2` and taught in Scale across regions, whose spread check now also
  waits for the rollout (it passed early on the old pods' placement).
- Losing a site left the demo and the webapp off region c (spreading acts only at scheduling), failing `verify`: its
  teardown rolls every spread Deployment outside kube-system.
- Lab texts corrected against what the lab prints (the `ip addr` output's flannel/cni interfaces, the scheduler's
  default soft spreading, the reroute time).

### Phase 11: OpsForge Academy (planned 2026-10-24)
Courses structured like Udemy's (lectures, multiple-choice quizzes, labs, LeetCode-style challenges), a VS Code-grade
editor (Monaco) and a Kubernetes catalogue from basics to advanced, built as a production app: a Go platform service
(modular monolith, ports and adapters, DI, sqlc + SQLite, OpenAPI first), a React + Vite + shadcn/ui front end, the
Python `opsforge` CLI as the lab toolchain behind a JSON contract, tests first. The plan lives in
[docs/courses/](courses/README.md): architecture, frontend, design, content model, syllabus, testing, milestones
(M0 foundations → M8 hardening). Phase 10's stdlib server is replaced at M2.

- [x] **M0 foundations (done 2026-10-24).** `academy/` Go module: typed config (flags over `ACADEMY_*`, loopback
  only), slog JSON with request ids, `/healthz` and `/readyz` (database + toolchain contract), graceful shutdown with a
  WAL checkpoint, Wire graph, goose (embedded) + sqlc on `modernc.org/sqlite`, golangci-lint v2 whose depguard rules
  enforce the layering (proved by planting violations). `academy/api/openapi.yaml` (3.1) → oapi-codegen strict server
  and openapi-typescript client; contract tests validate every response against the spec and fail when a documented
  response is produced by no test. `opsforge academy compile|state|version --json` with `lab/academy.py`,
  `lab/course.schema.json` and `academy/api/bundle.schema.json`; the golden bundle and state documents in
  `academy/testdata/` are written by pytest and read by the Go adapter's tests. `web/` is the Vite + React 19 app
  (TanStack Router/Query, Tailwind v4, shadcn with the Phase 10 tokens, self-hosted fonts, Vitest + MSW, Playwright);
  Phase 10's static UI moved to `lab/web/` until M2. CI: `python`, `go`, `web`, `generated`, `e2e` jobs.
  **Measured:** `academy` binary 14 MB; 26 MiB RSS (16 MiB anon) serving with the real toolchain; initial JS 121 KB
  gzip (budget 250), CSS 14 KB gzip; `make check` green; e2e 8/8 on Chromium desktop and 390 px. Disk on the host:
  `web/node_modules` 411 MB, Playwright browsers 681 MB (`~/.cache/ms-playwright`), Go module cache 2.8 GB.
  Found on the way:
  - TypeScript 7 (the native compiler) has no JS API, which typescript-eslint needs: pinned `typescript ~6.0`.
    `eslint-plugin-jsx-a11y` supports ESLint ≤ 9: pinned `eslint ^9`.
  - openapi-fetch captures `fetch` when the client is created, before MSW patches it: the client looks it up per
    call. MSW 3 renamed `onUnhandledRequest` to `onUnhandledFrame`.
  - The shadcn CLI wrote `import { cn } from "cn"`: fixed by hand (check each `shadcn add`).
  - A `-race` test binary re-executed as the fake toolchain sleeps 1 s on exit: `GORACE=atexit_sleep_ms=0`.
  - Wire needs `github.com/google/wire` in the service's own go.mod (tidy drops it before `wire.go` exists).
- [x] **Spike S6, Go terminal (done 2026-10-24): holds.** `internal/terminal` (sessions, 64 KiB replay ring, 60 s
  linger after the socket drops, newest tab takes the session over, a client that falls 256 chunks behind is
  detached and reconnects to the replay, 8 sessions at most) + `terminal/ptyshell` (creack/pty, SIGHUP to the
  process group on close, Phase 10's rcfile with shell-quoted paths) + `POST /api/v1/terminals` and
  `/ws/terminal/{session}` (coder/websocket; Phase 10's frames: binary output, `{t:i,d}` input, `{t:r,cols,rows}`
  resize; close codes 1000 exited, 4000 replaced, 4001 slow, 1001 shutdown). Parity proved by a test on real bash:
  typed input, bracketed paste (`ESC[200~…ESC[201~`), resize seen by `stty size`, the `opsforge` function,
  reconnect replay, exit. Linger, takeover and slow-client rules are `testing/synctest` tests (a mutation that
  dropped the linger reset was caught only after adding the reconnect-then-disconnect case). The Host guard (421
  for non-loopback names: DNS rebinding) and the WebSocket Origin check came with it. Live on the built binary:
  `opsforge academy state` typed in a browser terminal answers in 0.6 s; 17.8 MiB RSS with 4 shells; SIGTERM hangs up
  every shell.
- [x] **M1 platform core (done 2026-10-25).** Modules identity, catalog, contentimport, history (event bus + SSE),
  progress, quiz, operations, workspace, labsession, then **rewards** (awards only; XP read from the passes; Phase
  10's rule types + course badges; evaluated after each rewarding event and for every user after each import, so new
  rules are retroactive; `GET /api/v1/badges`), **labstate** (`GET /api/v1/lab/state`, 5 s cache invalidated by lab
  events; lab texts resolved with the `placeholders` table `opsforge academy state` now returns, computed by
  `scenario.substitute` itself, plus `${ns}` of the learner's attempt) and **`academy migrate-legacy`** (Phase 10's
  labs.db in one transaction, imported ids remembered so a re-run imports only what is new).
  **Acceptance met 2026-10-25 (stretched, live), through the API with the learner's commands:** a locked lab refused
  (409 `/problems/lab-locked`), an attempt abandoned, then 6/6 labs passed: one hint (Regions and latency, 90 XP), the
  "not yet" path answered in ~1 s, a reset in Nodes and regions started the steps over; Routing around failure
  rerouted in ~11 s; Losing a site NotReady 43 s after the partition, pods moved after 63 s more, back Ready 16 s after
  `heal`. 790 XP (Phase 10's 800 less the hint), 6/6 badges, the temporary course complete; importing unchanged packs
  changed nothing; editing a lab flagged exactly its pass, reverting cleared it; a badge added afterwards was awarded
  from history and stayed (retired) when removed. `verify` green afterwards. Server 48 MiB RSS. The real Phase 10
  labs.db migrated: 9 attempts, 38 step results, 62 events, 6 awards; 800 XP and 6/6 badges as in Phase 10; a second
  run imported nothing. `make check` and `make e2e` (16/16) green.
- [ ] **Split into two repositories (decided 2026-10-25), right after M1's acceptance run, before M2.** OpsForge
  stays the infra lab for developers (Python CLI, Incus, Ansible, scenarios); OpsForge Academy becomes its own repo
  (Go platform, web app, course and lab content) that drives an installed `opsforge` only through the JSON contract.
  Why: different audiences and toolchains (an infra user should not need Go/pnpm/Playwright), independent releases,
  a one-way dependency, and the contract (`contract: N`, `toolchain.schema.json`, golden documents) is already the
  boundary. Why not before: the rest of M1 still changes the contract, and both sides change in one commit until it
  is frozen. Until the split, keep coupling from growing: lab-side features only through the contract (Go never reads
  `.opsforge/` or `topology.yml` itself; from step 5 not even the checkout), content stays out of `lab/` code.
  Decided 2026-10-25: the new repo is `opsforge-academy` (github.dev.com/IhebBelhadj/opsforge-academy, Go module
  `github.com/IhebBelhadj/opsforge-academy`); the course compiler is **ported to Go** (Academy is one language, the
  bundle becomes in-process data); the toolchain commands become `opsforge api <cmd>`.
  **Boundary (decided 2026-10-26).** Two standalone projects with nothing shared but the contract:
  - **OpsForge** owns everything that touches the lab: the CLI, `lab/`, `topology.yml` + `topologies/`, `ansible/`,
    `scenarios/`, the scenario step language and its execution, `.opsforge/` (keys, state, workspaces), the host
    scripts, and `contract/`. It **opens an API** (REST + WebSocket, `opsforge api serve`) and knows nothing about
    courses, learners, XP or Academy's database.
  - **OpsForge Academy** is a learning platform that stands on its own (users, courses, lectures, quizzes, progress,
    rewards, its web app, its database, its content). From OpsForge it needs one thing: **somewhere to run what the
    learner does**. For each lab or challenge it asks OpsForge for a **sandbox** sized by the course's requirements
    (step 7), works only inside it (files, terminal, checks) and deletes it at the end. It never reads OpsForge's
    files, never runs its CLI, never shares a disk with it: a URL and a token.
  - Course content (lab and challenge packs, with their setup/teardown/probe steps) is Academy's: the steps are data
    in OpsForge's step language, sent in sandbox requests and checked by `validate-steps`, like SQL sent to a
    database. OpsForge's own `scenarios/` stay OpsForge's.
  - Files used by both today are split, never shared: each repo gets its own Makefile, CI workflow, README,
    CLAUDE.md, `.gitignore`, `.yamllint` and CHANGELOG (step 6's table).
  Steps, in this repo first (each leaves `make check` green):
  1. **Contract v1 for two repos** (done 2026-10-25, see "Versioning" below): `opsforge academy …` → `opsforge api
     …` (`--json` kept: without it the commands print for humans); OpsForge owns `contract/` (`toolchain.schema.json`
     with open outputs, `golden/*.json` written by its pytest, `README.md` with the rules and a table of minors);
     `api version` reports `{contract, minor, opsforge}` (`VERSION` in `opsforge`, `1.0.0-dev` until tagged); Academy
     keeps a pinned copy (`academy/contract/` + `PINNED`, `make contract-update`; `make contract-check` fails while it
     differs), reads outputs leniently and fails `/readyz` naming the minimum OpsForge when the minor is too old
     (`opsforgecli.MinMinor`/`MinRelease`).
  2. **Compiler to Go** (done 2026-10-25): `academy/internal/compiler` replaces `lab/academy.py`'s compiler and the
     pack logic of `lab/labs.py` it used (loading, schema + cross-reference checks, content hashes, badge rules);
     `course.schema.json` moved to `academy/api/`, `labpack.schema.json` copied there (Phase 10 keeps its own until
     step 3). Packs are read as PyYAML read them (YAML 1.1 scalars) and hashed over Python's `json.dumps` bytes: the
     Go bundle is byte-identical to Python's for the repo and demo packs, so no pass turned stale (re-import into
     the live database: only the pack row changed, its `path` now relative to the content folder). Scenario steps
     stay OpsForge's knowledge: the compiler sends every lab's and challenge's steps in one `opsforge api
     validate-steps` call (in minor 0, since contract 1 is not released yet; `compile` left the contract). Content
     import calls it in-process; `academy compile [--json] [pack...]` for authors and `make check`; `--content`
     (default `<root>/labs`) names the packs folder.
  3. **Phase 10 removed** (done 2026-10-25, before M2's parity at the user's choice: the old UI is not used, Academy's
     will have everything): `lab/labserver.py`, `lab/labsdb.py`, `lab/labs.py`, `lab/labpack.schema.json` (Academy
     has its copy), `lab/web/`, `lab/migrations/`, `tests/test_labs.py`, `opsforge labs` and `serve`. Until M2 labs
     run through Academy's API. `.opsforge/labs.db` stays: `academy migrate-legacy` reads the file, not the code.
  4. **OpsForge API service** (done 2026-10-25; decided the same day: before the move, so the repos split along a
     network seam and never share a machine or a filesystem again): `opsforge api serve` (`lab/apiserver.py`, stdlib
     `ThreadingHTTPServer`, port 8091) serves the contract over HTTP + WebSocket; `opsforge api <cmd> --json` stays for
     people and scripts, both on the same functions. `contract/openapi.yaml` maps `toolchain.schema.json`'s documents
     (new: `problem`, `jobOutput`, `filesOutput`, `moveInput`) to routes under `/v1/`; still contract 1 minor 0
     (nothing is released). Built as planned, with these choices:
     - *Jobs*: `POST /v1/runs|probes|heal` → 202 + job (`Location`), `GET /v1/jobs/{id}?wait=<s>` long-polls (≤ 60 s)
       instead of an SSE stream; `log` grows while it runs (`lab_run(lines=)`); one lab job at a time (409
       `lab-busy`); finished jobs kept an hour, none survive a restart (a client then sees 404 and fails the work).
       Shutdown waits for a running job (stopping halfway through a setup leaves the lab half-changed).
     - *Workspaces*: the learner also edits files from the terminal, so OpsForge holds the **only** copy
       (`.opsforge/workspaces/<ws>/`): create, list, read, write, delete, move, delete the tree, with Academy's limits
       (1 MiB per file, 10 MiB per tree, its path rules). Python has no `os.Root`: every path is opened component by
       component with `O_NOFOLLOW` from the workspace's folder, so a link made in the terminal never leads the API out
       (tested). Academy's `workspace.FileStore` port maps 1:1 onto these routes (step 5's adapter); ownership,
       versions (`base_sha256`) and the editor stay Academy's.
     - *Terminal*: `GET /v1/terminal?workspace=&cols=&rows=` upgrades to a WebSocket shell (Phase 10's wire format and
       framing, recovered from git); bash is spawned with `subprocess` + `setsid --ctty` on `os.openpty()` (no
       `pty.fork` in a threaded server: the child may deadlock), so it keeps job control; closing the socket hangs
       up its process group.
     - *Security*: bearer token `.opsforge/api-token` (0600, made on first serve, compared in constant time) on every
       route but `/v1/version`; any `Origin` header (a browser) → 403; on loopback a foreign `Host` → 421; another
       bind address is refused without `--tls-cert/--tls-key`.
     - *Tests*: `tests/test_apiserver.py` (26) runs a real server in a thread: token/Origin/Host, jobs (busy, live
       log, failed with a problem), validate-steps, workspaces (escapes, links, limits), the terminal (workspace,
       size, job control, close 1000), and the OpenAPI document vs the routes. Golden `job.json`.
     - *Not done*: a systemd user unit (the checkout's path is the user's; `opsforge api serve` runs in a terminal
       until a deployment needs it). Running it needs the `incus-admin` group like every lab command.
  5. **Academy on the API** (done 2026-10-25): `academy/internal/opsforgeapi` is Academy's only way to OpsForge: a
     hand-written HTTP client over `internal/lab`'s types (outputs checked against the pinned schema; oapi-codegen was
     not needed) implementing `LabController` (state; run, probe, heal as jobs polled with `?wait=`), `StepChecker`,
     the readiness check (version, minor, token), `workspace.FileStore` (the trees on the lab host; its methods now
     take a context) and `terminal.Spawner` (`terminal.Spec` is now a workspace and a size; the shell is a WebSocket
     to `/v1/terminal`). Deleted: `internal/opsforgecli`, `internal/terminal/ptyshell` (creack/pty),
     `internal/workspace/disk`, `web/e2e/fake-opsforge.sh`. Config: `--data` (default
     `$XDG_DATA_HOME/opsforge-academy`: `academy.db`, `opsforge-token`), `--content` (default `./labs`),
     `--opsforge-url` (plain http only to loopback), `--opsforge-token-file` (read at every call: a missing token is a
     readyz problem, not a start failure; migrate-legacy needs no OpsForge), `--legacy-db` (required by
     migrate-legacy); `--root`, `--toolchain`, `--workspaces` are gone. Tests: `opsforgeapi/opsforgefake` (the API in
     memory) serves httpapi's world, cmd/academy and the e2e suite (`cmd/opsforge-fake`); the golden documents decode;
     `make academy-test` and `academy-validate` run against a throwaway real `opsforge api serve`
     (`scripts/with-opsforge-api.sh`), where real bash in the terminal is tested (paste, resize, job control, a file
     made in the shell read back through the API). CI's go job installs Python for it; the python job no longer
     compiles packs (it has no Go).
     **Acceptance met 2026-10-25 (stretched, live)**, Academy talking only HTTP to `opsforge api serve` and every
     learner command typed into Academy's terminal (a shell on the lab host in the attempt's workspace): the same as
     M1's run: locked lab refused, an abandon, 6/6 passed (one hint: 90 XP; not-yet in ~1 s; a reset; Routing around
     failure rerouted in ~11 s; Losing a site NotReady after 50 s, pods moved after 61 s more, Ready 58 s after
     `heal`), 790 XP and 6/6 badges, import twice = no change, an edit flags exactly its pass, a late badge awarded
     from history. `verify` green afterwards. Academy 51 MiB RSS, `opsforge api serve` 40 MiB.
  6. **Move** (done 2026-10-26): `/home/iheb/projects/OpsForge/` is now a plain folder holding two checkouts:
     `opsforge/` (this repository, history kept, `.opsforge/` and the live lab moved with it) and `opsforge-academy/`
     (a new repository, `git init` with one initial commit, as asked: the history of `academy/`, `web/`,
     `labs/` and `docs/courses/` up to the move stays here). Academy's Go module is
     `github.com/IhebBelhadj/opsforge-academy/academy` (`go.mod` stays in `academy/`); its docs are `docs/` (this
     plan's `docs/courses/`); its Makefile finds OpsForge through `OPSFORGE_DIR` (default `../opsforge`) only for
     the real-API tests, the pack compile and `contract-update`; its CI checks OpsForge out next to it. OpsForge's
     `lab/academy.py` became `lab/contract.py` and `tests/test_academy.py` `tests/test_api.py`. Found on the way: the
     deeper checkout pushed the SSH ControlMaster socket (`cm-%C`, a 40-character hash) past the 108-byte Unix socket
     limit; `render` now uses `cm-%n` (the host alias). **Tags wait for step 7**: OpsForge `v1.0.0` (contract 1.0
     frozen) and Academy `v0.1.0` once sandboxes are in. From then on the two evolve separately: OpsForge changes the
     contract by minors (or a major), Academy updates its pinned copy (`make contract-update OPSFORGE_REF=<tag>`,
     fetched from this repository's git) when it wants an addition. Measured: `make check` green in both (OpsForge: 150 pytest; Academy: Go
     tests incl. the real-API ones, web 38, gen-check), Academy `make e2e` 16/16.
     What goes where (nothing stays shared):

     | OpsForge | OpsForge Academy |
     |---|---|
     | `opsforge`, `lab/` (`academy.py` renamed `contract.py`), `ansible/`, `topology.yml`, `topologies/`, `scenarios/` | `academy/` (Go platform, its pinned `contract/` copy), `web/`, `labs/` (content packs) |
     | `contract/` (owner: schema, `openapi.yaml`, golden documents) | `docs/courses/` → `docs/` (its plan; Phase 11's milestones continue there) |
     | `tests/` (`test_academy.py` renamed `test_api.py`), `scripts/host-setup.sh`, `k3s-spike.sh`, `lab/host/` | `scripts/with-opsforge.sh`, `scripts/contract.sh` (see "Disconnected" below) |
     | Makefile: shellcheck, yamllint, py_compile, ansible-lint, topology validation, pytest, contract golden check | Makefile: `academy`, `academy-test`/`-lint`/`-validate`, `web-check`, `gen`/`gen-check`, `e2e`, `contract-update`/`-check`, `academy compile` of the packs, yamllint of `labs/` |
     | CI: the `python` job | CI: `go`, `web`, `generated`, `e2e`, and a nightly contract job against OpsForge `master` |
     | `docs/PLAN.md` (Phase 11 keeps the API side and points to Academy for the rest), README, CLAUDE.md | its own README (the OpsForge range it supports), CLAUDE.md, `.gitignore` |
     | `.opsforge/` (incl. Phase 10's `labs.db`, read once by `academy migrate-legacy --legacy-db` from a copy) | Academy's data folder (`$XDG_DATA_HOME/opsforge-academy`) |

     **Disconnected** (2026-10-26, at the user's request: "they only communicate through APIs and they can live in
     separate machines"): Academy's build no longer reads `../opsforge` either. `OPSFORGE_DIR` and
     `with-opsforge-api.sh` are gone; the real-API tests and the pack compile use the API named by
     `ACADEMY_OPSFORGE_URL`/`_TOKEN_FILE`/`_CA_FILE`, on any machine (without: the tests skip and the compile runs
     against Academy's fake, warning that steps are unchecked); `contract-update` fetches `contract/` from this
     repository's git (`OPSFORGE_REPO`, `OPSFORGE_REF`) and `PINNED` records commit and checksum (`contract-check`
     offline); Academy's CI starts this API as a service outside its checkout. New Academy flag `--opsforge-ca-file`
     (trust a self-signed `--tls-cert`, for calls and terminal WebSockets). Here: the namespaces lab work creates are
     labelled `app.kubernetes.io/managed-by=opsforge-api` (was `opsforge-academy`: OpsForge names no client).
  7. **Sandboxes** (decided 2026-10-26, before the tags so contract 1.0 is frozen with them; still minor 0: nothing is
     released, so replacing routes is free). OpsForge's side first (contract, API, tests), then Academy's in its repo. Today Academy assembles an attempt from loose parts (a namespace name it
     picks, a workspace, setup/probe/teardown jobs, a terminal on a workspace) and must remember to clean each up.
     They become one OpsForge resource, the **sandbox**: an isolated place on the lab, made for one lab or challenge
     attempt, that OpsForge creates, holds and always cleans up.
     - `POST /v1/sandboxes` (`sandboxInput`: `{contract, kind: lab|challenge, label, requires, files, setup, teardown,
       expires_after?}`) → 202 + job; its output is the sandbox. `requires` comes from the course: `layout`
       (stretched / multi-cluster), `needs` (roles, sites, a `ci` node…), `faults: true` when setup may break links,
       `cluster_changes` (outside the namespace, undone by teardown). OpsForge checks them against the running
       topology and refuses with 409 `/problems/unmet-requirements` naming what is missing (`unmet_needs` moves
       here from Academy). Setup creates the namespace on every cluster (`sb-<id>`, labelled
       `app.kubernetes.io/managed-by=opsforge`), the workspace with `files`, then runs `setup`.
     - The sandbox document: `{contract, id, kind, label, status: creating|ready|failed|deleting, namespace,
       workspace, layout, placeholders (the topology's + `${ns}`), setup: runOutput, created_at, expires_at}`.
     - Everything else hangs under it: `GET /v1/sandboxes[/{id}]`; `/v1/sandboxes/{id}/files/...` (replaces
       `/v1/workspaces`); `GET /v1/sandboxes/{id}/terminal` (replaces `/v1/terminal`: bash in the workspace,
       `kubectl`'s default namespace the sandbox's); `POST /v1/sandboxes/{id}/probes` and `/actions` (jobs; a
       challenge's actions; faults refused); `POST /v1/sandboxes/{id}/reset` (teardown + setup); `DELETE
       /v1/sandboxes/{id}` (job: `teardown`, the namespace, `heal` when it had faults, the workspace). The loose
       `/v1/runs`, `/v1/probes`, `/v1/workspaces`, `/v1/terminal` and the `namespace` inputs go; `heal`,
       `state`, `version`, `validate-steps` stay.
     - OpsForge keeps sandboxes in `.opsforge/sandboxes/<id>.json` with their teardown, so cleanup never depends on
       Academy remembering it: they survive an API restart, `expires_after` (default 24 h without a call) deletes an
       abandoned one, and `opsforge api sandbox list|delete` lets the lab's owner clean up by hand. One sandbox
       with `faults` at a time (409 `/problems/lab-busy`, the lab is one cluster); others may coexist later
       (challenges only observe).
     - Academy: `labsession`/`challenge` take one `Sandboxes` port (create, reset, probe, actions, delete) instead
       of `LabController.Run/Probe` + `workspace.FileStore` + `terminal.Spawner` on names it chose; an attempt stores
       the sandbox id (`attempts.namespace`/`workspace` → `sandbox`, in step 8's baseline). Content keeps
       `namespace: auto` (= the sandbox's namespace, `${ns}`).
     - Tests first on both sides: `tests/test_apiserver.py` (create/refuse on requirements, cleanup after a failed
       setup, restart, expiry, files and terminal confined to the sandbox), golden `sandbox.json`; `opsforgefake`
       gains sandboxes; the six-lab run passes again through sandboxes.
     - **OpsForge's side done (2026-10-26):** `lab/sandboxes.py` (records, state machine, requirements, expiry),
       the CLI's `sandbox_up`/`sandbox_down`/`sandbox_probe`/`sandbox_action`, the routes in `apiserver.ROUTES` and
       `openapi.yaml`, `opsforge api sandbox list|delete`; 18 tests in `tests/test_sandboxes.py` plus the moved
       file/terminal/job tests (170 in all), golden `sandbox.json`. Decided on the way: the namespace is asked for
       (`namespace: true`, Academy's `namespace: auto`), not always made; the document has no `workspace` path (the
       files and terminal are routes); statuses add `resetting` and `deleted` (the delete job's output); setup and
       teardown may hold fault steps only with `requires.faults` (so delete knows to heal); `files` are UTF-8 texts;
       `cluster_changes` is a boolean (Academy keeps its texts for the learner); lab work a killed API left halfway
       is finished by deleting that sandbox when it comes back (the client sees 404 and fails the attempt); steps
       run in the workspace with `opsforge` on the PATH (`lab/bin`), so packs write `opsforge ...`, not
       `./opsforge` (Academy's packs move with its adapter); namespace label `opsforge.lab/sandbox=<id>` besides
       `managed-by=opsforge-api`.
     - **Academy's side done (2026-10-26, its repo):** `labsession` on one `Sandboxes` port (`opsforgeapi.Sandboxes`:
       `Create` → refusals at once, `Await`, `Probe`, `Reset`, `Delete`); the attempt's workspace is its sandbox (files
       and terminal under `/v1/sandboxes/{id}/`); its fake OpsForge serves sandboxes; packs write `opsforge ...`. Every
       lab's sandbox requires `faults`: several labs have the learner break links in the terminal (`opsforge
       partition`, `degrade`), which no setup step shows, and Phase 10 healed after every lab. Academy does not sweep
       orphan sandboxes (OpsForge may serve other clients): they expire.
     - **Acceptance met 2026-10-26 (stretched, live):** the six core labs played through Academy's API, every command
       typed into the attempt's terminal, Academy talking only HTTP to `opsforge api serve`: all passed (16, 8, 24, 5,
       20, 127 s), one sandbox per attempt, none and no namespace left after any, `verify` green; Academy 26 MiB RSS,
       the API 43 MiB. Next: tag OpsForge `v1.0.0` (contract 1.0 frozen), then Academy `v0.1.0`.
  8. **PostgreSQL** (decided 2026-10-25; in the Academy repo after step 7, before M2 adds tables): Academy is
     becoming a service of its own (the lab may move to another machine), so its database becomes PostgreSQL in a
     dedicated Docker container `academy-pg` (Docker: already on the host, no pacman while the host is mid-upgrade,
     and outside Incus so `opsforge down` never touches it; 127.0.0.1 only, named volume, small `shared_buffers`,
     ~40-60 MiB idle; `make db` starts it). Relational, with `jsonb` where the data is a document (the `content`
     table's `doc`); no document database (XP, staleness and badges are joins, imports need unique keys and
     transactions) and no ORM: sqlc moves to its `postgresql` engine on `pgx/v5`, goose to the postgres dialect.
     The 11 SQLite migrations are squashed into one baseline (nothing is released); `timestamptz` replaces
     `sqlite.FormatTime`'s fixed-width text, identity columns the integer ids, `ACADEMY_DATABASE_URL` / `--database`
     replaces `--db`. Tests: one shared Postgres (`make db`, a CI service container), each test on its own
     database cloned from a migrated template (`CREATE DATABASE ... TEMPLATE`), so they stay fast and isolated; the
     e2e suite gets its own database. Data: re-run `academy migrate-legacy` from `labs.db` into Postgres (the
     learner's real history); `academy.db` holds mostly the M1 acceptance run (a one-off copy tool only if wanted).
     Later, if Academy runs as several instances: `LISTEN/NOTIFY` for the event stream (the labsession busy lock
     stays per lab).
     **Done when:** `make check`/CI green on Postgres only (no SQLite left), the six-lab run passes again, import
     twice = no write, `migrate-legacy` gives the same XP and badges as on SQLite (800 XP, 6/6 on the live data).
  - **Done when:** each repo's `make check` and CI are green on its own; Academy against tagged OpsForge `v1.0.0`
    (over its API, one sandbox per attempt, none left afterwards) passes the six-lab run on the live lab; OpsForge
    clones and runs with no Go/Node toolchain; Academy clones and runs (against the fake API) with no Python/Incus.

  **Versioning after the split.**
  - *Toolchain contract* (the one boundary between the repos): `contract: <major>` in every document, `minor` and the
    OpsForge release in `api version`. Additive (new command, optional input, output field) = minor; removed,
    renamed or changed meaning = major. Academy reads outputs leniently (its schema copy allows unknown fields; OpsForge's
    own tests stay strict), sends a new optional input only when OpsForge's minor has it, and `/readyz` names the
    minimum OpsForge when it is too old; another major is refused. Breaking changes are free until OpsForge `v1.0.0`.
    Academy updates its pinned contract with `make contract-update V=x.y.z`; a nightly CI job runs its contract tests
    against OpsForge `main`.
  - *HTTP API*: major in the path (`/api/v1`), OpenAPI `info.version` = the Academy release; additive only within v1,
    a break is `/api/v2` next to v1 for one release. The web app ships inside the binary; a tab left open across an
    upgrade sees the server's version header differ from its build and reloads.
  - *Content*: the bundle is internal once the compiler is in Academy; `pack.yml` gets `format: 1` and the compiler
    refuses a newer major.
  - *Database*: goose, forward-only, per repo. *Releases*: semver tags + CHANGELOG.md per repo; OpsForge from
    `v1.0.0`, Academy `v0.x` until M2's parity, then `v1.0.0`; Academy's README states the OpsForge range it supports.

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
| K3s inside LXC may need privileged containers | **Retired** (Phase 4): server, agents, flannel vxlan over the VPN, hostPorts and ingress all run unprivileged with the `k3s-node` profile. |
| `br_netfilter` (loaded by host-setup) is system-wide: bridged traffic on all bridges, incl. Docker's, hits iptables | Accepted trade-off (Docker supports it); told to the user. |
| Docker on host interferes with bridge forwarding | Phase 0 check + `DOCKER-USER` rules. |
| Host kernel updates (Arch) break module availability until reboot | `opsforge up` preflight checks required modules. |
| netem imprecision under host load | Keep WAN rates realistic; preflight warns if host load is high; measure in Phase 1 acceptance. |
| Double encapsulation MTU issues | Explicit MTU math: tunnels 1420, flannel underlay 1420, pods 1370; Phase 3/4 measured large transfers with no fragmentation. |
| Linux-only host requirement | Document; KVM/libvirt or a Linux VM as the path for other OSes. |

## 7. Decisions on previously open questions (2026-10-20)
- **Host OS:** Linux only. The lab depends on host netfilter/tc/kernel modules; other OSes would run it inside a
  Linux VM, which is out of scope. (Resolved; revisit only if another developer needs it.)
- **Default sites:** 3. Three sites give a triangle with an asymmetric link, which is the smallest topology where
  partition and routing behaviour is interesting.
- **CI:** opt-in, as in Phase 7: a node with `role: ci` in `topology.yml` (the `profiles.ci` flag first planned was
  dropped as redundant with the role, like `monitoring`).
