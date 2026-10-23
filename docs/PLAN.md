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
| mon | VictoriaMetrics + Grafana, own node in site b (survives partitioning site a) | 512 MiB | ~200–300 MB (measured 326 MiB) |
| b-w1, c-w1 | K3s agent + Traefik | 768 MiB each | ~250–400 MB each (measured ~172 MiB) |
| ci (optional) | Gitea + Woodpecker + registry | 1 GiB | ~500–800 MB |
| opsforge-cache | zot pull-through registry (outside the lab, Phase 4) | 256 MiB | ~30–60 MB (measured 55 MiB) |
| **Total (no CI)** | | | **≈ 1.5–2.1 GB (measured 1.6 GiB, Phase 5)** |


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

### Phase 7: Lightweight CI/CD (optional profile `ci: true`)
- [ ] Gitea (+ Actions) or Woodpecker, and `registry:2`, on a `ci` node in site a.
- [ ] Flux on the cluster pulls manifests from Gitea (GitOps across the WAN); replaces the webhook stub.
- [ ] A small reference workload (web app + database) deployed through the pipeline.

**Done when:** a push to Gitea builds an image, pushes to the registry, and Flux rolls it out across sites.

### Phase 8: Multi-cluster (realistic multi-cloud)
- [ ] Design note: reserve `cluster:` as an optional per-node field in the schema from Phase 4, defaulting to
      one cluster, so Phases 4-5 (labels, scrape configs, kubeconfig paths) are keyed by cluster, not global.
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
- **CI:** opt-in (`profiles.ci`), as in Phase 7.
