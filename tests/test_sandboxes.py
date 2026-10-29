"""Sandboxes (lab/sandboxes.py, docs/PLAN.md Phase 11 split step 7) over HTTP: made from a client's requirements,
cleaned up by OpsForge (a failed setup at once; delete; expiry; after a restart). Lab work runs host `run` steps (no
Incus); the lab state, namespaces and heal are faked (test_apiserver's `lab`)."""

import fcntl
import json
import subprocess

import pytest

import apiserver
import sandboxes
from conftest import ROOT, cli
from test_api import contract_doc, echo, golden, timeless
from test_apiserver import api, lab, problem, wait_job  # noqa: F401 (fixtures)


def create(api, **doc):
    """(status, body) of POST /v1/sandboxes."""
    status, _, body = api.call("POST", "/v1/sandboxes", {"contract": 1, "kind": "lab", "label": "test", **doc})
    return status, body


def delete(api, sid):
    status, _, job = api.call("DELETE", f"/v1/sandboxes/{sid}")
    assert status == 202, job
    return wait_job(api, job["id"])


def timeless_sandbox(doc):
    """A sandbox document with its id, times and log stamps fixed, for golden documents."""
    doc = json.loads(json.dumps(doc).replace(doc["id"], "0123abcd"))
    for part in ("setup", "teardown"):
        if doc[part]:
            doc[part] = timeless(doc[part])
    return {**doc, "created_at": "2026-10-26T12:00:00Z", "expires_at": "2026-10-27T12:00:00Z"}


# Creating: the workspace, the namespace, setup; the job knows its sandbox from the 202 on


def test_a_sandbox_is_made_from_its_input(api, lab):
    status, job = create(api, label="lab nodes-and-regions, attempt 7", requires={"needs": ["k3s"]}, namespace=True,
                         files={"deploy.yaml": "kind: Deployment\n"},
                         setup=[{"name": "here", "run": {"cmd": "test -f deploy.yaml && command -v opsforge >/dev/null "
                                                                "&& echo 1"}},
                                {"run": {"cmd": "echo -n ${ns} > ns.txt"}}],
                         teardown=[{"run": {"cmd": "echo bye"}}])
    assert status == 202 and job["kind"] == "create" and job["sandbox"]
    sid = job["sandbox"]
    sb = contract_doc("sandboxOutput", wait_job(api, job["id"])["output"])
    assert sb["status"] == "ready" and sb["namespace"] == f"sb-{sid}" and sb["setup"]["error"] is None
    assert sb["setup"]["metrics"]["here.value"] == 1, "steps run in the workspace with opsforge on the PATH"
    assert (api.workspaces / sid / "ns.txt").read_text() == f"sb-{sid}"
    assert sb["placeholders"]["${ns}"] == f"sb-{sid}" and sb["placeholders"]["${node:a}"] == "a-cp1"
    assert sb["layout"] == "stretched" and sb["teardown"] is None
    created = [c for _, c in lab.kube if "create namespace" in c]
    assert len(created) == 1 and f"opsforge.lab/sandbox={sid}" in created[0] and "managed-by=opsforge-api" in created[0]
    got = contract_doc("sandboxOutput", api.call("GET", f"/v1/sandboxes/{sid}")[2])
    assert {**got, "expires_at": None} == {**sb, "expires_at": None} and got["expires_at"] >= sb["expires_at"], \
        "a call keeps it from expiring"
    listed = contract_doc("sandboxesOutput", api.call("GET", "/v1/sandboxes")[2])
    assert [s["id"] for s in listed["sandboxes"]] == [sid]
    golden("sandbox.json", timeless_sandbox(sb))


def test_without_a_namespace(api, lab):
    sb = api.sandbox(setup=[{"run": {"cmd": "true"}}])
    assert sb["namespace"] is None and "${ns}" not in sb["placeholders"] and not lab.kube
    status, body = create(api, setup=[{"run": {"cmd": "echo ${ns}"}}])
    sb = wait_job(api, body["id"])["output"]
    assert sb["status"] == "failed" and "${ns}" in sb["setup"]["error"]


# Requirements: checked against the running lab before anything is made


def test_unmet_requirements_are_refused(api, lab):
    status, body = create(api, requires={"layout": "multi-cluster", "needs": ["vpn", "ci"]})
    assert problem(status, body, "unmet-requirements") == 409
    assert len(body["unmet"]) == 1 and body["unmet"][0].startswith("layout multi-cluster (the lab is stretched")
    lab.topo = {**lab.topo, "nodes": {n: v for n, v in lab.topo["nodes"].items() if v["role"] != "ci"}}
    status, body = create(api, requires={"needs": ["vpn", "ci", "monitoring"]})
    assert problem(status, body, "unmet-requirements") == 409 and body["unmet"] == ["ci (not in topology.yml)"]
    lab.topo = {**lab.topo, "nodes": {n: v for n, v in lab.topo["nodes"].items() if not v["role"].startswith("k3s")}}
    status, body = create(api, namespace=True)
    assert body["unmet"] == ["k3s (a namespace needs a K3s cluster)"]
    lab.state = {**lab.state, "up": False, "error": None}
    status, body = create(api)
    assert problem(status, body, "lab-down") == 409
    assert api.call("GET", "/v1/sandboxes")[2]["sandboxes"] == [] and not list(api.workspaces.glob("*"))


def test_fault_steps_must_be_declared(api):
    status, body = create(api, setup=[{"partition": "c"}])
    assert problem(status, body, "bad-input") == 400 and "setup: steps[0]: partition: fault steps" in body["detail"]
    status, body = create(api, setup=[{"pign": {}}])
    assert problem(status, body, "bad-input") == 400 and "unknown step 'pign'" in body["detail"]


def test_a_sandbox_with_faults_runs_alone(api, lab, monkeypatch):
    monkeypatch.setattr(cli, "cmd_partition", lambda args: None)
    faulty = api.sandbox(requires={"faults": True}, setup=[{"partition": "c"}])
    assert faulty["status"] == "ready"
    status, body = create(api)
    assert problem(status, body, "lab-busy") == 409 and faulty["id"] in body["detail"]
    delete(api, faulty["id"])
    assert lab.healed, "deleting a sandbox with faults heals the lab"
    plain = api.sandbox()
    status, body = create(api, requires={"cluster_changes": True})
    assert problem(status, body, "lab-busy") == 409 and plain["id"] in body["detail"]
    assert api.sandbox()["status"] == "ready", "sandboxes that stay in their namespace coexist"


# Cleanup: OpsForge's, whatever the client does


def test_a_failed_setup_is_cleaned_up_at_once(api, lab):
    sb = api.sandbox(namespace=True, requires={"faults": True}, setup=[echo("a", 1), {"run": {"cmd": "exit 3"}},
                                                                      {"name": "zz", "run": {"cmd": "echo ${node:zz}"}}],
                     teardown=[{"name": "t", "run": {"cmd": "echo 5"}}])
    assert sb["status"] == "failed" and "no K3s node in site 'zz'" in sb["setup"]["error"]
    assert sb["teardown"]["metrics"]["t.value"] == 5 and sb["teardown"]["error"] is None
    assert [c for _, c in lab.kube if "namespace" in c][-1].endswith("--timeout=120s") and lab.healed
    assert (api.workspaces / sb["id"]).is_dir(), "the files stay until the sandbox is deleted"
    status, _, body = api.call("POST", f"/v1/sandboxes/{sb['id']}/probes",
                               {"contract": 1, "steps": [echo("a", 1)], "expect": {"a.value": "== 1"}})
    assert problem(status, body, "sandbox-state") == 409
    kube, healed = len(lab.kube), len(lab.healed)
    out = delete(api, sb["id"])["output"]
    assert out["status"] == "deleted" and len(lab.kube) == kube and len(lab.healed) == healed, "cleaned up once"


def test_delete_runs_the_teardown_and_removes_everything(api, lab):
    sb = api.sandbox(namespace=True, setup=[{"run": {"cmd": "echo hi > out.txt"}}],
                     teardown=[{"name": "t", "run": {"cmd": "cat out.txt"}}])
    job = delete(api, sb["id"])
    out = contract_doc("sandboxOutput", job["output"])
    assert job["kind"] == "delete" and out["status"] == "deleted" and out["teardown"]["error"] is None
    assert any("cat out.txt" in line for line in out["teardown"]["log"]), "teardown ran in the workspace"
    assert f"delete namespace sb-{sb['id']}" in lab.kube[-1][1] and not lab.healed, "no faults: no heal"
    assert not (api.workspaces / sb["id"]).exists() and not (api.data / "sandboxes" / f"{sb['id']}.json").exists()
    status, _, body = api.call("GET", f"/v1/sandboxes/{sb['id']}")
    assert problem(status, body, "not-found") == 404


def test_reset_runs_teardown_then_setup_and_keeps_the_files(api):
    sb = api.sandbox(files={"mine.txt": "learner's\n"}, setup=[{"run": {"cmd": "echo setup >> trail"}}],
                     teardown=[{"run": {"cmd": "echo teardown >> trail"}}])
    status, _, job = api.call("POST", f"/v1/sandboxes/{sb['id']}/reset")
    assert status == 202 and job["kind"] == "reset"
    out = wait_job(api, job["id"])["output"]
    assert out["status"] == "ready" and out["teardown"] is None
    ws = api.workspaces / sb["id"]
    assert (ws / "trail").read_text().split() == ["setup", "teardown", "setup"] and (ws / "mine.txt").exists()


def test_probes_and_actions_only_observe(api):
    sb = api.sandbox()
    for kind, doc in (("probes", {"steps": [{"partition": "c"}], "expect": {"x.value": "== 1"}}),
                      ("actions", {"steps": [{"degrade": {"link": "a-b", "rtt": "10ms"}}]})):
        status, _, body = api.call("POST", f"/v1/sandboxes/{sb['id']}/{kind}", {"contract": 1, **doc})
        assert problem(status, body, "bad-input") == 400 and "fault steps are not allowed" in body["detail"]


# Records outlive the server; idle sandboxes expire


def test_sandboxes_survive_a_restart(api, lab, tmp_path):
    sb = api.sandbox(namespace=True, teardown=[{"run": {"cmd": "echo bye"}}])
    store = sandboxes.Sandboxes(cli, tmp_path, apiserver.Workspaces(tmp_path / "workspaces"), apiserver.Jobs(cli))
    again = store.get(sb["id"], touch=False)
    assert again.doc(1) == sb and again.teardown_steps == [{"run": {"cmd": "echo bye"}}] and again.dirty


def test_lab_work_a_stopped_api_left_halfway_is_cleaned_up(api, lab, tmp_path):
    sb = api.sandbox(namespace=True)
    record = tmp_path / "sandboxes" / f"{sb['id']}.json"
    record.write_text(json.dumps({**json.loads(record.read_text()), "status": "creating"}))
    jobs = apiserver.Jobs(cli)
    store = sandboxes.Sandboxes(cli, tmp_path, apiserver.Workspaces(tmp_path / "workspaces"), jobs)
    assert store.get(sb["id"], touch=False).status == "deleting"
    started = store.reap()
    started[sb["id"]].done.wait(20)
    assert not store.items and not record.exists() and f"delete namespace sb-{sb['id']}" in lab.kube[-1][1]


def test_idle_sandboxes_expire(api, monkeypatch):
    sb = api.sandbox(expires_after=60)
    other = api.sandbox()
    clock = sandboxes.now()
    monkeypatch.setattr(sandboxes, "now", lambda: clock + 59)
    assert api.sandboxes.reap() == {}
    api.call("GET", f"/v1/sandboxes/{sb['id']}")  # a call: idle from now on
    monkeypatch.setattr(sandboxes, "now", lambda: clock + 59 + 60)
    started = api.sandboxes.reap()
    assert list(started) == [sb["id"]]
    started[sb["id"]].done.wait(20)
    assert [s["id"] for s in api.call("GET", "/v1/sandboxes")[2]["sandboxes"]] == [other["id"]]


def test_expiry_is_bounded(api):
    for after in (59, 604801):
        status, body = create(api, expires_after=after)
        assert problem(status, body, "bad-input") == 400


# The lab owner's escape hatch: opsforge api sandbox list | delete


def run_cli(*args):
    return subprocess.run([str(ROOT / "opsforge"), "api", "sandbox", *args], capture_output=True, text=True, cwd=ROOT)


def test_the_cli_lists_and_deletes_sandboxes(api, tmp_path):
    sb = api.sandbox(label="lab losing-a-site, attempt 3")
    out = run_cli("list", "--data", str(tmp_path))
    assert out.returncode == 0 and sb["id"] in out.stdout and "lab losing-a-site, attempt 3" in out.stdout
    listed = json.loads(run_cli("--json", "--data", str(tmp_path)).stdout)
    assert [s["id"] for s in contract_doc("sandboxesOutput", listed)["sandboxes"]] == [sb["id"]]
    lock = open(tmp_path / "sandboxes" / ".serving", "w")  # noqa: SIM115 (as api serve holds it)
    fcntl.flock(lock, fcntl.LOCK_EX)
    out = run_cli("delete", sb["id"], "--data", str(tmp_path))
    assert out.returncode == 1 and "opsforge api serve owns" in out.stderr
    lock.close()
    out = run_cli("delete", sb["id"], "--data", str(tmp_path))
    assert out.returncode == 0 and f"sandbox {sb['id']} deleted" in out.stdout, out.stderr
    assert not (tmp_path / "sandboxes" / f"{sb['id']}.json").exists()
    out = run_cli("delete", "0123abcd", "--data", str(tmp_path))
    assert out.returncode == 1 and "no sandbox 0123abcd" in out.stderr


@pytest.mark.parametrize("args", [["nope"], ["delete"], ["list", "x"]])
def test_the_cli_refuses_other_words(args, tmp_path):
    out = run_cli(*args, "--data", str(tmp_path))
    assert out.returncode == 1 and "usage" in out.stderr


def test_opsforge_kubectl_defaults_to_the_sandboxs_namespace(monkeypatch, tmp_path):
    config = tmp_path / "kubeconfig"
    config.write_text("")
    calls = []
    monkeypatch.setattr(cli, "kubeconfig", lambda cluster: config)
    monkeypatch.setattr(cli.shutil, "which", lambda name: "/usr/bin/kubectl")
    monkeypatch.setattr(cli, "active_forwards", lambda: [{"local": cli.k3s_cluster(cli.load_valid_topology(), None)[1]["api_port"]}])
    monkeypatch.setattr(cli.os, "execvp", lambda name, argv: calls.append(argv[3:]))
    args = cli.argparse.Namespace(cluster=None, kubectl_args=["get", "pods", "-n", "kube-system"])
    monkeypatch.setenv("OPSFORGE_NAMESPACE", "sb-0123abcd")
    cli.cmd_kubectl(args)
    monkeypatch.delenv("OPSFORGE_NAMESPACE")
    cli.cmd_kubectl(args)
    assert calls == [["--namespace=sb-0123abcd", "get", "pods", "-n", "kube-system"], ["get", "pods", "-n", "kube-system"]]


# Minor 1: a sandbox with its own environment (docs/PLAN.md Phase 12): the lab is reshaped to it, then setup


def one_site(topo):
    return {**topo, "sites": {"a": topo["sites"]["a"]}, "links": {}, "interconnect": "none",
            "nodes": {"a-cp1": topo["nodes"]["a-cp1"]}} | ({} if "routing" not in topo else {"routing": "static"})


@pytest.fixture
def reshapes(monkeypatch):
    """The documents the lab was reshaped to (apply_environment faked: no Incus)."""
    done = []
    monkeypatch.setattr(cli, "apply_environment", lambda doc, log: done.append(doc) or log("reshaped"))
    return done


def test_a_sandbox_brings_its_environment_and_needs_no_running_lab(api, lab, reshapes):
    lab.state = {**lab.state, "up": False}
    env = one_site(lab.topo)
    sb = api.sandbox(environment=env, requires={"needs": ["k3s"]}, setup=[echo("a", 1)])
    assert sb["status"] == "ready" and reshapes == [env] and sb["setup"]["metrics"]["a.value"] == 1
    assert sb["layout"] == "stretched" and sb["placeholders"]["${node:a}"] == "a-cp1"
    assert "reshaped" in " ".join(sb["setup"]["log"])


def test_a_sandbox_environment_is_checked_and_meets_the_requirements(api, lab, reshapes):
    env = one_site(lab.topo)
    status, body = create(api, environment={**env, "nodes": {"a-cp1": {**env["nodes"]["a-cp1"], "site": "z"}}})
    assert problem(status, body, "bad-input") == 400 and body["detail"].startswith("environment: ")
    status, body = create(api, environment=env, requires={"needs": ["vpn"]})
    assert problem(status, body, "unmet-requirements") == 409 and body["unmet"][0].startswith("vpn")
    assert reshapes == [] and api.call("GET", "/v1/sandboxes")[2]["sandboxes"] == []


def test_a_sandbox_with_an_environment_runs_alone(api, lab, reshapes):
    plain = api.sandbox()
    status, body = create(api, environment=one_site(lab.topo))
    assert problem(status, body, "lab-busy") == 409 and plain["id"] in body["detail"]
    delete(api, plain["id"])
    own = api.sandbox(environment=one_site(lab.topo))
    status, body = create(api)
    assert problem(status, body, "lab-busy") == 409 and own["id"] in body["detail"]


def test_environment_steps_only_in_a_sandbox_with_an_environment_and_never_in_probes(api, lab, reshapes):
    patch = {"environment": {"nodes": {"a-w2": {"site": "a", "role": "k3s-agent"}}}}
    status, body = create(api, setup=[patch])
    assert problem(status, body, "bad-input") == 400 and "environment: only in the setup or actions" in body["detail"]
    sb = api.sandbox(environment=one_site(lab.topo))
    lab.topo = one_site(lab.topo)  # the running document once reshaped
    status, _, body = api.call("POST", f"/v1/sandboxes/{sb['id']}/probes",
                               {"contract": 1, "steps": [patch], "expect": {}})
    assert problem(status, body, "bad-input") == 400
    status, _, job = api.call("POST", f"/v1/sandboxes/{sb['id']}/actions", {"contract": 1, "steps": [patch]})
    assert status == 202 and wait_job(api, job["id"])["output"]["error"] is None
    assert sorted(reshapes[-1]["nodes"]) == ["a-cp1", "a-w2"], "an action patched the running document"
