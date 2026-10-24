"""render.py is pure (topology in, plan out), so it is tested without Incus."""

import json

import pytest

import render
from conftest import cli


def test_parse_expose_defaults():
    assert render.parse_expose("8080") == (8080, 8080, "tcp")
    assert render.parse_expose("8080:80") == (8080, 80, "tcp")
    assert render.parse_expose("53:53/udp") == (53, 53, "udp")


def test_units():
    assert render.rate_bps("100mbit") == 100_000_000
    assert render.rate_bps("1gbit") == 10**9
    assert render.time_us("40ms") == 40_000
    assert render.time_us("1.5s") == 1_500_000


def test_node_addresses_follow_topology_order(topo):
    topo["nodes"].pop("ci", None)  # optional (Phase 7): would take .12
    topo["nodes"]["a-w2"] = {"site": "a", "role": "k3s-agent"}
    addrs = render.node_addresses(topo)
    assert str(addrs["a-cp1"]) == "10.10.0.11/24"
    assert str(addrs["a-w2"]) == "10.10.0.12/24"
    assert str(addrs["b-w1"]) == "10.20.0.11/24"


def test_gateway_is_vpc_dot_one(topo):
    assert str(render.gateway_address(topo["sites"]["c"])) == "10.30.0.1/24"


def test_netem_halves_rtt_and_scales_jitter():
    out = render.netem_args({"rtt": "40ms", "jitter": "5ms", "loss": "0.1%", "rate": "100mbit"})
    assert "delay 20000us" in out
    assert "3536us distribution normal" in out  # 5ms / sqrt(2)
    assert "loss 0.1%" in out
    assert out.endswith("rate 100mbit")


def test_netem_always_has_a_rate_and_omits_zero_loss():
    out = render.netem_args({"rtt": "10ms", "loss": "0%"})
    assert "loss" not in out and "distribution" not in out
    assert out.endswith(f"rate {render.UNSHAPED_RATE}")


def test_link_state_round_trips_through_json(topo):
    state = render.link_state(topo)
    assert json.loads(json.dumps(state)) == state
    assert state["sites"]["a"] == "203.0.113.10"
    assert set(state["links"]) == {"a-b", "a-c", "b-c"}


def test_links_script_has_a_class_per_directed_pair(topo):
    script = render.render_links_sh(render.link_state(topo))
    assert script.startswith("#!/bin/sh")
    assert script.count("netem") >= 6  # 3 links x 2 directions


@pytest.fixture
def lab(topo):
    return render.plan(topo, "10.99.0.1")


def test_plan_has_two_networks_per_site(topo, lab):
    networks, _ = lab
    assert len(networks) == 2 * len(topo["sites"])
    assert all(n.startswith("of-") for n in networks)


def test_plan_interface_names_fit_the_kernel_limit(lab):
    networks, _ = lab
    assert all(len(n) <= 15 for n in networks)


def test_plan_instances(topo, lab):
    _, instances = lab
    names = [i.name for i in instances]
    assert names[0] == render.WAN_CORE
    assert set(names) == {render.WAN_CORE, *map(render.gateway_name, topo["sites"]), *topo["nodes"]}
    assert len(names) == len(set(names))


def test_only_nodes_and_gateways_are_managed(lab):
    _, instances = lab
    assert {i.name: i.managed for i in instances}[render.WAN_CORE] is False
    assert all(i.managed for i in instances if i.name != render.WAN_CORE)


def test_nodes_never_attach_to_the_default_bridge(lab):
    _, instances = lab
    for inst in instances:
        assert "incusbr0" not in inst.nics.values() or inst.name == render.WAN_CORE


def test_memory_limits(lab):
    _, instances = lab
    limits = {i.name: i.config["limits.memory"] for i in instances}
    assert limits[render.WAN_CORE] == "64MiB"
    assert limits["a-gw"] == "128MiB"
    assert limits["a-cp1"] == "1536MiB"


def test_no_instance_enables_swap(lab):
    _, instances = lab
    assert all(i.config.get("limits.memory.swap") == "false" for i in instances)


def test_ssh_config_and_inventory_cover_every_managed_host(topo, lab, tmp_path):
    _, instances = lab
    managed = {i.name for i in instances if i.managed}
    config = render.render_ssh_config(topo, tmp_path / "id", tmp_path / "kh", tmp_path / "ctl")
    inventory = render.render_inventory(topo, tmp_path / "ssh_config", tmp_path)
    assert all(f"Host {name}" in config for name in managed)
    assert all(name in json.dumps(inventory, default=str) for name in managed)


def test_render_is_deterministic(topo):
    first = render.plan(topo, "10.99.0.1")
    second = render.plan(topo, "10.99.0.1")
    assert first == second


def fingerprints(topo):
    return {inst.name: render.fingerprint(inst) for inst in render.plan(topo, "10.99.0.1")[1]}


def test_fingerprint_ignores_limits(topo):
    before = fingerprints(topo)
    topo["nodes"]["b-w1"]["memory"] = "1GiB"
    assert fingerprints(topo) == before


def test_adding_a_site_touches_wan_core_and_gateways_only(topo):
    before = fingerprints(topo)
    topo["sites"]["d"] = {"vpc": "10.40.0.0/24", "public_ip": "203.0.113.40"}
    topo["links"]["a-d"] = {"rtt": "10ms"}
    after = fingerprints(topo)
    changed = {name for name in before if after[name] != before[name]}
    # gateways change because their firewall lists the other sites' public IPs
    assert changed == {"wan-core", "a-gw", "b-gw", "c-gw"}
    assert "d-gw" in after


def test_moving_a_node_changes_only_that_node(topo):
    del topo["nodes"]["mon"]  # moving c-w1 into b ahead of mon would renumber mon (and the gateways' rules for it)
    topo["nodes"]["c-w1"].pop("expose", None)
    before = fingerprints(topo)
    topo["nodes"]["c-w1"]["site"] = "b"
    after = fingerprints(topo)
    assert {name for name in before if after[name] != before[name]} == {"c-w1"}


def test_moving_an_exposed_node_also_changes_both_gateways(topo):
    del topo["nodes"]["mon"]
    topo["nodes"]["c-w1"]["expose"] = ["80"]
    before = fingerprints(topo)
    topo["nodes"]["c-w1"]["site"] = "b"
    after = fingerprints(topo)
    assert {name for name in before if after[name] != before[name]} == {"c-w1", "b-gw", "c-gw"}


def test_vpn_mesh_ports_pair_up(topo):
    ends = {(site, peer): (port, peer_port) for site in topo["sites"] for peer, _, port, peer_port in render.tunnels(topo, site)}
    assert len(ends) == len(topo["sites"]) * (len(topo["sites"]) - 1)
    for (site, peer), (port, peer_port) in ends.items():
        assert ends[(peer, site)] == (peer_port, port)  # each side sends to the port the other listens on
    ports = [port for (site, _), (port, _) in ends.items() if site == "a"]
    assert len(set(ports)) == len(ports)


def test_gateway_tunnels_use_the_peers_keys(topo):
    keys = {s: (f"priv-{s}", f"pub-{s}") for s in topo["sites"]}
    gw = next(i for i in render.plan(topo, "10.99.0.1", keys)[1] if i.name == "a-gw")
    conf = gw.files["/etc/opsforge/wg/wg-b.conf"]
    assert conf[1] == "0600"
    assert "PrivateKey = priv-a" in conf[0] and "PublicKey = pub-b" in conf[0]
    assert f"Endpoint = {topo['sites']['b']['public_ip']}:" in conf[0]
    b_gw = next(i for i in render.plan(topo, "10.99.0.1", keys)[1] if i.name == "b-gw")
    assert "\nEndpoint" not in b_gw.files["/etc/opsforge/wg/wg-a.conf"][0]  # a initiates, b only responds
    start = gw.files["/etc/local.d/opsforge.start"][0]
    assert f"mtu {render.WG_MTU}" in start
    assert "maxseg size set rt mtu" in gw.files["/etc/opsforge/firewall.nft"][0]


def test_ospf_costs_follow_link_rtt_and_static_routes_replace_it(topo):
    topo["routing"] = "ospf"
    gw = next(i for i in render.plan(topo, "10.99.0.1")[1] if i.name == "a-gw")
    bird = gw.files["/etc/bird.conf"][0]
    assert 'interface "wg-b" { type ptp; cost 40;' in bird and 'interface "wg-c" { type ptp; cost 120;' in bird
    assert "rc-service bird start" in gw.files["/etc/local.d/opsforge.start"][0]
    assert "ip route replace" not in gw.files["/etc/local.d/opsforge.start"][0].split("wg-b", 1)[1]
    topo["routing"] = "static"
    gw = next(i for i in render.plan(topo, "10.99.0.1")[1] if i.name == "a-gw")
    assert "/etc/bird.conf" not in gw.files
    assert f"ip route replace {topo['sites']['b']['vpc']} dev wg-b" in gw.files["/etc/local.d/opsforge.start"][0]


def test_interconnect_none_has_no_tunnels(topo):
    topo["interconnect"] = "none"
    for inst in render.plan(topo, "10.99.0.1")[1]:
        assert not any("/wg/" in path for path in inst.files)
        assert "wg-" not in inst.files.get("/etc/opsforge/firewall.nft", ("",))[0]


def test_k3s_nodes_get_the_k3s_profile(topo):
    topo["nodes"]["a-misc"] = {"site": "a", "role": "plain"}
    profiles = {i.name: i.profiles for i in render.plan(topo, "10.99.0.1")[1]}
    assert profiles["a-cp1"] == profiles["b-w1"] == [render.PROFILE, render.K3S_PROFILE]
    assert profiles["a-misc"] == profiles["a-gw"] == profiles[render.WAN_CORE] == [render.PROFILE]
    assert "vxlan" in render.K3S_PROFILE_CONFIG["linux.kernel_modules"].split(",")
    assert render.K3S_PROFILE_CONFIG["security.privileged"] == "false"


def test_wan_core_dnats_the_cache_only_with_k3s_nodes(topo):
    wan_core = render.plan(topo, "10.99.0.1")[1][0]
    firewall = wan_core.files["/etc/opsforge/firewall.nft"][0]
    assert f"ip daddr 203.0.113.2 tcp dport {render.CACHE_PORT} dnat to 10.99.0.2" in firewall
    for node in topo["nodes"].values():
        node["role"] = "plain"
    wan_core = render.plan(topo, "10.99.0.1")[1][0]
    assert "203.0.113.2" not in wan_core.files["/etc/opsforge/firewall.nft"][0]
    assert not render.uses_cache(topo)


def test_cache_mirrors_each_upstream_under_its_prefix():
    cache = render.render_cache("10.99.0.1")
    assert cache.address == "10.99.0.2" and cache.nics == {"eth0": "incusbr0"} and cache.profiles == []
    config = json.loads(cache.files["/etc/zot/config.json"][0])
    assert "docker2s2" in config["http"]["compat"]  # else Docker-format images bypass the cache
    registries = config["extensions"]["sync"]["registries"]
    assert {r["content"][0]["destination"] for r in registries} == {f"/{u}" for u in render.CACHE_UPSTREAMS}
    assert all(r["onDemand"] and r["content"][0]["stripPrefix"] for r in registries)


def test_inventory_gives_k3s_the_cache_and_path_mtu(topo, tmp_path):
    lab_vars = render.render_inventory(topo, tmp_path / "c", tmp_path)["all"]["children"]["lab"]["vars"]
    assert lab_vars["lab_registry_cache"] == f"203.0.113.2:{render.CACHE_PORT}"
    assert lab_vars["lab_path_mtu"] == render.WG_MTU
    topo["interconnect"] = "none"
    assert render.render_inventory(topo, tmp_path / "c", tmp_path)["all"]["children"]["lab"]["vars"]["lab_path_mtu"] == 1500


def test_gateways_let_the_monitoring_node_scrape_them(topo):
    mon_ip = str(render.node_addresses(topo)["mon"].ip)
    for inst in render.plan(topo, "10.99.0.1")[1]:
        if inst.role == "gateway":
            firewall = inst.files["/etc/opsforge/firewall.nft"][0]
            assert f'ip saddr {mon_ip} tcp dport {{ {render.NODE_EXPORTER_PORT}, {render.BLACKBOX_PORT} }} accept' in firewall
    del topo["nodes"]["mon"]
    for inst in render.plan(topo, "10.99.0.1")[1]:
        assert str(render.BLACKBOX_PORT) not in inst.files.get("/etc/opsforge/firewall.nft", ("",))[0]


def test_inventory_marks_sites_with_an_http_ingress(topo, tmp_path):
    topo["nodes"]["c-w1"]["expose"] = ["8080:80/tcp", "80/udp"]
    sites = render.render_inventory(topo, tmp_path / "c", tmp_path)["all"]["children"]["lab"]["vars"]["lab_sites"]
    assert sites["a"]["ingress"] and sites["b"]["ingress"] and not sites["c"]["ingress"]


def test_mtu_black_hole_drops_big_packets_both_ways(topo):
    state = render.link_state(topo)
    assert "opsforge_mtu forward" not in render.render_links_sh(state)
    state["links"]["a-b"]["mtu"] = 1400
    script = render.render_links_sh(state)
    assert "ip saddr 203.0.113.10 ip daddr 203.0.113.20 ip length gt 1400 drop" in script
    assert "ip saddr 203.0.113.20 ip daddr 203.0.113.10 ip length gt 1400 drop" in script
    assert "mtu" not in render.netem_args(state["links"]["a-b"])


def test_stretched_layout_is_one_default_cluster(topo):
    clusters = render.k3s_clusters(topo)
    assert list(clusters) == [render.DEFAULT_CLUSTER]
    assert clusters["lab"] == {"server": "a-cp1", "nodes": ["a-cp1", "b-w1", "c-w1"], "sites": ["a", "b", "c"],
                             "api_port": render.K3S_API_PORT}
    assert render.cluster_links(topo) == []


def test_multi_cluster_ports_and_links_cost_the_rtt(multi):
    clusters = render.k3s_clusters(multi)
    assert {name: (c["server"], c["api_port"]) for name, c in clusters.items()} == {
        "a": ("a-cp1", 6443), "b": ("b-cp1", 6444), "c": ("c-cp1", 6445),
    }
    # each cluster dials the earlier ones, at the smallest RTT between their sites
    assert render.cluster_links(multi) == [("b", "a", 40), ("c", "a", 120), ("c", "b", 90)]


def test_inventory_tags_k3s_nodes_with_their_cluster(multi, tmp_path):
    inv = render.render_inventory(multi, tmp_path / "c", tmp_path)["all"]["children"]["lab"]
    nodes = inv["children"]["nodes"]["hosts"]
    assert nodes["b-cp1"]["lab_cluster"] == "b"
    assert "lab_cluster" not in nodes["mon"]
    assert inv["vars"]["lab_clusters"]["c"]["links"] == {"a": 120, "b": 90}
    assert next(i for i in render.plan(multi, "10.99.0.1")[1] if i.name == "c-cp1").cluster == "c"


def test_geo_targets_prefer_the_own_site_then_rtt(topo):
    # c: own ingress (VPC address, gateways do no hairpin NAT), then b (90 ms) before a (120 ms), by public IP
    c_w1 = str(render.node_addresses(topo)["c-w1"].ip)
    assert render.geo_targets(topo, "c") == [
        ("c", c_w1, f"http://{c_w1}:80/"),
        ("b", "203.0.113.20", "http://203.0.113.20/"),
        ("a", "203.0.113.10", "http://203.0.113.10/"),
    ]
    assert [s for s, _, _ in render.geo_targets(topo, "a")] == ["a", "b", "c"]


def test_geo_targets_skip_sites_without_an_http_ingress(topo, tmp_path):
    topo["nodes"]["c-w1"]["expose"] = ["443"]
    assert [s for s, _, _ in render.geo_targets(topo, "c")] == ["b", "a"]
    gws = render.render_inventory(topo, tmp_path / "c", tmp_path)["all"]["children"]["lab"]["children"]["gateways"]["hosts"]
    assert gws["c-gw"]["lab_geo"][0] == {"site": "b", "address": "203.0.113.20", "url": "http://203.0.113.20/"}
