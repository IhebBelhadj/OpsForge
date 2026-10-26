"""Render topology.yml into Incus resources and in-container config (docs/PLAN.md §3).

Pure functions, no Incus calls: `opsforge render` prints the result for inspection.
Also renders the controller side: the SSH client config and the Ansible inventory (Phase 2).

Wiring: each site has a VPC bridge (of-<site>) shared by its gateway and nodes, and a point-to-point
uplink bridge (of-<site>-wan) between its gateway and wan-core. Gateways therefore cannot reach each
other directly: every inter-site packet crosses wan-core, which shapes it. No lab bridge has a host
address, so the host neither routes between VPCs nor leaks into them.
"""

import hashlib
import ipaddress
import json
import math
import re
from dataclasses import dataclass, field

ALPINE_IMAGE = "images:alpine/3.23"
DEBIAN_IMAGE = "images:debian/13"
# Built once by `opsforge up` from a base image; bump an image's suffix when its build script changes.
# Both ship sshd + python3 so Ansible can manage instances from their first boot. SSH host keys are
# per instance (pushed by `up`), and machine-id is regenerated on first boot.
ROUTER_IMAGE = "opsforge-router-4"
# iproute2: BusyBox's `ip` cannot create WireGuard links; bird: OSPF over the site-to-site tunnels
ROUTER_BUILD = """
set -e
timeout 60 sh -c 'until nslookup dl-cdn.alpinelinux.org >/dev/null 2>&1; do sleep 1; done'
apk add --no-cache iproute2 iproute2-tc nftables iperf3 tcpdump openssh-server openssh-sftp-server python3 \\
	wireguard-tools-wg bird
rc-update add local default
rc-update del crond default || true
rm -f /etc/ssh/ssh_host_*
"""
NODE_IMAGE = "opsforge-node-3"
NODE_BUILD = """
set -e
timeout 60 sh -c 'until getent hosts deb.debian.org >/dev/null; do sleep 1; done'
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends openssh-server openssh-sftp-server python3 python3-apt tcpdump
apt-get clean
rm -rf /var/lib/apt/lists/* /var/lib/apt/periodic/update-success-stamp /etc/ssh/ssh_host_*
# Ansible's apt cache_valid_time reads the lists' mtime: mark them stale so the first run refreshes them
touch -d @0 /var/lib/apt/lists
truncate -s 0 /etc/machine-id
"""
# Image pull-through cache (Phase 4): zot, one binary syncing several upstream registries on demand. Its release
# binary links glibc, which Alpine's gcompat provides.
ZOT_VERSION = "v2.1.22"
ZOT_SHA256 = "33b9b5bfe05d6788c076de951a01920477dbd6395c330146e23cc2340073e52e"
CACHE_IMAGE = "opsforge-cache-1"
CACHE_BUILD = f"""
set -e
timeout 60 sh -c 'until nslookup dl-cdn.alpinelinux.org >/dev/null 2>&1; do sleep 1; done'
apk add --no-cache gcompat
wget -qO /usr/local/bin/zot https://github.com/project-zot/zot/releases/download/{ZOT_VERSION}/zot-linux-amd64
echo '{ZOT_SHA256}  /usr/local/bin/zot' | sha256sum -c -
chmod 0755 /usr/local/bin/zot
cat >/etc/init.d/zot <<'EOF'
#!/sbin/openrc-run
command=/usr/local/bin/zot
command_args="serve /etc/zot/config.json"
supervisor=supervise-daemon
output_log=/var/log/zot.log
error_log=/var/log/zot.log
depend() {{ need net; }}
EOF
chmod 0755 /etc/init.d/zot
rc-update add zot default
rc-update del crond default || true
"""
IMAGES = {
    ROUTER_IMAGE: (ALPINE_IMAGE, ROUTER_BUILD, "OpsForge router: Alpine + tc/nftables/WireGuard/BIRD/tcpdump/sshd"),
    NODE_IMAGE: (DEBIAN_IMAGE, NODE_BUILD, "OpsForge node: Debian + sshd/python3/tcpdump"),
    CACHE_IMAGE: (ALPINE_IMAGE, CACHE_BUILD, f"OpsForge image cache: Alpine + zot {ZOT_VERSION}"),
}
PROFILE = "opsforge"
# Added to K3s nodes. Unprivileged (Phase 2.5 spike): nesting for containerd, /dev/kmsg for the kubelet, and the
# modules K3s needs loaded by Incus on the host, since an unprivileged container cannot load them itself.
K3S_PROFILE = "k3s-node"
K3S_PROFILE_CONFIG = {
    "security.nesting": "true",
    "security.privileged": "false",
    "linux.kernel_modules": "overlay,br_netfilter,nf_conntrack,ip_tables,ip6_tables,iptable_nat,iptable_filter,"
    "iptable_mangle,xt_conntrack,xt_comment,xt_mark,xt_multiport,xt_addrtype,xt_MASQUERADE,vxlan",
}
K3S_PROFILE_DEVICES = {"kmsg": {"type": "unix-char", "source": "/dev/kmsg", "path": "/dev/kmsg"}}
K3S_ROLES = ("k3s-server", "k3s-agent")
# Phase 8: K3s nodes without `cluster:` form this one cluster, stretched over their sites. Each cluster's API is
# forwarded to the host on K3S_API_PORT + its index in topology order (`opsforge kubectl --cluster`).
DEFAULT_CLUSTER = "lab"
K3S_API_PORT = 6443
CACHE = "opsforge-cache"
CACHE_PORT = 5000
CACHE_LIMITS = {"limits.cpu": "1", "limits.memory": "256MiB", "limits.memory.swap": "false"}
# Mirrored registries; the cache stores each under its own prefix (docker.io/library/alpine)
CACHE_UPSTREAMS = {
    "docker.io": "https://registry-1.docker.io",
    "ghcr.io": "https://ghcr.io",
    "registry.k8s.io": "https://registry.k8s.io",
    "quay.io": "https://quay.io",
}
WAN_CORE = "wan-core"
ROUTER_LIMITS = {"limits.cpu": "1", "limits.memory": "64MiB", "limits.memory.swap": "false"}
# Gateways idle like wan-core but run Ansible's Python modules, which do not fit in 64 MiB
GATEWAY_LIMITS = ROUTER_LIMITS | {"limits.memory": "128MiB"}
NODE_DEFAULTS = {"cpu": 1, "memory": "512MiB"}
NODE_HOST_OFFSET = 11  # nodes get .11, .12, ... of their VPC; .1 is the gateway
# netem rate for links without one; netem always gets a rate so jitter never reorders packets
UNSHAPED_RATE = "10gbit"
# Never routed from the lab to the real internet: other VPCs, the host's LANs, incusbr0
BOGONS = ("10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16")
HEADER = "# Generated by opsforge from topology.yml. Do not edit: changes are lost on the next `opsforge up`."
# Routers run their config at boot from /etc/local.d; `opsforge up` waits for READY_FLAG
START_LOG = "/var/log/opsforge.log"
READY_FLAG = "/run/opsforge.ready"
# interconnect: vpn. One WireGuard interface per peer gateway (wg-<peer>), each on its own UDP port:
# WG_PORT + the peer's index in topology.yml's sites. 1420 = 1500 - 80 (IPv6-sized overhead, as wg-quick).
WG_PORT = 51820
WG_MTU = 1420
WG_KEEPALIVE = 25  # seconds; keeps handshakes fresh (and NAT/conntrack state alive) on idle tunnels
# routing: ospf. A neighbour is declared dead after OSPF_DEAD seconds without hellos, then traffic reroutes
OSPF_HELLO = 2
OSPF_DEAD = 10
# Phase 5: exporters on every managed host, scraped by the monitoring node across the VPN. Gateways also run a
# blackbox exporter that probes the other sites, so each site pair is measured from its own end.
NODE_EXPORTER_PORT = 9100
BLACKBOX_PORT = 9115
LAB_DOMAIN = "lab"  # each site's zone is <site>.lab, served by its gateway (ansible/roles/dns)
# Geo-aware DNS: every gateway answers this name with the nearest site whose ingress passes its health check
GEO_ZONE = "global"
GEO_NAME = f"www.{GEO_ZONE}.{LAB_DOMAIN}"
# The controller (host) enters the lab like an operator on the internet: through wan-core, which it can
# reach over incusbr0, to the gateways' public IPs. That first hop is wan-core's own traffic: unshaped.
SSH_ENTRY = f"incus exec {WAN_CORE} -- nc %h %p"


def vpc_net(site):
    return f"of-{site}"


def uplink_net(site):
    return f"of-{site}-wan"


def gateway_name(site):
    return f"{site}-gw"


def core_address(topo):
    return next(ipaddress.ip_network(topo["transit"]).hosts())


def cache_public_address(topo):
    """Where the lab reaches the image cache: the transit address after wan-core's, DNATed by wan-core like a CDN
    edge on the internet (reachable from every site, unshaped)."""
    return core_address(topo) + 1


def cache_address(upstream_dns):
    """The cache's own address on incusbr0: the one after incusbr0's (a static DHCP reservation)."""
    return ipaddress.ip_address(upstream_dns) + 1


def node_addresses(topo):
    """Node name -> IPv4 interface in its site's VPC, in topology.yml order."""
    counts, addrs = {}, {}
    for name, node in topo["nodes"].items():
        vpc = ipaddress.ip_network(topo["sites"][node["site"]]["vpc"])
        k = counts.get(node["site"], 0)
        counts[node["site"]] = k + 1
        addrs[name] = ipaddress.ip_interface(f"{vpc.network_address + NODE_HOST_OFFSET + k}/{vpc.prefixlen}")
    return addrs


def gateway_address(site):
    vpc = ipaddress.ip_network(site["vpc"])
    return ipaddress.ip_interface(f"{vpc.network_address + 1}/{vpc.prefixlen}")


def parse_expose(spec):
    """'8080:80/tcp' -> (8080, 80, 'tcp'); private port and protocol default to the public port and tcp."""
    ports, _, proto = spec.partition("/")
    public, _, private = ports.partition(":")
    return int(public), int(private or public), proto or "tcp"


def rate_bps(value):
    num, unit = re.fullmatch(r"(\d+)(kbit|mbit|gbit)", value).groups()
    return int(num) * {"kbit": 10**3, "mbit": 10**6, "gbit": 10**9}[unit]


def start_preamble():
    return ["#!/bin/sh", HEADER, f"exec >{START_LOG} 2>&1", "set -ex", f"rm -f {READY_FLAG}"]


def time_us(value):
    num, unit = re.fullmatch(r"(\d+(?:\.\d+)?)(us|ms|s)", value).groups()
    return float(num) * {"us": 1, "ms": 1e3, "s": 1e6}[unit]


@dataclass
class Instance:
    name: str
    role: str
    site: str | None
    image: str
    config: dict
    nics: dict  # interface name in the container -> Incus network
    address: str | None = None
    files: dict = field(default_factory=dict)  # path -> (content, mode)
    managed: bool = False  # reachable over SSH and in the Ansible inventory
    profiles: list = field(default_factory=lambda: [PROFILE])
    cluster: str | None = None  # K3s nodes: the cluster they belong to


def fingerprint(inst):
    """Digest of what an instance is wired and configured with (address, NICs, pushed files, profiles; not image or
    limits). `up` records it as user.opsforge.spec, so a later `up` knows which instances a topology edit touched."""
    blob = json.dumps([inst.address, inst.nics, inst.files, inst.profiles], sort_keys=True)
    return hashlib.sha256(blob.encode()).hexdigest()[:16]


def tunnels(topo, site):
    """[(peer, interface, listen port, peer's port)] of a site's gateway: a full mesh when interconnect is vpn.
    The site listed first in topology.yml initiates each tunnel, see render_wg_conf."""
    if topo.get("interconnect", "none") != "vpn":
        return []
    names = list(topo["sites"])
    me = names.index(site)
    return [(peer, f"wg-{peer}", WG_PORT + i, WG_PORT + me) for i, peer in enumerate(names) if peer != site]


def routing(topo):
    """How gateways route between VPCs over the mesh: ospf (default) reroutes around a dead link, static does not."""
    return topo.get("routing", "ospf")


def ospf_cost(topo, site, peer):
    """A tunnel's OSPF cost is its link's configured RTT in ms, so the fastest path wins; 1 between sites without a
    link (unshaped). From topology.yml: live `opsforge link` edits do not change it."""
    link = topo["links"].get(f"{site}-{peer}") or topo["links"].get(f"{peer}-{site}")
    return max(1, round(time_us(link["rtt"]) / 1000)) if link else 1


def render_bird_conf(topo, site):
    ifaces = "\n".join(
        f'\t\tinterface "{dev}" {{ type ptp; cost {ospf_cost(topo, site, peer)}; hello {OSPF_HELLO}; dead {OSPF_DEAD}; }};'
        for peer, dev, _, _ in tunnels(topo, site)
    )
    return f"""{HEADER}
# {gateway_name(site)}: OSPF over the WireGuard mesh; announces VPC {topo['sites'][site]['vpc']}, learns the others.
router id {topo['sites'][site]['public_ip']};
log syslog all;

protocol device {{
}}

protocol kernel {{
	ipv4 {{
		import none;
		# Learned routes only: the VPC's own route is the kernel's already
		export where source = RTS_OSPF && ifname != "vpc";
	}};
}}

protocol ospf v2 {{
	ipv4 {{
		import all;
		export none;
	}};
	area 0 {{
		interface "vpc" {{ stub yes; }};
{ifaces}
	}};
}}
"""


def render_wg_conf(private_key, port, peer_public_key, endpoint=None):
    """endpoint: set on the initiating side only. The responder learns it from the first authenticated packet. When
    both sides initiate at the same moment (gateways booting together), each one's response to the other discards its
    own pending handshake, and the pair retries in lockstep every 5 s: measured up to 37 s to connect."""
    peer = f"Endpoint = {endpoint}\n" if endpoint else "# No Endpoint: this side responds, the peer initiates\n"
    return f"""{HEADER}
[Interface]
PrivateKey = {private_key}
ListenPort = {port}

[Peer]
PublicKey = {peer_public_key}
{peer}# Any destination: routing (not WireGuard's cryptokey routing) decides which tunnel a packet takes
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = {WG_KEEPALIVE}
"""


def ssh_files(authorized_key, host_key, host_pub):
    """Files that make an instance reachable over SSH from its first boot, with a host key known in advance."""
    return {
        "/root/.ssh/authorized_keys": (authorized_key, "0600"),
        "/etc/ssh/ssh_host_ed25519_key": (host_key, "0600"),
        "/etc/ssh/ssh_host_ed25519_key.pub": (host_pub, "0644"),
        "/etc/ssh/sshd_config.d/00-opsforge-hostkey.conf": (f"{HEADER}\nHostKey /etc/ssh/ssh_host_ed25519_key\n", "0644"),
    }


def link_state(topo):
    """What wan-core needs to shape traffic; stored in the container so `opsforge link` can change it live."""
    return {
        "sites": {name: site["public_ip"] for name, site in topo["sites"].items()},
        "links": {name: dict(link) for name, link in topo["links"].items()},
    }


def netem_args(link):
    # rtt is round-trip: half each way. jitter is the RTT standard deviation (what ping reports as mdev),
    # so each direction gets jitter/sqrt(2).
    args = [f"delay {round(time_us(link['rtt']) / 2)}us"]
    jitter = time_us(link.get("jitter", "0ms"))
    if jitter:
        args.append(f"{round(jitter / math.sqrt(2))}us distribution normal")
    if float(link.get("loss", "0%").rstrip("%")):
        args.append(f"loss {link['loss']}")
    args.append(f"rate {link.get('rate') or UNSHAPED_RATE}")
    return " ".join(args)


def render_links_sh(state):
    """tc script for wan-core: on the interface facing each site, one HTB class + netem leaf per peer site,
    classified by the peer gateway's (source) public IP. Unclassified traffic (internet, wan-core) is unshaped."""
    lines = ["#!/bin/sh", HEADER, "# Applies WAN link conditions; safe to re-run.", "set -e"]
    links = sorted(state["links"].items())
    for site in state["sites"]:
        dev = f"to-{site}"
        lines += [
            "",
            f"tc qdisc del dev {dev} root 2>/dev/null || true",
            f"tc qdisc add dev {dev} root handle 1: htb default 0",
        ]
        for i, (name, link) in enumerate(links, start=1):
            a, b = name.split("-")
            if site not in (a, b):
                continue
            peer = b if site == a else a
            lines += [
                f"# {name}: {peer} -> {site}",
                f"tc class add dev {dev} parent 1: classid 1:{i:x} htb rate {UNSHAPED_RATE} quantum 60000",
                f"tc qdisc add dev {dev} parent 1:{i:x} handle {i + 1:x}: netem {netem_args(link)}",
                f"tc filter add dev {dev} parent 1: protocol ip prio 1 u32 "
                f"match ip src {state['sites'][peer]}/32 flowid 1:{i:x}",
            ]
    # mtu: a path-MTU black hole, as a misconfigured link or a firewall eating ICMP "fragmentation needed" makes one.
    # The tunnels' UDP between the two gateways is dropped above that size, so small packets pass and big ones vanish.
    lines += ["", "nft delete table inet opsforge_mtu 2>/dev/null || true"]
    holes = [(name, link["mtu"]) for name, link in links if "mtu" in link]
    if holes:
        lines += [
            "nft add table inet opsforge_mtu",
            "nft add chain inet opsforge_mtu forward '{ type filter hook forward priority filter - 10; }'",
        ]
        for name, mtu in holes:
            a, b = (state["sites"][s] for s in name.split("-"))
            for src, dst in ((a, b), (b, a)):
                lines.append(f"nft add rule inet opsforge_mtu forward ip saddr {src} ip daddr {dst} ip length gt {mtu} drop")
    return "\n".join(lines) + "\n"


def link_files(state):
    """wan-core's link state and the tc script applying it; `opsforge link` rewrites both, so they survive restarts."""
    return {
        "/etc/opsforge/links.sh": (render_links_sh(state), "0755"),
        "/etc/opsforge/links.json": (json.dumps(state, indent=2) + "\n", "0644"),
    }


def render_wan_core(topo, upstream_dns):
    core = core_address(topo)
    sites = topo["sites"]
    start = [*start_preamble(), "sysctl -qw net.ipv4.ip_forward=1"]
    for name, site in sites.items():
        dev = f"to-{name}"
        start += [
            f"ip link set {dev} up",
            f"ip addr replace {core}/32 dev {dev}",
            f"ip route replace {site['public_ip']}/32 dev {dev}",
        ]
    start += ["nft -f /etc/opsforge/firewall.nft", "/etc/opsforge/links.sh", f"touch {READY_FLAG}"]
    cache_dnat = "" if not uses_cache(topo) else (
        f"\t\t# image cache ({CACHE}, on incusbr0): a CDN edge on the internet\n"
        f"\t\tiifname \"to-*\" ip daddr {cache_public_address(topo)} tcp dport {CACHE_PORT} "
        f"dnat to {cache_address(upstream_dns)}\n"
    )

    firewall = f"""{HEADER}
# wan-core: the simulated internet. Routes between site gateways, gives them real internet access
# through incusbr0, and answers DNS on {core} by forwarding to the incusbr0 resolver.
flush ruleset

table inet opsforge {{
	set bogons {{
		type ipv4_addr; flags interval
		elements = {{ {", ".join(BOGONS)} }}
	}}

	chain input {{
		type filter hook input priority filter; policy drop
		ct state established,related accept
		iif lo accept
		icmp type echo-request accept
	}}

	chain forward {{
		type filter hook forward priority filter; policy drop
		ct state established,related accept
		ct status dnat accept
		iifname "to-*" oifname "to-*" accept
		# private destinations (other VPCs, the host's LANs) are not routed, like on the real internet
		iifname "to-*" oifname "eth0" ip daddr != @bogons accept
	}}
}}

table ip nat {{
	chain prerouting {{
		type nat hook prerouting priority dstnat
		iifname "to-*" ip daddr {core} meta l4proto {{ tcp, udp }} th dport 53 dnat to {upstream_dns}
{cache_dnat}	}}

	chain postrouting {{
		type nat hook postrouting priority srcnat
		oifname "eth0" masquerade
	}}
}}
"""
    nics = {"eth0": "incusbr0"} | {f"to-{name}": uplink_net(name) for name in sites}
    files = {
        "/etc/local.d/opsforge.start": ("\n".join(start) + "\n", "0755"),
        "/etc/opsforge/firewall.nft": (firewall, "0644"),
    } | link_files(link_state(topo))
    return Instance(WAN_CORE, "wan-core", None, ROUTER_IMAGE, dict(ROUTER_LIMITS), nics, str(core), files)


def monitoring_node(topo):
    return next((n for n, node in topo["nodes"].items() if node["role"] == "monitoring"), None)


def k3s_clusters(topo):
    """K3s cluster name -> {"server", "nodes", "sites", "api_port"} in topology.yml order. Nodes without `cluster:`
    share DEFAULT_CLUSTER: one cluster stretched over their sites (Phase 4); one per site is multi-cluster (Phase 8)."""
    clusters = {}
    for name, node in topo["nodes"].items():
        if node["role"] not in K3S_ROLES:
            continue
        c = clusters.setdefault(node.get("cluster", DEFAULT_CLUSTER), {"server": None, "nodes": [], "sites": []})
        c["nodes"].append(name)
        if node["site"] not in c["sites"]:
            c["sites"].append(node["site"])
        if node["role"] == "k3s-server":
            c["server"] = name
    for i, c in enumerate(clusters.values()):
        c["api_port"] = K3S_API_PORT + i
    return clusters


def cluster_links(topo):
    """[(cluster, peer, cost)]: with several clusters, each one links to every cluster listed before it (Skupper links
    carry traffic both ways; one side must dial). Cost = the smallest RTT in ms between their sites from
    topology.yml, like the gateways' OSPF costs, so a service is served from the nearest cluster that has it."""
    clusters = k3s_clusters(topo)
    names = list(clusters)
    return [
        (c, peer, min(ospf_cost(topo, a, b) for a in clusters[c]["sites"] for b in clusters[peer]["sites"]))
        for i, c in enumerate(names) for peer in names[:i]
    ]


def ingress_node(topo, site):
    """(node, private port) behind the site's public HTTP port (expose 80/tcp), or None."""
    for name, node in topo["nodes"].items():
        if node["site"] == site:
            for spec in node.get("expose", []):
                public, private, proto = parse_expose(spec)
                if (public, proto) == (80, "tcp"):
                    return name, private
    return None


def geo_targets(topo, site):
    """[(site, address, url)] in the order `site`'s gateway prefers them for GEO_NAME: its own ingress first, then
    the other sites' by link RTT (ospf_cost; ties in topology order). Split horizon: the own site's answer is the
    ingress node's VPC address (gateways do no hairpin NAT), the others' their public IP, reached across the WAN."""
    addrs = node_addresses(topo)
    targets = []
    for peer in sorted(topo["sites"], key=lambda p: 0 if p == site else ospf_cost(topo, site, p)):
        ingress = ingress_node(topo, peer)
        if ingress is None:
            continue
        if peer == site:
            ip = str(addrs[ingress[0]].ip)
            targets.append((peer, ip, f"http://{ip}:{ingress[1]}/"))
        else:
            ip = topo["sites"][peer]["public_ip"]
            targets.append((peer, ip, f"http://{ip}/"))
    return targets


def render_gateway(topo, name, addrs, wg_keys):
    site = topo["sites"][name]
    core = core_address(topo)
    lan = gateway_address(site)
    mesh = tunnels(topo, name)
    wg_files, wg_start, wg_input = {}, [], []
    for peer, dev, port, peer_port in mesh:
        other = topo["sites"][peer]
        initiator = list(topo["sites"]).index(name) < list(topo["sites"]).index(peer)
        endpoint = f"{other['public_ip']}:{peer_port}" if initiator else None
        conf = render_wg_conf(wg_keys[name][0], port, wg_keys[peer][1], endpoint)
        wg_files[f"/etc/opsforge/wg/{dev}.conf"] = (conf, "0600")
        wg_start += [
            f"ip link del {dev} 2>/dev/null || true",
            f"ip link add {dev} type wireguard",
            f"wg setconf {dev} /etc/opsforge/wg/{dev}.conf",
            f"ip link set {dev} mtu {WG_MTU} up",
            f"ip addr replace {lan.ip} peer {gateway_address(other).ip} dev {dev}",
            *([f"ip route replace {other['vpc']} dev {dev}"] if routing(topo) == "static" else []),
        ]
        wg_input.append(f'\t\tiifname "wan" ip saddr {other["public_ip"]} udp dport {port} accept  # WireGuard from {peer}')
    if mesh and (mon := monitoring_node(topo)):
        wg_input.append(f'\t\tiifname "wg-*" ip saddr {addrs[mon].ip} tcp dport {{ {NODE_EXPORTER_PORT}, {BLACKBOX_PORT} }} '
                        f'accept  # metrics, scraped by {mon}')
    if mesh and routing(topo) == "ospf":
        wg_files["/etc/bird.conf"] = (render_bird_conf(topo, name), "0640")
        wg_start.append("rc-service bird start")
        wg_input.append('\t\tiifname "wg-*" ip protocol 89 accept  # OSPF')
    start = [
        *start_preamble(),
        "sysctl -qw net.ipv4.ip_forward=1",
        "ip link set wan up",
        "ip link set vpc up",
        f"ip addr replace {site['public_ip']}/32 dev wan",
        f"ip route replace {core}/32 dev wan",
        f"ip route replace default via {core} dev wan onlink",
        f"ip addr replace {lan} dev vpc",
        "nft -f /etc/opsforge/firewall.nft",
        *wg_start,
        "rc-service sshd start",
        f"touch {READY_FLAG}",
    ]
    peers = ", ".join(s["public_ip"] for n, s in topo["sites"].items() if n != name)
    # Site-to-site traffic is trusted like peered VPCs; wg -> wg is transit when a direct tunnel is down
    vpn_forward = "" if not mesh else """		iifname "vpc" oifname "wg-*" accept
		iifname "wg-*" oifname "vpc" accept
		iifname "wg-*" oifname "wg-*" accept
	}

	# TCP MSS clamped to the tunnel MTU, so TCP across the VPN never depends on path-MTU discovery
	chain mss {
		type filter hook forward priority mangle; policy accept
		oifname "wg-*" tcp flags & (syn | rst) == syn tcp option maxseg size set rt mtu"""
    forwards = []
    for node_name, node in topo["nodes"].items():
        if node["site"] != name:
            continue
        for spec in node.get("expose", []):
            public, private, proto = parse_expose(spec)
            ip = addrs[node_name].ip
            forwards.append(f'\t\tiifname "wan" {proto} dport {public} dnat to {ip}:{private}  # {node_name}')

    firewall = f"""{HEADER}
# {gateway_name(name)}: site {name} VPC {site['vpc']} behind public IP {site['public_ip']}.
# SNAT outbound, default-deny inbound except ping and the port forwards declared with `expose`.
flush ruleset

table inet opsforge {{
	chain input {{
		type filter hook input priority filter; policy drop
		ct state established,related accept
		iif lo accept
		iifname "vpc" accept
		icmp type echo-request accept
		# management SSH (key-only), and DNS for this site's zone from the other gateways
		iifname "wan" tcp dport 22 accept
		iifname "wan" ip saddr {{ {peers} }} meta l4proto {{ tcp, udp }} th dport 53 accept
{chr(10).join(wg_input)}
		jump verify
	}}

	# Empty; `opsforge verify` opens temporary test ports here
	chain verify {{
	}}

	chain forward {{
		type filter hook forward priority filter; policy drop
		ct state established,related accept
		ct status dnat accept
		iifname "vpc" oifname "wan" accept
{vpn_forward}
	}}
}}

table ip nat {{
	chain prerouting {{
		type nat hook prerouting priority dstnat
{chr(10).join(forwards)}
	}}

	chain postrouting {{
		type nat hook postrouting priority srcnat
		oifname "wan" snat to {site['public_ip']}
	}}
}}
"""
    files = {
        "/etc/local.d/opsforge.start": ("\n".join(start) + "\n", "0755"),
        "/etc/opsforge/firewall.nft": (firewall, "0644"),
        "/etc/resolv.conf": (f"nameserver {core}\n", "0644"),
        # Bastion for the site: ProxyJump to the nodes is a local (stdio) forward, which Alpine disables
        "/etc/ssh/sshd_config.d/00-opsforge-bastion.conf": (f"{HEADER}\nAllowTcpForwarding local\n", "0644"),
    } | wg_files
    nics = {"wan": uplink_net(name), "vpc": vpc_net(name)}
    return Instance(
        gateway_name(name), "gateway", name, ROUTER_IMAGE, dict(GATEWAY_LIMITS), nics, site["public_ip"], files, True
    )


def render_node(topo, name, addr):
    node = NODE_DEFAULTS | topo["nodes"][name]
    site = topo["sites"][node["site"]]
    # 10- sorts before the image's eth0.network, so systemd-networkd uses this one
    network = f"""{HEADER}
[Match]
Name=eth0

[Network]
Address={addr}
Gateway={gateway_address(site).ip}
DNS={core_address(topo)}
"""
    config = {"limits.cpu": str(node["cpu"]), "limits.memory": node["memory"], "limits.memory.swap": "false"}
    files = {"/etc/systemd/network/10-opsforge.network": (network, "0644")}
    k3s = node["role"] in K3S_ROLES
    profiles = [PROFILE, K3S_PROFILE] if k3s else [PROFILE]
    return Instance(
        name, node["role"], node["site"], NODE_IMAGE, config, {"eth0": vpc_net(node["site"])}, str(addr.ip), files, True,
        profiles, node.get("cluster", DEFAULT_CLUSTER) if k3s else None,
    )


def uses_cache(topo):
    """The image cache runs when the lab has K3s nodes to pull images."""
    return any(node["role"] in K3S_ROLES for node in topo["nodes"].values())


def render_zot_config():
    """zot syncs an image from its upstream on first request (onDemand), then serves it locally; each upstream lives
    under its own prefix (docker.io/library/alpine), which K3s' registries.yaml adds with a rewrite."""
    registries = [
        {"urls": [url], "onDemand": True, "tlsVerify": True,
         "content": [{"prefix": "**", "destination": f"/{name}", "stripPrefix": True}]}
        for name, url in CACHE_UPSTREAMS.items()
    ]
    config = {
        "distSpecVersion": "1.1.1",
        "storage": {"rootDirectory": "/var/lib/zot", "gc": True, "dedupe": False},
        # docker2s2: also store Docker schema 2 manifests (most of docker.io), not only OCI ones. Without it zot
        # answers 503 for those and containerd silently falls back to the upstream registry.
        "http": {"address": "0.0.0.0", "port": str(CACHE_PORT), "compat": ["docker2s2"]},
        "log": {"level": "info"},
        "extensions": {"sync": {"enable": True, "registries": registries}},
    }
    return json.dumps(config, indent=2) + "\n"


def render_cache(upstream_dns):
    """The image cache lives outside the lab (on incusbr0, not tagged as a lab instance), so `down` keeps it and its
    images; `down --purge` removes it. Its NIC has a static address, which wan-core's DNAT points to."""
    files = {"/etc/zot/config.json": (render_zot_config(), "0644")}
    return Instance(CACHE, "cache", None, CACHE_IMAGE, dict(CACHE_LIMITS), {"eth0": "incusbr0"},
                    str(cache_address(upstream_dns)), files, profiles=[])


def placeholder_wg_keys(topo):
    return {site: (f"<{site} private key>", f"<{site} public key>") for site in topo["sites"]}


def plan(topo, upstream_dns, wg_keys=None):
    """Returns (Incus networks, instances in start order) for a validated topology. wg_keys: {site: (private,
    public)} WireGuard keys of the gateways (base64); placeholders if omitted, for display and tests."""
    wg_keys = wg_keys or placeholder_wg_keys(topo)
    networks = [net for site in topo["sites"] for net in (vpc_net(site), uplink_net(site))]
    addrs = node_addresses(topo)
    instances = [render_wan_core(topo, upstream_dns)]
    instances += [render_gateway(topo, site, addrs, wg_keys) for site in topo["sites"]]
    instances += [render_node(topo, name, addrs[name]) for name in topo["nodes"]]
    return networks, instances


def render_ssh_config(topo, identity, known_hosts, control_dir):
    """OpenSSH client config: gateways through wan-core to their public IP, nodes via ProxyJump through their
    site's gateway, as when managing hosts in a remote VPC through a bastion."""
    hosts = {gateway_name(name): (site["public_ip"], f"ProxyCommand {SSH_ENTRY}") for name, site in topo["sites"].items()}
    for name, addr in node_addresses(topo).items():
        hosts[name] = (addr.ip, f"ProxyJump {gateway_name(topo['nodes'][name]['site'])}")
    lines = [HEADER]
    for name, (ip, via) in hosts.items():
        # Host keys are generated by `opsforge up` and known by name before first contact
        lines += ["", f"Host {name}", f"\tHostName {ip}", f"\tHostKeyAlias {name}", f"\t{via}"]
    lines += [
        "",
        "Host *",
        "\tUser root",
        f"\tIdentityFile {identity}",
        "\tIdentitiesOnly yes",
        f"\tUserKnownHostsFile {known_hosts}",
        "\tStrictHostKeyChecking yes",
        "\tControlMaster auto",
        f"\tControlPath {control_dir}/cm-%n",  # the alias: %C (a 40-char hash) overflows the 108-byte socket path
        "\tControlPersist 60s",
    ]
    return "\n".join(lines) + "\n"


def render_inventory(topo, ssh_config, state_dir):
    """Ansible inventory: groups `gateways` and `nodes` (children of `lab`), `site_<site>` and one group per node role."""
    addrs = node_addresses(topo)
    gateways = {
        gateway_name(name): {
            "lab_site": name,
            "lab_address": str(gateway_address(site).ip),
            "lab_public_ip": site["public_ip"],
            "lab_geo": [{"site": s, "address": ip, "url": url} for s, ip, url in geo_targets(topo, name)],
        }
        for name, site in topo["sites"].items()
    }
    nodes = {
        name: {"lab_site": node["site"], "lab_address": str(addrs[name].ip), "lab_role": node["role"]}
        | ({"lab_cluster": node.get("cluster", DEFAULT_CLUSTER)} if node["role"] in K3S_ROLES else {})
        for name, node in topo["nodes"].items()
    }
    links = cluster_links(topo)
    clusters = {
        name: c | {"links": {peer: cost for src, peer, cost in links if src == name}}
        for name, c in k3s_clusters(topo).items()
    }
    groups = {
        "lab": {
            "vars": {
                "ansible_user": "root",
                "ansible_ssh_common_args": f"-F {ssh_config}",
                "ansible_python_interpreter": "/usr/bin/python3",
                "lab_domain": LAB_DOMAIN,
                "lab_geo_name": GEO_NAME,
                "lab_upstream_dns": str(core_address(topo)),
                "lab_state_dir": str(state_dir),
                # Smallest MTU on the path between VPCs: the WireGuard tunnels' (flannel's VXLAN goes inside them)
                "lab_path_mtu": WG_MTU if topo.get("interconnect") == "vpn" else 1500,
                "lab_registry_cache": f"{cache_public_address(topo)}:{CACHE_PORT}",
                "lab_registry_upstreams": list(CACHE_UPSTREAMS),
                # K3s clusters (Phase 8): server, nodes, sites, the host's API port, Skupper links to earlier clusters
                "lab_clusters": clusters,
                "lab_sites": {
                    name: {
                        "vpc": site["vpc"],
                        "public_ip": site["public_ip"],
                        "gateway": str(gateway_address(site).ip),
                        # A node exposes HTTP on the public IP: the monitoring node probes it from the other sites
                        "ingress": ingress_node(topo, name) is not None,
                    }
                    for name, site in topo["sites"].items()
                },
            },
            "children": {"gateways": {"hosts": gateways}, "nodes": {"hosts": nodes}},
        }
    }
    for name in topo["sites"]:
        members = [gateway_name(name)] + [n for n, node in topo["nodes"].items() if node["site"] == name]
        groups[f"site_{name}"] = {"hosts": dict.fromkeys(members)}
    for name, node in topo["nodes"].items():
        groups.setdefault(node["role"].replace("-", "_"), {"hosts": {}})["hosts"][name] = None
    return {"all": {"children": groups}}
