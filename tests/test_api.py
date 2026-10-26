"""OpsForge's machine interface (contract/README.md): `opsforge api`. The contract's golden documents
(contract/golden/) are what OpsForge publishes and Academy tests against. Regenerate them with
OPSFORGE_UPDATE_GOLDEN=1 and review the diff. No Incus needed."""

import json
import os
import re
import subprocess

import jsonschema
import pytest

import contract
from conftest import ROOT, cli

CONTRACT = ROOT / "contract"


def golden(name, doc):
    """Compares `doc` with contract/golden/<name>, rewriting the file under OPSFORGE_UPDATE_GOLDEN=1."""
    path = CONTRACT / "golden" / name
    text = json.dumps(doc, indent=2, sort_keys=True) + "\n"
    if os.environ.get("OPSFORGE_UPDATE_GOLDEN"):
        path.write_text(text)
    assert json.loads(path.read_text()) == doc, f"{name} is out of date (OPSFORGE_UPDATE_GOLDEN=1, review the diff)"


# The CLI contract: JSON on stdout, problems on stderr


def run_cli(*args, env=None, input=""):
    return subprocess.run([str(ROOT / "opsforge"), "api", *args], capture_output=True, text=True,
                          env={**os.environ, "SOURCE_DATE_EPOCH": "1792800000", **(env or {})}, cwd=ROOT, input=input)


def test_cli_problem_document_on_stderr():
    out = run_cli("validate-steps", "--json", input="not json")
    assert out.returncode == 1 and out.stdout == ""
    problem = json.loads(out.stderr)
    assert problem["contract"] == contract.CONTRACT and problem["type"] == "/problems/bad-input"


def test_cli_version():
    out = run_cli("version", "--json")
    doc = contract_doc("versionOutput", json.loads(out.stdout))
    assert doc == {"contract": contract.CONTRACT, "minor": contract.CONTRACT_MINOR, "opsforge": cli.VERSION}
    golden("version.json", doc)


# State: the documents contract/golden/state.*.json, from a fake Incus


def fake_lab(monkeypatch, *, running, live=None, pre_fault=None):
    topo = cli.load_topology()
    names = ["wan-core", *(f"{s}-gw" for s in topo["sites"]), *topo["nodes"]]
    monkeypatch.setattr(cli, "lab_instances", lambda: [{"name": n, "status": "Running"} for n in names]
                        if running else [])
    monkeypatch.setattr(cli, "read_link_state", lambda: {"links": live or topo["links"]})
    monkeypatch.setattr(cli, "read_pre_fault", lambda: pre_fault)
    monkeypatch.setattr(cli, "TOPOLOGY", ROOT / "topology.yml")
    monkeypatch.setattr(cli, "load_valid_topology", lambda: topo)
    return topo


def test_state_lab_down(monkeypatch):
    fake_lab(monkeypatch, running=False)
    state = cli.api_state()
    assert state["contract"] == contract.CONTRACT and state["up"] is False
    assert {n["status"] for n in state["nodes"]} == {"Missing"}
    golden("state.down.json", state)


def test_state_lab_up_with_a_partition(monkeypatch):
    topo = cli.load_topology()
    live = {name: ({**link, "loss": "100%"} if "c" in name.split("-") else link) for name, link in topo["links"].items()}
    fake_lab(monkeypatch, running=True, live=live, pre_fault={"links": topo["links"]})
    state = cli.api_state()
    assert state["up"] is True and state["fault"] is True and state["error"] is None
    assert {k: v["condition"] for k, v in state["links"].items()} == {"a-b": "ok", "a-c": "cut", "b-c": "cut"}
    golden("state.up.json", state)


# run, probe, heal: scenario steps on the lab. Host `run` steps need no Incus, so these run the real step runner.

TOOLCHAIN_SCHEMA = json.loads((CONTRACT / "toolchain.schema.json").read_text())


def strict(node):
    """The schema with every object closed: readers ignore unknown output fields, but OpsForge prints none."""
    if isinstance(node, dict):
        out = {k: strict(v) for k, v in node.items()}
        if out.get("type") == "object" and "properties" in out:
            out.setdefault("additionalProperties", False)
        return out
    return [strict(v) for v in node] if isinstance(node, list) else node


def contract_doc(kind, doc):
    """`doc` checked against the toolchain schema's $defs/<kind>, closed (an undocumented output field fails)."""
    jsonschema.validate(doc, {**strict(TOOLCHAIN_SCHEMA), "$ref": f"#/$defs/{kind}"})
    return doc


def timeless(doc):
    """Timings zeroed (log stamps, *.seconds metrics), for golden documents."""
    doc = json.loads(json.dumps(doc))
    if "metrics" in doc:
        doc["metrics"] = {k: 0.0 if k.endswith(".seconds") else v for k, v in doc["metrics"].items()}
    doc["log"] = [re.sub(r"seconds=[\d.e+-]+", "seconds=0", re.sub(r"^\s*[\d.]+s", "  0.0s", line))
                  for line in doc["log"]]
    return doc


def echo(name, value):
    return {"name": name, "run": {"cmd": f"echo {value}"}}


def test_with_namespace_fills_the_placeholder_everywhere():
    steps = [{"run": {"cmd": "kubectl -n ${ns} get pods", "host": "host"}}, {"sleep": "1s"}]
    assert contract.with_namespace(steps, "lab-7") == [{"run": {"cmd": "kubectl -n lab-7 get pods", "host": "host"}},
                                                      {"sleep": "1s"}]
    assert contract.with_namespace(steps[1:], None) == steps[1:]
    with pytest.raises(ValueError, match=r"\$\{ns\}"):
        contract.with_namespace(steps, None)


@pytest.mark.parametrize("kind, doc, message", [
    ("runInput", {"contract": 1, "phase": "check", "steps": []}, "phase"),
    ("runInput", {"contract": 1, "phase": "setup", "steps": [], "namespace": "kube-system"}, "namespace"),
    ("runInput", {"contract": 1, "phase": "setup", "steps": [], "attempt": 3}, "attempt"),
    ("probeInput", {"contract": 1, "steps": [echo("a", 1)], "expect": {}}, "expect"),
])
def test_input_errors(kind, doc, message):
    errs = contract.input_errors(kind, doc)
    assert errs and message in " ".join(errs), errs


def test_run_reports_metrics_and_log():
    doc = contract_doc("runOutput", cli.api_run(
        {"contract": 1, "phase": "setup", "steps": [echo("x", 42), {"run": {"cmd": "true"}}]}))
    assert doc["error"] is None and doc["metrics"]["x.value"] == 42 and doc["metrics"]["x.exit"] == 0
    assert doc["log"][0].endswith("s  run cmd=echo 42  -> x")
    golden("run.golden.json", timeless(doc))


def test_run_stops_at_a_failing_step():
    doc = contract_doc("runOutput", cli.api_run(
        {"contract": 1, "phase": "setup", "steps": [{"run": {"cmd": "echo ${node:zz}"}}, echo("never", 1)]}))
    assert "no K3s node in site 'zz'" in doc["error"] and doc["metrics"] == {}


def test_probe_passes():
    doc = contract_doc("probeOutput", cli.api_probe(
        {"contract": 1, "steps": [echo("a", 1), echo("b", 2)], "expect": {"a.value": "== 1", "b.value": [">= 2", "<= 2"]}}))
    assert doc["passed"] is True and [r["passed"] for r in doc["results"]] == [True, True, True]
    golden("probe.passed.json", timeless(doc))


def test_probe_stops_at_the_first_failing_named_step():
    doc = contract_doc("probeOutput", cli.api_probe(
        {"contract": 1, "steps": [echo("a", 1), echo("b", 2)], "expect": {"a.value": "== 0", "b.value": "== 2"}}))
    assert doc["passed"] is False
    assert [(r["metric"], r["got"], r["passed"], r["checked"]) for r in doc["results"]] == [
        ("a.value", 1, False, True), ("b.value", None, False, False)]
    golden("probe.failed.json", timeless(doc))


@pytest.fixture
def kube(monkeypatch):
    """Records the commands run in instances; namespaces are made with `k3s kubectl` on each cluster's server."""
    calls = []

    def run_in(name, *cmd):
        calls.append((name, " ".join(cmd)))
        return subprocess.CompletedProcess(cmd, 0, "", "")

    monkeypatch.setattr(cli, "run_in", run_in)
    return calls


def test_setup_creates_the_namespace_first_and_teardown_deletes_it_last(kube):
    marker = {"run": {"host": "a-gw", "cmd": "step -n ${ns}"}}
    assert cli.api_run({"contract": 1, "phase": "setup", "steps": [marker], "namespace": "lab-7"})["error"] is None
    assert cli.api_run({"contract": 1, "phase": "teardown", "steps": [marker], "namespace": "lab-7"})["error"] is None
    cmds = [c for _, c in kube]
    assert "create namespace lab-7" in cmds[0] and "step -n lab-7" in cmds[1]
    assert "step -n lab-7" in cmds[2] and "delete namespace lab-7" in cmds[3] and len(cmds) == 4
    assert kube[0][0] == cli.render.k3s_clusters(cli.load_valid_topology())["lab"]["server"]


def test_teardown_deletes_the_namespace_even_after_a_failing_step(kube):
    doc = cli.api_run({"contract": 1, "phase": "teardown", "steps": [{"run": {"cmd": "echo ${node:zz}"}}],
                           "namespace": "lab-7"})
    assert "zz" in doc["error"] and "delete namespace lab-7" in kube[-1][1]


def test_heal(monkeypatch):
    monkeypatch.setattr(cli, "heal_links", lambda: True)
    golden("heal.json", contract_doc("healOutput", cli.api_heal()))
    monkeypatch.setattr(cli, "heal_links", lambda: False)
    assert cli.api_heal()["healed"] is False


def test_cli_probe_reads_its_input_on_stdin():
    out = run_cli("probe", "--json", input=json.dumps(
        {"contract": 1, "steps": [echo("a", 41)], "expect": {"a.value": "> 40"}}))
    assert out.returncode == 0, out.stderr
    assert contract_doc("probeOutput", json.loads(out.stdout))["passed"] is True


@pytest.mark.parametrize("stdin, slug", [
    ("not json", "bad-input"),
    (json.dumps({"contract": 2, "phase": "setup", "steps": []}), "contract-mismatch"),
    (json.dumps({"contract": 1, "phase": "setup", "steps": [], "namespace": "default"}), "bad-input"),
])
def test_cli_rejects_bad_input(stdin, slug):
    out = run_cli("run", "--json", input=stdin)
    assert out.returncode == 1 and out.stdout == ""
    assert json.loads(out.stderr)["type"] == f"/problems/{slug}"


# validate-steps: scenario steps checked without running them (Academy's course compiler sends every lab's)


def test_validate_steps_reports_each_set():
    doc = contract_doc("stepsOutput", cli.api_validate_steps({"contract": 1, "sets": [
        {"steps": [{"sleep": "1s"}, echo("a", 1)], "expect": {"a.value": "== 1"}},
        {"steps": [{"pign": {"from": "a-gw"}}, {"degrade": {"rtt": "10ms"}}]},
        {"steps": [{"partition": "c"}, echo("a", 1)], "expect": {"b.value": "=> 1"}, "faults": False},
    ]}))
    assert [(e["set"], e["message"]) for e in doc["errors"]] == [
        (1, "steps[0]: unknown step 'pign' (one of " + ", ".join(cli.scenario.STEPS) + ")"),
        (1, "steps[1]: degrade needs 'link'"),
        (2, "steps[0]: partition: fault steps are not allowed here (only a lab's setup and teardown change the lab)"),
        (2, "expect: b.value: no step is named 'b'"),
        (2, "expect: b.value: condition '=> 1' is not '<op> <value>' (op: <, <=, >, >=, ==, !=)"),
    ]
    golden("validate-steps.json", doc)


def test_validate_steps_allows_faults_by_default():
    assert cli.api_validate_steps({"contract": 1, "sets": [{"steps": [{"partition": "c"}]}]})["errors"] == []


def test_cli_validate_steps_reads_its_input_on_stdin():
    out = run_cli("validate-steps", "--json", input=json.dumps({"contract": 1, "sets": [{"steps": [{"nope": 1}]}]}))
    assert out.returncode == 0, out.stderr
    assert contract_doc("stepsOutput", json.loads(out.stdout))["errors"][0]["message"].startswith("steps[0]: unknown")
    out = run_cli("validate-steps", "--json", input=json.dumps({"contract": 1, "sets": [{"steps": [], "extra": 1}]}))
    assert out.returncode == 1 and json.loads(out.stderr)["type"] == "/problems/bad-input"
