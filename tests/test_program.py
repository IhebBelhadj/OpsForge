"""The `program` step (contract minor 2): its checks, the pod it runs, waiting for it, and the runtimes a document
asks its nodes to pull. No Incus: the cluster is faked."""

import json
import subprocess

import pytest

from conftest import cli

PROGRAM = {"runtime": "python", "files": ["judge.py", "solution.py"], "cmd": ["python3", "judge.py"]}


def errors(args):
    return cli.scenario.errors({"description": "-", "measure": [{"program": args, "name": "judge"}]})


def test_a_program_step_is_checked():
    assert errors(PROGRAM) == []
    assert errors({**PROGRAM, "timeout": "30s"}) == []
    assert errors({**PROGRAM, "runtime": "ruby", "files": ["../x", "a/b"], "cmd": "python3 judge.py",
                   "timeout": "5m"}) == [
        "measure[0]: program: runtime 'ruby' is not one of python, javascript, go",
        "measure[0]: program: files: '../x' is not a plain file name (letters, digits, '.', '_', '-'; no folders)",
        "measure[0]: program: files: 'a/b' is not a plain file name (letters, digits, '.', '_', '-'; no folders)",
        "measure[0]: program: cmd is the command as a list of words, e.g. [python3, judge.py]",
        "measure[0]: program: timeout is at most 60s",
    ]
    assert errors({"runtime": "go"}) == ["measure[0]: program needs 'files'", "measure[0]: program needs 'cmd'",
                                         "measure[0]: program: files is a list of 1 to 16 workspace files",
                                         "measure[0]: program: cmd is the command as a list of words, e.g. "
                                         "[python3, judge.py]"]


def test_program_steps_are_allowed_in_checks():
    sets = [{"steps": [{"program": PROGRAM, "name": "judge"}], "faults": False, "expect": {"judge.exit": "== 0"}}]
    assert cli.contract.step_errors(sets, cli.topology_errors) == []


def test_the_pod_is_locked_down():
    doc = cli.program_manifest("program-1", "go", {"main.go": "package main"}, ["go", "run", "main.go"], 10)
    config, policy, pod = doc["items"]
    assert config["data"] == {"main.go": "package main"}
    assert policy["spec"]["policyTypes"] == ["Ingress", "Egress"] and "ingress" not in policy["spec"]
    spec = pod["spec"]
    assert spec["automountServiceAccountToken"] is False and spec["restartPolicy"] == "Never"
    assert spec["securityContext"]["runAsNonRoot"] is True
    container = spec["containers"][0]
    assert container["image"] == cli.render.RUNTIMES["go"]["image"]
    assert container["securityContext"] == {"allowPrivilegeEscalation": False, "readOnlyRootFilesystem": True,
                                            "capabilities": {"drop": ["ALL"]}}
    assert container["resources"]["limits"]["memory"] == container["resources"]["requests"]["memory"] == "512Mi"
    assert {"name": "GOCACHE", "value": "/tmp/go-cache"} in container["env"]
    assert {"name": "work", "mountPath": "/work", "readOnly": True} in container["volumeMounts"]


def test_files_are_read_from_the_workspace_never_through_a_symlink(tmp_path):
    (tmp_path / "a.py").write_text("print(1)")
    (tmp_path / "link.py").symlink_to(tmp_path / "a.py")
    assert cli.program_files(tmp_path, ["a.py"]) == {"a.py": "print(1)"}
    with pytest.raises(cli.LabError, match="link.py"):
        cli.program_files(tmp_path, ["link.py"])
    with pytest.raises(cli.LabError, match="missing.py"):
        cli.program_files(tmp_path, ["missing.py"])
    (tmp_path / "big.py").write_bytes(b"x" * (cli.PROGRAM_MAX_BYTES + 1))
    with pytest.raises(cli.LabError, match="exceed"):
        cli.program_files(tmp_path, ["big.py"])


class Clock:
    def __init__(self):
        self.t = 0.0

    def __call__(self):
        return self.t

    def sleep(self, s):
        self.t += s


def fake_pod(monkeypatch, states):
    """kubectl get pod answers each state in turn (then the last one forever)."""
    states = list(states)

    def run_in(name, *cmd):
        state = states.pop(0) if len(states) > 1 else states[0]
        body = {"status": {"phase": "Running", "containerStatuses": [{"state": state}]}}
        return subprocess.CompletedProcess(cmd, 0, json.dumps(body), "")
    monkeypatch.setattr(cli, "run_in", run_in)


def test_waiting_counts_from_the_containers_start(monkeypatch):
    clock = Clock()
    fake_pod(monkeypatch, [{"waiting": {"reason": "ContainerCreating"}}] * 20 + [{"running": {}}] * 3
             + [{"terminated": {"exitCode": 0, "reason": "Completed"}}])
    got = cli.program_wait("a-cp1", "sb-1", "program-1", 10, clock, clock.sleep)
    assert got == {"exit": 0, "seconds": pytest.approx(0.9), "timed_out": 0, "oom": 0}


def test_a_program_running_too_long_times_out_and_one_out_of_memory_says_so(monkeypatch):
    clock = Clock()
    fake_pod(monkeypatch, [{"running": {}}])
    assert cli.program_wait("a-cp1", "sb-1", "p", 2, clock, clock.sleep)["timed_out"] == 1
    fake_pod(monkeypatch, [{"terminated": {"exitCode": 137, "reason": "OOMKilled"}}])
    assert cli.program_wait("a-cp1", "sb-1", "p", 2, clock, clock.sleep) == {"exit": 137, "seconds": 0.0,
                                                                           "timed_out": 0, "oom": 1}


def test_a_pod_that_never_starts_is_an_error(monkeypatch):
    clock = Clock()
    fake_pod(monkeypatch, [{"waiting": {"reason": "ImagePullBackOff", "message": "pull failed"}}])
    with pytest.raises(cli.LabError, match="did not start in 120s: pull failed"):
        cli.program_wait("a-cp1", "sb-1", "p", 2, clock, clock.sleep)


def test_a_named_program_step_records_its_metrics_and_output(topo, monkeypatch, tmp_path):
    (tmp_path / "judge.py").write_text("print(3)")
    (tmp_path / "solution.py").write_text("")
    applied, deleted = [], []
    monkeypatch.setattr(cli.subprocess, "run", lambda cmd, **kw: applied.append((cmd, json.loads(kw["input"])))
                        or subprocess.CompletedProcess(cmd, 0, "", ""))
    monkeypatch.setattr(cli, "program_wait", lambda *a: {"exit": 0, "seconds": 0.4, "timed_out": 0, "oom": 0})

    def run_in(name, *cmd):
        if "delete" in cmd:
            deleted.append(cmd)
        return subprocess.CompletedProcess(cmd, 0, "case 1 ok\n3\n" if "logs" in cmd else "", "")
    monkeypatch.setattr(cli, "run_in", run_in)
    run = cli.ScenarioRun(topo, tmp_path, {"OPSFORGE_NAMESPACE": "sb-1"})
    run.log = lambda phase, text: None
    run.step("action", {"program": PROGRAM, "name": "judge"})
    assert run.metrics == {"judge.exit": 0, "judge.seconds": 0.4, "judge.timed_out": 0, "judge.oom": 0,
                           "judge.value": 3.0}
    assert run.outputs == {"judge": "case 1 ok\n3\n"}
    cmd, doc = applied[0]
    assert cmd[:4] == ["incus", "exec", "a-cp1", "--"] and cmd[-4:] == ["-n", "sb-1", "-f", "-"]
    assert doc["items"][0]["data"] == {"judge.py": "print(3)", "solution.py": ""}
    assert deleted and deleted[0][-3] == doc["items"][2]["metadata"]["name"], "the pod and its files are deleted"


def test_a_program_needs_a_sandbox_namespace(topo, tmp_path):
    run = cli.ScenarioRun(topo, tmp_path)
    run.log = lambda phase, text: None
    run.env.pop("OPSFORGE_NAMESPACE", None)
    with pytest.raises(cli.LabError, match="sandbox's namespace"):
        run.step("measure", {"program": PROGRAM})


def test_runtimes_are_pulled_by_the_nodes_of_documents_that_list_them(topo):
    def images(topo):
        return cli.render.render_inventory(topo, "ssh_config", "state")["all"]["children"]["lab"]["vars"][
            "lab_runtime_images"]
    assert images(topo) == [cli.render.RUNTIMES[r]["image"]
                                                              for r in ("python", "javascript", "go")]
    del topo["runtimes"]
    assert images(topo) == []


def test_runtimes_need_a_k3s_server(topo):
    topo["nodes"] = {"mon": topo["nodes"]["mon"]}
    assert "runtimes: program runtimes run on K3s: the document has no k3s-server" in cli.topology_errors(topo)


def test_a_sandbox_can_require_runtimes(topo):
    state = {"layout": "stretched", "topology": "topology.yml"}
    assert cli.sandboxes.unmet({"needs": ["k3s"], "runtimes": ["go"]}, True, state, topo) == []
    topo["runtimes"] = ["python"]
    assert cli.sandboxes.unmet({"runtimes": ["go"]}, True, state, topo) == [
        "runtime go (not in the runtimes of topology.yml)"]
