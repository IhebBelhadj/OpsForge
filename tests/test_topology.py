"""Schema, address-plan and cross-reference checks (`topology_errors`)."""

from conftest import cli


def errors(topo):
    return cli.topology_errors(topo)


def test_repo_topology_is_valid(topo):
    assert errors(topo) == []


def test_schema_rejects_unknown_role(topo):
    topo["nodes"]["a-cp1"]["role"] = "wizard"
    assert errors(topo)


def test_public_ip_must_be_in_transit(topo):
    topo["sites"]["a"]["public_ip"] = "198.51.100.10"
    assert any("outside transit" in e for e in errors(topo))


def test_duplicate_public_ip(topo):
    topo["sites"]["b"]["public_ip"] = topo["sites"]["a"]["public_ip"]
    assert any("already used" in e for e in errors(topo))


def test_core_address_is_reserved(topo):
    topo["sites"]["a"]["public_ip"] = "203.0.113.1"
    assert any("reserved for wan-core" in e for e in errors(topo))


def test_vpc_overlap_between_sites(topo):
    topo["sites"]["b"]["vpc"] = "10.10.0.0/24"
    assert any("overlaps" in e for e in errors(topo))


def test_vpc_must_avoid_reserved_ranges(topo):
    for reserved in ("10.99.0.0/24", "172.17.0.0/24", "10.42.1.0/24", "10.43.0.0/24"):
        topo["sites"]["a"]["vpc"] = reserved
        assert any("overlaps" in e for e in errors(topo)), reserved


def test_link_to_unknown_site(topo):
    topo["links"]["a-z"] = {"rtt": "10ms"}
    assert any("unknown site 'z'" in e for e in errors(topo))


def test_duplicate_link_in_either_direction(topo):
    topo["links"]["b-a"] = {"rtt": "10ms"}
    assert any("duplicate link" in e for e in errors(topo))


def test_node_on_unknown_site(topo):
    topo["nodes"]["a-cp1"]["site"] = "z"
    assert any("unknown site 'z'" in e for e in errors(topo))


def test_node_cannot_take_router_name(topo):
    topo["nodes"]["wan-core"] = {"site": "a", "role": "k3s-agent"}
    assert any("reserved for a lab router" in e for e in errors(topo))


def test_expose_port_collision_on_one_site(topo):
    topo["nodes"]["a-cp1"]["expose"] = ["8080:80/tcp"]
    topo["nodes"]["a-w2"] = {"site": "a", "role": "k3s-agent", "expose": ["8080:81/tcp"]}
    assert any("already forwarded" in e for e in errors(topo))


def test_expose_same_port_other_protocol_is_fine(topo):
    topo["nodes"]["a-cp1"]["expose"] = ["8080:80/tcp", "8080:80/udp"]
    assert errors(topo) == []


def test_only_one_k3s_server(topo):
    topo["nodes"]["b-w1"]["role"] = "k3s-server"
    assert any("only one k3s-server" in e for e in errors(topo))


def test_vpc_exhausted(topo):
    topo["sites"]["a"]["vpc"] = "10.10.0.0/29"
    assert any("no address left" in e for e in errors(topo))


def test_save_link_rewrites_only_that_line(tmp_path):
    path = tmp_path / "topology.yml"
    path.write_text((cli.ROOT / "topology.yml").read_text())
    before = path.read_text()
    cli.save_link("a-c", {"rtt": "300ms", "jitter": "15ms", "loss": "2%", "rate": "20mbit"}, path)
    after = path.read_text()
    changed = [(a, b) for a, b in zip(before.splitlines(), after.splitlines()) if a != b]
    assert changed == [(
        "  a-c: { rtt: 120ms, jitter: 15ms, loss: 1%, rate: 20mbit }",
        "  a-c: { rtt: 300ms, jitter: 15ms, loss: 2%, rate: 20mbit }",
    )]
    topo = cli.load_topology(path)
    assert topo["links"]["a-c"]["rtt"] == "300ms"
    assert errors(topo) == []
