"""The environment as code (docs/PLAN.md Phase 12, step 1): which topology is active, `env plan`'s diff, the K3s
nodes an applied document leaves behind, and `env apply`'s order. No Incus: what touches it is faked."""

import argparse
import subprocess

import pytest
import yaml

from conftest import ROOT, cli


def one_site(topo):
    """The stretched lab cut down to site a: one K3s server, no VPN, no links."""
    topo["sites"] = {"a": topo["sites"]["a"]}
    topo["links"] = {}
    topo["interconnect"] = "none"
    topo.pop("routing", None)
    topo["nodes"] = {"a-cp1": topo["nodes"]["a-cp1"]}
    return topo


def write(tmp_path, doc, name="env.yml"):
    path = tmp_path / name
    path.write_text(yaml.safe_dump(doc, sort_keys=False))
    return path


def test_the_active_topology_is_the_override_then_the_applied_environment_then_topology_yml(tmp_path, monkeypatch):
    assert cli.topology_path() == ROOT / "topology.yml"
    env = tmp_path / "environment.yml"
    env.write_text("sites: {}\n")
    monkeypatch.setattr(cli, "ENVIRONMENT", env)
    assert cli.topology_path() == env and "env apply" in cli.topology_source()
    monkeypatch.setenv("OPSFORGE_TOPOLOGY", str(ROOT / "topologies" / "multi-cluster.yml"))
    assert cli.topology_path() == ROOT / "topologies" / "multi-cluster.yml"
    assert "OPSFORGE_TOPOLOGY" in cli.topology_source()


@pytest.mark.parametrize("path", sorted((ROOT / "topologies").glob("*.yml")), ids=lambda p: p.name)
def test_every_shipped_document_is_valid(path):
    cli.load_document(path)


def test_a_one_site_document_is_valid(topo, tmp_path):
    assert cli.load_document(write(tmp_path, one_site(topo)))["nodes"].keys() == {"a-cp1"}


def test_an_invalid_document_is_refused_with_its_errors(topo, tmp_path):
    topo["nodes"]["a-cp1"]["site"] = "z"
    with pytest.raises(cli.LabError, match=r"error\(s\)\n  - .*z"):
        cli.load_document(write(tmp_path, topo))


def live_instance(inst, status="Running", **config):
    return {"name": inst.name, "status": status, "config": cli.desired_config(inst) | config}


def test_plan_from_the_stretched_lab_to_one_site_deletes_what_left_and_rewires_what_changed(topo):
    _, running = cli.render.plan(topo, "10.99.0.1")
    _, wanted = cli.render.plan(one_site(topo), "10.99.0.1")
    live = {inst.name: live_instance(inst) for inst in running}
    plan = cli.plan_changes(wanted, live, lambda inst, cur: False)
    assert plan["delete"] == ["b-gw", "b-w1", "c-gw", "c-w1", "ci", "mon"]
    assert plan["create"] == [] and plan["recreate"] == []
    # wan-core loses two sites, a's gateway its VPN: their wiring or files change
    assert {"wan-core", "a-gw"} <= set(plan["reconfigure"])


def test_plan_from_one_site_back_to_the_stretched_lab_creates_the_rest(topo):
    full = cli.render.plan(topo, "10.99.0.1")[1]
    small = cli.render.plan(one_site(topo), "10.99.0.1")[1]
    live = {inst.name: live_instance(inst, status="Stopped") for inst in small}
    plan = cli.plan_changes(full, live, lambda inst, cur: inst.name == "a-cp1")
    assert plan["create"] == ["b-gw", "c-gw", "b-w1", "c-w1", "mon", "ci"]
    assert plan["recreate"] == ["a-cp1"] and plan["delete"] == []


def test_unchanged_running_instances_are_left_out_and_stopped_ones_started(topo):
    _, instances = cli.render.plan(topo, "10.99.0.1")
    live = {inst.name: live_instance(inst, status="Stopped" if inst.name == "mon" else "Running")
            for inst in instances}
    plan = cli.plan_changes(instances, live, lambda inst, cur: False)
    assert {k: v for k, v in plan.items() if v} == {"start": ["mon"]}


def test_k3s_nodes_that_left_the_document_are_stale(topo):
    nodes = {"a-cp1": ["a-cp1", "b-w1", "c-w1"]}
    assert cli.stale_k3s_nodes(one_site(topo), nodes.get) == {"a-cp1": ["b-w1", "c-w1"]}


def test_a_cluster_that_cannot_be_asked_has_no_stale_nodes(topo, multi):
    assert cli.stale_k3s_nodes(topo, lambda server: None) == {}
    # multi-cluster: one server per site, each asked about its own cluster only
    nodes = {"a-cp1": ["a-cp1", "a-w9"], "b-cp1": ["b-cp1"], "c-cp1": None}
    assert cli.stale_k3s_nodes(multi, nodes.get) == {"a-cp1": ["a-w9"]}


def test_removing_stale_nodes_deletes_them_on_their_server(topo, monkeypatch):
    ran = []
    monkeypatch.setattr(cli, "kubectl_json", lambda server, *a: [{"metadata": {"name": n}} for n in
                                                                  ("a-cp1", "b-w1", "c-w1")])
    monkeypatch.setattr(cli, "run_in", lambda name, *cmd: ran.append((name, *cmd)) or
                        subprocess.CompletedProcess(cmd, 0, "", ""))
    cli.remove_stale_k3s_nodes(one_site(topo))
    assert ran == [("a-cp1", "k3s", "kubectl", "delete", "node", "b-w1"),
                   ("a-cp1", "k3s", "kubectl", "delete", "node", "c-w1")]


def apply_args(path, **kw):
    return argparse.Namespace(action="apply", file=path, no_provision=kw.get("no_provision", False))


def test_apply_records_the_document_then_ups_cleans_and_provisions(topo, tmp_path, monkeypatch):
    env = tmp_path / "state" / "environment.yml"
    monkeypatch.setattr(cli, "STATE", env.parent)
    monkeypatch.setattr(cli, "ENVIRONMENT", env)
    monkeypatch.setattr(cli, "ROOT", tmp_path)
    calls = []
    monkeypatch.setattr(cli, "cmd_up", lambda a: calls.append(("up", cli.topology_path())) or 0)
    monkeypatch.setattr(cli, "remove_stale_k3s_nodes", lambda t: calls.append(("clean", sorted(t["nodes"]))))
    monkeypatch.setattr(cli, "cmd_provision", lambda a: calls.append(("provision", a.ansible_args)) or 0)
    doc = write(tmp_path, one_site(topo))
    assert cli.cmd_env(apply_args(doc)) == 0
    assert env.read_text() == doc.read_text()
    assert calls == [("up", env), ("clean", ["a-cp1"]), ("provision", [])]


def test_apply_stops_when_up_fails_and_can_skip_provisioning(topo, tmp_path, monkeypatch):
    monkeypatch.setattr(cli, "STATE", tmp_path)
    monkeypatch.setattr(cli, "ENVIRONMENT", tmp_path / "environment.yml")
    monkeypatch.setattr(cli, "ROOT", tmp_path)
    calls = []
    monkeypatch.setattr(cli, "remove_stale_k3s_nodes", lambda t: calls.append("clean"))
    monkeypatch.setattr(cli, "cmd_provision", lambda a: calls.append("provision") or 0)
    doc = write(tmp_path, one_site(topo), "doc.yml")
    monkeypatch.setattr(cli, "cmd_up", lambda a: 1)
    with pytest.raises(cli.LabError, match="up failed"):
        cli.cmd_env(apply_args(doc))
    assert calls == [] and cli.ENVIRONMENT.read_text() == doc.read_text(), "kept: applying it again resumes"
    monkeypatch.setattr(cli, "cmd_up", lambda a: 0)
    assert cli.cmd_env(apply_args(doc, no_provision=True)) == 0 and calls == ["clean"]


def test_apply_refuses_an_invalid_document_or_an_override(topo, tmp_path, monkeypatch):
    monkeypatch.setattr(cli, "cmd_up", lambda a: pytest.fail("up ran"))
    topo["nodes"]["a-cp1"]["site"] = "z"
    with pytest.raises(cli.LabError, match="error"):
        cli.cmd_env(apply_args(write(tmp_path, topo)))
    monkeypatch.setenv("OPSFORGE_TOPOLOGY", str(ROOT / "topology.yml"))
    with pytest.raises(cli.LabError, match="OPSFORGE_TOPOLOGY"):
        cli.cmd_env(apply_args(ROOT / "topology.yml"))
    assert not cli.ENVIRONMENT.exists()


# Step 3: the `environment` step (an RFC 7386 merge patch) and its checks


def test_merge_patch_follows_rfc_7386():
    target = {"a": "b", "c": {"d": "e", "f": "g"}}
    assert cli.scenario.merge_patch(target, {"a": "z", "c": {"f": None}}) == {"a": "z", "c": {"d": "e"}}
    assert cli.scenario.merge_patch(target, {"c": {"h": {"i": 1}}})["c"] == {"d": "e", "f": "g", "h": {"i": 1}}
    assert cli.scenario.merge_patch({"a": [1, 2]}, {"a": [3]}) == {"a": [3]}, "lists are replaced, not merged"
    assert cli.scenario.merge_patch(target, {"x": None}) == target and target["c"] == {"d": "e", "f": "g"}


def test_an_environment_step_is_a_patch_of_the_documents_keys():
    errs = cli.scenario.errors({"description": "-", "setup": [{"environment": {"nodes": {}}}, {"environment": []},
                                                              {"environment": {"node": {}}}]})
    assert errs == ["setup[1]: environment takes a merge patch of the environment document, e.g. "
                    "{nodes: {b-w2: {site: b, role: k3s-agent}}}",
                    "setup[2]: environment: no 'node' in an environment document (one of transit, interconnect, "
                    "routing, sites, links, nodes)"]


def test_the_patchable_keys_are_the_schemas():
    schema = cli.json.loads(cli.SCHEMA.read_text())
    assert set(cli.scenario.DOCUMENT_KEYS) == set(schema["properties"])


AGENT = {"nodes": {"a-w2": {"site": "a", "role": "k3s-agent", "cpu": 1, "memory": "768MiB"}}}


def test_environment_steps_need_an_environment_and_each_patch_is_checked_in_order(topo):
    steps = [{"environment": AGENT}, {"environment": {"nodes": {"a-w2": {"site": "z"}}}}]
    assert cli.contract.step_errors([{"steps": steps}], cli.topology_errors) == [
        {"set": 0, "message": f"steps[{i}]: environment: only in the setup or actions of a sandbox with an environment "
                              "of its own (sandboxInput.environment), never in a check"} for i in (0, 1)]
    errors = cli.contract.step_errors([{"steps": steps, "environment": one_site(topo)}], cli.topology_errors)
    assert [e["message"].split(":")[0] for e in errors] == ["steps[1]"], "the first patch is valid, the second not"
    assert "z" in errors[0]["message"]


def test_the_environment_step_reshapes_the_lab_and_later_steps_see_the_new_document(topo, monkeypatch):
    applied = []
    monkeypatch.setattr(cli, "apply_environment", lambda doc, log: applied.append(doc) or log("reshaped"))
    run = cli.ScenarioRun(one_site(topo))
    run.log = lambda phase, text: None
    run.step("setup", {"environment": AGENT})
    assert sorted(applied[0]["nodes"]) == ["a-cp1", "a-w2"] and run.topo is applied[0]
    with pytest.raises(cli.LabError, match="patched document is invalid"):
        run.step("setup", {"environment": {"nodes": {"a-w3": {"site": "z", "role": "k3s-agent"}}}})
    assert len(applied) == 1
