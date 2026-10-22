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
    before = fingerprints(topo)
    topo["nodes"]["c-w1"]["site"] = "b"
    after = fingerprints(topo)
    assert {name for name in before if after[name] != before[name]} == {"c-w1"}
