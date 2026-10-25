"""OpsForge's machine interface (contract/README.md): `opsforge api` and the course compiler it still hosts
(lab/academy.py, until Academy compiles packs itself). The contract's golden documents (contract/golden/) are what
OpsForge publishes and Academy tests against; the golden bundle (academy/testdata/) feeds Academy's import tests.
Regenerate them with OPSFORGE_UPDATE_GOLDEN=1 and review the diff. No Incus needed."""

import json
import os
import re
import shutil
import subprocess

import jsonschema
import pytest
import yaml

import academy
import labs
from conftest import ROOT, cli

TESTDATA = ROOT / "academy" / "testdata"
CONTRACT = ROOT / "contract"
DEMO = TESTDATA / "packs" / "demo"
BUNDLE_SCHEMA = json.loads((ROOT / "academy" / "api" / "bundle.schema.json").read_text())
EPOCH = "2026-10-24T00:00:00Z"


def golden(name, doc):
    """Compares `doc` with contract/golden/<name> (academy/testdata/ for the bundle), rewriting the file under
    OPSFORGE_UPDATE_GOLDEN=1."""
    path = (TESTDATA if name.startswith("bundle.") else CONTRACT / "golden") / name
    text = json.dumps(doc, indent=2, sort_keys=True) + "\n"
    if os.environ.get("OPSFORGE_UPDATE_GOLDEN"):
        path.write_text(text)
    assert json.loads(path.read_text()) == doc, f"{name} is out of date (OPSFORGE_UPDATE_GOLDEN=1, review the diff)"


def compile_(*paths):
    return academy.compile_bundle(list(paths), ROOT, EPOCH)


@pytest.fixture
def pack(tmp_path):
    """A writable copy of the demo pack."""
    dst = tmp_path / "demo"
    shutil.copytree(DEMO, dst)
    return dst


def edit(path, change):
    doc = yaml.safe_load(path.read_text())
    change(doc)
    path.write_text(yaml.safe_dump(doc, sort_keys=False))


def messages(bundle):
    return [e["message"] for e in bundle["errors"]]


# Compile: the golden bundle, the contract schema, determinism


def test_demo_pack_compiles_to_the_golden_bundle():
    bundle = compile_(DEMO)
    assert bundle["errors"] == []
    golden("bundle.golden.json", bundle)


def test_bundles_match_the_contract_schema():
    for bundle in (compile_(DEMO), academy.compile_bundle(labs.find_packs(ROOT / "labs"), ROOT, EPOCH)):
        jsonschema.Draft202012Validator(BUNDLE_SCHEMA).validate(bundle)


def test_the_repo_packs_compile_without_errors():
    bundle = academy.compile_bundle(labs.find_packs(ROOT / "labs"), ROOT, EPOCH)
    assert bundle["errors"] == []
    assert [p["id"] for p in bundle["packs"]] == ["core"]
    assert len(bundle["packs"][0]["labs"]) == 6


def test_compile_is_deterministic():
    assert json.dumps(compile_(DEMO), sort_keys=True) == json.dumps(compile_(DEMO), sort_keys=True)


def test_compiled_items_carry_defaults_and_durations():
    p = compile_(DEMO)["packs"][0]
    course = p["courses"][0]
    assert [(i["type"], i["id"]) for i in course["sections"][0]["items"]] == [
        ("lecture", "what-is-a-pod"), ("quiz", "pods-check"), ("lab", "meet-the-cluster"), ("challenge", "first-pod")]
    assert course["minutes"] == course["sections"][0]["minutes"] == 8 + 5 + 10 + 10
    quiz = p["quizzes"][0]
    assert (quiz["pass"], quiz["xp"], quiz["shuffle"], quiz["time_limit_s"], quiz["draw"]) == (70, 20, True, None, None)
    assert [q["answer"] for q in quiz["questions"]] == [[1], [0, 1, 3]]
    assert [q["type"] for q in quiz["questions"]] == ["single", "multiple"]
    ch = p["challenges"][0]
    assert ch["time_limit_s"] == 60 and ch["tests"][0]["action"] == []
    assert set(ch["starter"]) == {"pod.yaml"} and "traefik/whoami" in ch["solution"]["pod.yaml"]
    lab = p["labs"][0]
    assert lab["files"] == {"hello.yaml": (DEMO / "labs/meet-the-cluster/files/hello.yaml").read_text()}
    assert p["assets"][0]["path"] == "pod.svg" and p["assets"][0]["bytes"] > 0
    assert "${node:c}" in p["lectures"][0]["markdown"], "texts keep their placeholders raw"


def test_phase10_lab_hashes_are_kept():
    """Labs without files hash as in Phase 10, so passes recorded there are not flagged as stale."""
    bundle = academy.compile_bundle(labs.find_packs(ROOT / "labs"), ROOT, EPOCH)
    doc = labs.load_pack(ROOT / "labs" / "core")
    assert {lab["id"]: lab["content_hash"] for lab in bundle["packs"][0]["labs"]} == {
        lid: labs.content_hash(lab) for lid, lab in doc["labs"].items()}


def test_content_hashes_follow_the_content(pack):
    before = compile_(pack)["packs"][0]
    md = pack / "courses/k8s-intro/lectures/what-is-a-pod.md"
    md.write_text(md.read_text().replace("network namespace", "network namespace and volumes"))
    after = compile_(pack)["packs"][0]
    assert after["lectures"][0]["content_hash"] != before["lectures"][0]["content_hash"]
    assert after["quizzes"][0]["content_hash"] == before["quizzes"][0]["content_hash"]
    assert after["courses"][0]["sections"][0]["items"][0]["content_hash"] == after["lectures"][0]["content_hash"]
    assert after["content_hash"] != before["content_hash"]


def test_lab_files_count_in_its_hash(pack):
    before = compile_(pack)["packs"][0]["labs"][0]["content_hash"]
    (pack / "labs/meet-the-cluster/files/hello.yaml").write_text("kind: ConfigMap\n")
    assert compile_(pack)["packs"][0]["labs"][0]["content_hash"] != before


# Compile: validation errors (one row per rule)

COURSE = "courses/k8s-intro/course.yml"
QUIZ = "courses/k8s-intro/quizzes/pods-check.yml"
CHALLENGE = "courses/k8s-intro/challenges/first-pod/challenge.yml"
LAB = "labs/meet-the-cluster.yml"


def _item(kind, ref):
    return lambda d: d["sections"][0]["items"].append({kind: ref})


ERROR_CASES = [
    ("unknown lab", COURSE, _item("lab", "nope"), "unknown lab 'nope'"),
    ("unknown lecture", COURSE, _item("lecture", "nope"), "unknown lecture 'nope'"),
    ("duplicate item", COURSE, _item("quiz", "pods-check"), "duplicate item 'pods-check'"),
    ("unknown badge", COURSE, lambda d: d.update(badge="nope"), "unknown badge 'nope'"),
    ("needs too narrow", COURSE, lambda d: d.update(needs=[]), "meet-the-cluster needs k3s"),
    ("unknown level", COURSE, lambda d: d.update(level="expert"), "'expert' is not one of"),
    ("unknown requires", COURSE, lambda d: d.update(requires=["nope"]), "requires unknown course 'nope'"),
    ("answer out of range", QUIZ, lambda d: d["questions"][0].update(answer=7), "shared: answer 7 is not a choice"),
    ("single with a list", QUIZ, lambda d: d["questions"][0].update(answer=[0, 1]),
     "shared: a single-answer question takes one index"),
    ("unknown see", QUIZ, lambda d: d["questions"][0].update(see="nope"), "shared: see: unknown lecture 'nope'"),
    ("duplicate question", QUIZ, lambda d: d["questions"].append(dict(d["questions"][0])), "duplicate question 'shared'"),
    ("no explanation", QUIZ, lambda d: d["questions"][0].pop("explain"), "'explain' is a required property"),
    ("draw too many", QUIZ, lambda d: d.update(draw=5), "draw 5 is more than the 2 questions"),
    ("missing starter file", CHALLENGE, lambda d: d.update(files=["missing.yaml"]),
     "missing.yaml is not in starter/"),
    ("fault in a test", CHALLENGE, lambda d: d["tests"][0]["probe"]["steps"].insert(0, {"partition": "c"}),
     "a check only observes the lab"),
    ("fault in an action", CHALLENGE, lambda d: d["tests"][0].update(action=[{"heal": None}]),
     "pod is ready: action: heal: grader actions may change only the challenge namespace"),
    ("lab open not in files", LAB, lambda d: d["steps"][1].update(open="other.yaml"),
     "apply: opens other.yaml, which is not in files"),
    ("lab file missing", LAB, lambda d: d.update(files=["hello.yaml", "gone.yaml"]),
     "gone.yaml is not in labs/meet-the-cluster/files/"),
]


@pytest.mark.parametrize("rel,change,expected", [c[1:] for c in ERROR_CASES], ids=[c[0] for c in ERROR_CASES])
def test_compile_reports_errors(pack, rel, change, expected):
    edit(pack / rel, change)
    bundle = compile_(pack)
    assert bundle["packs"] == [], "an invalid pack is left out of the bundle"
    assert any(expected in m for m in messages(bundle)), messages(bundle)
    assert all(e["pack"] == "demo" for e in bundle["errors"])


def test_lecture_images_must_be_pack_assets(pack):
    md = pack / "courses/k8s-intro/lectures/what-is-a-pod.md"
    text = md.read_text()
    for image, expected in (("../../../assets/missing.svg", "missing.svg is not a file in assets/"),
                            ("../../../pack.yml", "is outside assets/"),
                            ("https://example.com/x.png", "is outside assets/")):
        md.write_text(text.replace("../../../assets/pod.svg", image))
        assert any(expected in m for m in messages(compile_(pack))), image


def test_lecture_front_matter_is_required(pack):
    md = pack / "courses/k8s-intro/lectures/what-is-a-pod.md"
    md.write_text("No front matter here.\n")
    assert any("what-is-a-pod.md: needs YAML front matter" in m for m in messages(compile_(pack)))


def test_broken_yaml_is_an_error_not_a_crash(pack):
    (pack / QUIZ).write_text("title: [unclosed\n")
    assert any("pods-check.yml" in m for m in messages(compile_(pack)))


def test_ids_are_unique_across_packs(pack, tmp_path):
    other = tmp_path / "other"
    shutil.copytree(pack, other)
    edit(other / "pack.yml", lambda d: d.update(id="other"))
    errs = messages(compile_(pack, other))
    assert any("course 'k8s-intro' is also in pack demo" in m for m in errs)
    assert any("lab 'meet-the-cluster' is also in pack demo" in m for m in errs)


def test_sections_may_use_labs_of_other_packs(pack):
    edit(pack / COURSE, _item("lab", "private-networks"))
    edit(pack / COURSE, lambda d: d.update(needs=["k3s", "vpn"]))
    bundle = compile_(pack, ROOT / "labs" / "core")
    assert bundle["errors"] == [], messages(bundle)


# The CLI contract: JSON on stdout, problems on stderr


def run_cli(*args, env=None, input=""):
    return subprocess.run([str(ROOT / "opsforge"), "api", *args], capture_output=True, text=True,
                          env={**os.environ, "SOURCE_DATE_EPOCH": "1792800000", **(env or {})}, cwd=ROOT, input=input)


def test_cli_compile_prints_the_bundle():
    out = run_cli("compile", str(DEMO), "--json")
    assert out.returncode == 0, out.stderr
    bundle = json.loads(out.stdout)
    assert bundle["contract"] == academy.CONTRACT and bundle["compiled_at"] == "2026-10-24T00:00:00Z"
    assert bundle["packs"][0]["id"] == "demo" and bundle["packs"][0]["path"] == "academy/testdata/packs/demo"


def test_cli_compile_defaults_to_the_repo_packs():
    out = run_cli("compile")
    assert out.returncode == 0, out.stderr
    assert "1 pack(s), 0 course(s), 6 lab(s): no errors" in out.stdout


def test_cli_problem_document_on_stderr(tmp_path):
    out = run_cli("compile", str(tmp_path / "nowhere"), "--json")
    assert out.returncode == 1 and out.stdout == ""
    problem = json.loads(out.stderr)
    assert problem["contract"] == academy.CONTRACT and problem["type"] == "/problems/not-a-pack"
    assert "nowhere" in problem["detail"]


def test_cli_version():
    out = run_cli("version", "--json")
    doc = contract_doc("versionOutput", json.loads(out.stdout))
    assert doc == {"contract": academy.CONTRACT, "minor": academy.CONTRACT_MINOR, "opsforge": cli.VERSION}
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
    assert state["contract"] == academy.CONTRACT and state["up"] is False
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
    assert academy.with_namespace(steps, "lab-7") == [{"run": {"cmd": "kubectl -n lab-7 get pods", "host": "host"}},
                                                      {"sleep": "1s"}]
    assert academy.with_namespace(steps[1:], None) == steps[1:]
    with pytest.raises(ValueError, match=r"\$\{ns\}"):
        academy.with_namespace(steps, None)


@pytest.mark.parametrize("kind, doc, message", [
    ("runInput", {"contract": 1, "phase": "check", "steps": []}, "phase"),
    ("runInput", {"contract": 1, "phase": "setup", "steps": [], "namespace": "kube-system"}, "namespace"),
    ("runInput", {"contract": 1, "phase": "setup", "steps": [], "attempt": 3}, "attempt"),
    ("probeInput", {"contract": 1, "steps": [echo("a", 1)], "expect": {}}, "expect"),
])
def test_input_errors(kind, doc, message):
    errs = academy.input_errors(kind, doc)
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
