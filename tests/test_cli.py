"""CLI helpers that need no Incus: `forward` argument handling and state records, the `capture` command line."""

import pytest

from conftest import cli


def test_parse_forward():
    assert cli.parse_forward("a-cp1:6443") == ("a-cp1", 6443)


@pytest.mark.parametrize("spec", ["a-cp1", "a-cp1:", ":6443", "a-cp1:http", "a-cp1:0", "a-cp1:65536"])
def test_parse_forward_rejects(spec):
    with pytest.raises(cli.LabError):
        cli.parse_forward(spec)


def test_privileged_ports_move_up_locally():
    assert cli.default_local_port(6443) == 6443
    assert cli.default_local_port(1024) == 1024
    assert cli.default_local_port(80) == 10080
    assert cli.default_local_port(443) == 10443


def test_dead_forwards_are_forgotten(tmp_path, monkeypatch):
    monkeypatch.setattr(cli, "FORWARDS", tmp_path)
    monkeypatch.setattr(cli, "ssh_control", lambda sock, op, host: host == "b-w1")
    (tmp_path / "8080.json").write_text('{"host": "b-w1", "port": 8080, "local": 8080, "to": "127.0.0.1"}')
    (tmp_path / "10022.json").write_text('{"host": "a-gw", "port": 22, "local": 10022, "to": "127.0.0.1"}')
    (tmp_path / "10022.sock").touch()
    assert [f["host"] for f in cli.active_forwards()] == ["b-w1"]
    assert sorted(p.name for p in tmp_path.iterdir()) == ["8080.json"]


def test_capture_runs_under_timeout_with_the_filter_as_one_argument():
    assert cli.capture_cmd("any", None, 10, ["udp", "port", "51820"]) == [
        "timeout", "10", "tcpdump", "-i", "any", "-n", "-p", "-U", "-w", "-", "udp port 51820",
    ]


def test_capture_until_interrupted_with_a_packet_limit():
    assert cli.capture_cmd("wan", 5, 0, []) == ["tcpdump", "-i", "wan", "-n", "-p", "-U", "-w", "-", "-c", "5"]


def _image(fingerprint, *aliases):
    return {"fingerprint": fingerprint, "aliases": [{"name": a} for a in aliases], "size": 0}


def test_gc_keeps_current_unaliased_foreign_and_in_use_images():
    current = next(iter(cli.render.IMAGES))
    images = [
        _image("old", "opsforge-router-1"),
        _image("cur", current),
        _image("base"),  # Incus' cache of images:debian/13
        _image("mine", "my-image"),
        _image("mixed", "opsforge-node-1", "keep-me"),
        _image("busy", "opsforge-node-1b"),
    ]
    instances = [{"config": {"volatile.base_image": "busy"}}]
    assert [i["fingerprint"] for i in cli.stale_images(images, instances)] == ["old"]


def test_nic_changes():
    devices = {
        "root": {"type": "disk", "path": "/"},
        "eth0": {"type": "nic", "network": "incusbr0"},
        "to-a": {"type": "nic", "network": "of-a-wan"},
        "to-c": {"type": "nic", "network": "of-c-wan"},
    }
    nics = {"eth0": "incusbr0", "to-a": "of-a-wan", "to-d": "of-d-wan"}
    assert cli.nic_changes(devices, nics) == (["to-c"], {"to-d": "of-d-wan"})
    assert cli.nic_changes({"eth0": {"type": "nic", "network": "of-a"}}, {"eth0": "of-b"}) == (["eth0"], {"eth0": "of-b"})


def test_up_keeps_live_link_conditions(topo):
    desired = cli.render.link_state(topo)
    live = {"sites": dict(desired["sites"]), "links": dict(desired["links"]) | {"a-b": {"rtt": "300ms"}}}
    del live["links"]["b-c"]
    topo["sites"]["d"] = {"vpc": "10.40.0.0/24", "public_ip": "203.0.113.40"}
    topo["links"]["a-d"] = {"rtt": "10ms"}
    del topo["links"]["a-c"]
    merged = cli.merge_link_state(live, cli.render.link_state(topo))
    assert merged["sites"]["d"] == "203.0.113.40"
    assert merged["links"]["a-b"] == {"rtt": "300ms"}  # live edit kept
    assert merged["links"]["b-c"] == topo["links"]["b-c"]  # missing live: from topology.yml
    assert "a-d" in merged["links"] and "a-c" not in merged["links"]


def test_desired_config_tags_and_spec(topo):
    _, instances = cli.render.plan(topo, "10.99.0.1")
    node = next(i for i in instances if i.name == "b-w1")
    config = cli.desired_config(node)
    assert config["user.opsforge.site"] == "b" and config["limits.memory"] == "768MiB"
    assert config[cli.SPEC_KEY] == cli.render.fingerprint(node)
    assert cli.config_changes(config | {"limits.memory": "1GiB"}, config) == {"limits.memory": "768MiB"}


def test_lab_snapshots_groups_by_name():
    insts = [
        {"name": "a-gw", "snapshots": [{"name": "base", "created_at": "2026-10-22T10:00:01Z"}]},
        {"name": "wan-core", "snapshots": [
            {"name": "base", "created_at": "2026-10-22T10:00:00Z"},
            {"name": "late", "created_at": "2026-10-22T11:00:00Z"},
        ]},
        {"name": "b-w1", "snapshots": None},
    ]
    assert cli.lab_snapshots(insts) == {
        "base": ("2026-10-22T10:00:00Z", ["a-gw", "wan-core"]),
        "late": ("2026-10-22T11:00:00Z", ["wan-core"]),
    }
