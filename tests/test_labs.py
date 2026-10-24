"""Teaching labs (Phase 10): pack validation (lab/labs.py), the database's import, progression and badge rules
(lab/labsdb.py), and the HTTP API with a fake lab (lab/labserver.py). No Incus needed."""

import copy
import http.client
import json
import threading
import types

import pytest

import labs
import labsdb
import labserver
import scenario
from conftest import ROOT, cli


def mini_pack():
    """Two levels: l1 (quiz) then l2 (confirm, requires l1); badges for the first pass and the curriculum."""
    return {
        "pack": {"id": "mini", "title": "Mini", "version": 1, "hint_penalty": 0.1},
        "curricula": {"basics": {"title": "Basics", "levels": [
            {"title": "One", "labs": ["l1"]},
            {"title": "Two", "labs": ["l2"]},
        ]}},
        "labs": {
            "l1": {"title": "Lab one", "summary": "s", "briefing": "b", "xp": 100, "steps": [
                {"id": "q", "title": "Q", "body": "Which?", "hints": ["h1", "h2"],
                 "check": {"quiz": {"question": "?", "choices": ["no", "yes"], "answer": 1, "explain": "e"}}},
            ]},
            "l2": {"title": "Lab two", "summary": "s", "briefing": "b", "xp": 50, "requires": ["l1"], "steps": [
                {"id": "c", "title": "C", "body": "Do it", "check": {"confirm": "Done"}},
            ]},
        },
        "badges": [
            {"id": "first", "title": "First", "description": "d", "rule": {"type": "labs_passed", "min": 1}},
            {"id": "all", "title": "All", "description": "d",
             "rule": {"type": "curriculum_complete", "curriculum": "basics"}},
        ],
    }


@pytest.fixture
def db():
    conn = labsdb.connect(":memory:")
    labsdb.import_pack(conn, mini_pack(), "mini")
    return conn, labsdb.local_user(conn)


def pass_lab(conn, user, lab_id, hints=0):
    attempt = labsdb.start_attempt(conn, user, lab_id)
    step = labsdb.get_lab(conn, lab_id)["body"]["steps"][0]["id"]
    for i in range(hints):
        labsdb.show_hint(conn, attempt, step, i)
    labsdb.record_check(conn, attempt, step, True, {"passed": True})
    return labsdb.finish(conn, attempt)


def dump(conn):
    tables = [r[0] for r in conn.execute("SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name")]
    return {t: [tuple(r) for r in conn.execute(f"SELECT * FROM {t} ORDER BY 1, 2")] for t in tables}


# Pack validation


def test_core_pack_is_valid():
    for path in labs.find_packs(ROOT / "labs"):
        assert labs.errors(labs.load_pack(path)) == [], path


def test_mini_pack_is_valid():
    assert labs.errors(mini_pack()) == []


@pytest.mark.parametrize("edit, message", [
    (lambda d: d["curricula"]["basics"]["levels"][0]["labs"].append("nope"), "unknown lab 'nope'"),
    (lambda d: d["labs"]["l2"]["requires"].append("ghost"), "requires unknown lab 'ghost'"),
    (lambda d: d["labs"]["l1"]["steps"].append(copy.deepcopy(d["labs"]["l1"]["steps"][0])), "duplicate step id 'q'"),
    (lambda d: d["labs"]["l1"]["steps"][0]["check"]["quiz"].update(answer=5), "answer must be a choice index"),
    (lambda d: d["labs"]["l2"]["steps"][0].update(check={"probe": {"steps": [{"partition": "c"}], "expect": {}}}),
     "a check only observes the lab"),
    (lambda d: d["labs"]["l2"]["steps"][0].update(check={"probe": {"steps": [{"run": {"cmd": "true"}, "name": "x"}],
                                                                   "expect": {"y.exit": "== 0"}}}),
     "no step is named 'y'"),
    (lambda d: d["labs"]["l2"]["steps"][0].update(check={"telepathy": {}}), "unknown check 'telepathy'"),
    (lambda d: d["labs"]["l1"].update(setup=[{"teleport": {}}]), "setup: setup[0]: unknown step 'teleport'"),
    (lambda d: d["badges"].append({"id": "x", "title": "X", "description": "d", "rule": {"type": "vibes"}}),
     "unknown rule type 'vibes'"),
    (lambda d: d["badges"].append({"id": "x", "title": "X", "description": "d",
                                   "rule": {"type": "lab_passed", "lab": "l1", "colour": "red"}}),
     "lab_passed has no 'colour'"),
    (lambda d: d["badges"].append({"id": "x", "title": "X", "description": "d",
                                   "rule": {"type": "curriculum_complete", "curriculum": "advanced"}}),
     "unknown curriculum 'advanced'"),
    (lambda d: d["labs"]["l1"].update(layout="mesh"), "labs/l1/layout"),
])
def test_pack_errors(edit, message):
    doc = mini_pack()
    edit(doc)
    errs = labs.errors(doc)
    assert any(message in e for e in errs), errs


def test_curricula_may_use_labs_of_other_packs():
    doc = mini_pack()
    doc["curricula"]["basics"]["levels"][0]["labs"].append("elsewhere")
    assert labs.errors(doc, {"labs": {"elsewhere"}, "curricula": set()}) == []


def test_content_hash_ignores_key_order():
    assert labs.content_hash({"a": 1, "b": [1, 2]}) == labs.content_hash({"b": [1, 2], "a": 1})
    assert labs.content_hash({"a": 1}) != labs.content_hash({"a": 2})


def test_pass_xp_loses_hints_but_never_below_half():
    assert labs.pass_xp(100, 0, 0.1) == 100
    assert labs.pass_xp(100, 2, 0.1) == 80
    assert labs.pass_xp(100, 9, 0.1) == 50


def test_unmet_needs(topo, multi):
    assert labs.unmet_needs({"layout": "stretched", "needs": ["k3s", "vpn", "monitoring", "ci"]}, topo) == []
    assert labs.unmet_needs({"layout": "multi-cluster"}, topo) == ["needs the multi-cluster layout"]
    assert labs.unmet_needs({"layout": None}, topo) == []
    del topo["nodes"]["mon"]
    topo["interconnect"] = "none"
    assert labs.unmet_needs({"needs": ["vpn", "monitoring"]}, topo) == [
        "needs the VPN (interconnect: vpn)", "needs the monitoring node"]
    assert labs.unmet_needs({"layout": "stretched"}, multi) == ["needs the stretched layout"]


def test_substitute_keeps_unresolvable_placeholders(topo):
    assert labs.substitute("ping ${public:c}", topo) == f"ping {topo['sites']['c']['public_ip']}"
    assert labs.substitute("ping ${public:zz}", topo) == "ping ${public:zz}"


# Database: import


def test_migrations_set_the_version():
    conn = labsdb.connect(":memory:")
    assert conn.execute("PRAGMA user_version").fetchone()[0] == labsdb.migrations()[-1][0]
    labsdb.migrate(conn)  # nothing left to apply


def test_import_twice_changes_nothing(db):
    conn, _ = db
    before = dump(conn)
    summary = labsdb.import_pack(conn, mini_pack(), "mini")
    assert summary["changed"] is False and summary["labs"]["unchanged"] == ["l1", "l2"]
    assert dump(conn) == before


def test_import_reports_and_retires(db):
    conn, user = db
    doc = mini_pack()
    doc["labs"]["l1"]["summary"] = "new words"
    del doc["labs"]["l2"]
    doc["curricula"]["basics"]["levels"].pop()
    doc["badges"].pop()
    summary = labsdb.import_pack(conn, doc, "mini")
    assert summary["labs"]["updated"] == ["l1"] and summary["labs"]["retired"] == ["l2"]
    assert [lab["id"] for c in labsdb.catalog(conn, user) for lv in c["levels"] for lab in lv["labs"]] == ["l1"]
    assert [b["id"] for b in labsdb.badges(conn, user)] == ["first"]
    with pytest.raises(ValueError):
        labsdb.start_attempt(conn, user, "l2")


def test_lab_ids_belong_to_one_pack(db):
    conn, _ = db
    other = mini_pack()
    other["pack"]["id"] = "other"
    with pytest.raises(ValueError, match="already belongs to pack mini"):
        labsdb.import_pack(conn, other, "other")


# Database: progression


def levels(conn, user):
    return [(lv["unlocked"], [(lab["id"], lab["status"], lab["locked"]) for lab in lv["labs"]])
            for lv in labsdb.catalog(conn, user)[0]["levels"]]


def test_levels_unlock_in_order(db):
    conn, user = db
    assert levels(conn, user) == [(True, [("l1", "new", False)]), (False, [("l2", "new", True)])]
    assert labsdb.lab_locked(conn, user, "l2")
    pass_lab(conn, user, "l1")
    assert levels(conn, user) == [(True, [("l1", "passed", False)]), (True, [("l2", "new", False)])]


def test_xp_required_opens_a_level_early(db):
    conn, user = db
    doc = mini_pack()
    doc["curricula"]["basics"]["levels"][1]["xp_required"] = 0
    doc["labs"]["l2"]["requires"] = []
    labsdb.import_pack(conn, doc, "mini")
    assert levels(conn, user)[1] == (True, [("l2", "new", False)])


def test_requires_keeps_a_lab_locked(db):
    conn, user = db
    doc = mini_pack()
    doc["curricula"]["basics"]["levels"][1]["xp_required"] = 0
    labsdb.import_pack(conn, doc, "mini")
    lab = labsdb.catalog(conn, user)[0]["levels"][1]["labs"][0]
    assert lab["locked"] and lab["missing"] == ["l1"]


def test_one_lab_runs_at_a_time(db):
    conn, user = db
    labsdb.start_attempt(conn, user, "l1")
    with pytest.raises(ValueError, match="another lab is running"):
        labsdb.start_attempt(conn, user, "l2")
    assert labsdb.catalog(conn, user)[0]["levels"][0]["labs"][0]["status"] == "running"


def test_hints_count_once_and_cost_xp(db):
    conn, user = db
    attempt = labsdb.start_attempt(conn, user, "l1")
    labsdb.show_hint(conn, attempt, "q", 0)
    labsdb.show_hint(conn, attempt, "q", 0)
    labsdb.show_hint(conn, attempt, "q", 1)
    assert labsdb.shown_hints(conn, attempt) == {"q": 2}
    labsdb.record_check(conn, attempt, "q", True, {})
    labsdb.finish(conn, attempt)
    assert labsdb.get_attempt(conn, attempt)["xp"] == 80
    assert labsdb.total_xp(conn, user) == 80


def test_best_pass_counts(db):
    conn, user = db
    pass_lab(conn, user, "l1", hints=2)
    pass_lab(conn, user, "l1")
    pass_lab(conn, user, "l1", hints=1)
    assert labsdb.total_xp(conn, user) == 100


def test_editing_a_lab_flags_old_passes(db):
    conn, user = db
    pass_lab(conn, user, "l1")
    assert labsdb.catalog(conn, user)[0]["levels"][0]["labs"][0]["stale"] is False
    doc = mini_pack()
    doc["labs"]["l1"]["briefing"] = "rewritten"
    labsdb.import_pack(conn, doc, "mini")
    lab = labsdb.catalog(conn, user)[0]["levels"][0]["labs"][0]
    assert lab["status"] == "passed" and lab["stale"] is True
    pass_lab(conn, user, "l1")
    assert labsdb.catalog(conn, user)[0]["levels"][0]["labs"][0]["stale"] is False


# Database: badges


def test_badges_are_awarded_on_pass(db):
    conn, user = db
    assert [b["id"] for b in pass_lab(conn, user, "l1")] == ["first"]
    assert [b["id"] for b in pass_lab(conn, user, "l2")] == ["all"]
    assert pass_lab(conn, user, "l1") == []


def test_a_badge_added_later_is_awarded_from_history(db):
    conn, user = db
    pass_lab(conn, user, "l1")
    doc = mini_pack()
    doc["badges"].append({"id": "clean", "title": "Clean", "description": "d",
                          "rule": {"type": "lab_passed", "lab": "l1", "max_hints": 0}})
    labsdb.import_pack(conn, doc, "mini")
    assert [b["id"] for b in labsdb.award_badges(conn, user)] == ["clean"]
    kinds = [r[0] for r in conn.execute("SELECT kind FROM events ORDER BY id")]
    assert kinds.count("badge.awarded") == 2


@pytest.mark.parametrize("rule, passes, xp, expected", [
    ({"type": "labs_passed", "min": 2}, [("l1", 0, 10)], 0, False),
    ({"type": "labs_passed", "min": 2}, [("l1", 0, 10), ("l1", 0, 10)], 0, False),
    ({"type": "labs_passed", "min": 2}, [("l1", 0, 10), ("l2", 0, 10)], 0, True),
    ({"type": "labs_passed", "min": 1, "max_hints": 0}, [("l1", 1, 10)], 0, False),
    ({"type": "labs_passed", "min": 1, "curriculum": "basics"}, [("x", 0, 10)], 0, False),
    ({"type": "lab_passed", "lab": "l1", "max_seconds": 60}, [("l1", 0, 61)], 0, False),
    ({"type": "lab_passed", "lab": "l1", "max_seconds": 60}, [("l1", 0, 61), ("l1", 3, 30)], 0, True),
    ({"type": "curriculum_complete", "curriculum": "basics"}, [("l1", 0, 1)], 0, False),
    ({"type": "curriculum_complete", "curriculum": "basics"}, [("l1", 0, 1), ("l2", 0, 1)], 0, True),
    ({"type": "curriculum_complete", "curriculum": "empty"}, [], 0, False),
    ({"type": "xp", "min": 100}, [], 99, False),
    ({"type": "xp", "min": 100}, [], 100, True),
])
def test_badge_rules(rule, passes, xp, expected):
    history = {"passes": [{"lab": lab, "hints": h, "seconds": s, "xp": 0} for lab, h, s in passes],
               "curricula": {"basics": ["l1", "l2"]}, "xp": xp}
    assert labs.earned(rule, history) is expected


# HTTP API with a fake lab


class FakeDriver:
    """LabDriver's interface without Incus: the lab is up, probes pass when `probe_passes`."""

    def __init__(self, topo):
        self.topo = topo
        self.busy = threading.Lock()
        self.cli = types.SimpleNamespace(TOPOLOGY=cli.TOPOLOGY)
        self.ran = []

    def topology(self):
        return self.topo

    def state(self):
        return {"up": True, "fault": False, "layout": "stretched", "sites": {}, "links": {}, "nodes": [], "error": None}

    def run(self, steps, phase):
        self.ran.append(phase)
        return {}, [], None

    def heal(self):
        self.ran.append("heal")


@pytest.fixture
def server(tmp_path, topo):
    db_path = tmp_path / "labs.db"
    conn = labsdb.connect(db_path)
    labsdb.import_pack(conn, mini_pack(), "mini")
    conn.close()
    driver = FakeDriver(topo)
    srv = labserver.Server(("127.0.0.1", 0), driver, db_path, ROOT, ROOT / "lab" / "web")
    thread = threading.Thread(target=srv.serve_forever, daemon=True)
    thread.start()
    yield srv
    srv.shutdown()
    srv.server_close()


def call(srv, method, path, body=None, host=None, origin=True):
    port = srv.server_address[1]
    host = host or f"127.0.0.1:{port}"
    headers = {"Host": host, "Content-Type": "application/json"}
    if origin:
        headers["Origin"] = f"http://{host}"
    c = http.client.HTTPConnection("127.0.0.1", port, timeout=5)
    c.request(method, path, json.dumps(body) if body is not None else None, headers)
    res = c.getresponse()
    data = res.read()
    return res.status, json.loads(data) if res.getheader("Content-Type") == "application/json" else data


def test_api_refuses_other_hosts_and_origins(server):
    assert call(server, "GET", "/api/state", host="evil.example:80")[0] == 403
    assert call(server, "POST", "/api/labs/l1/start", {}, origin=False)[0] == 403
    assert call(server, "GET", "/api/state", origin=False)[0] == 200


def test_api_serves_the_page(server):
    status, page = call(server, "GET", "/")
    assert status == 200 and b"OpsForge Labs" in page
    assert call(server, "GET", "/static/../opsforge")[0] == 404


def test_api_lab_flow(server):
    status, lab = call(server, "GET", "/api/labs/l1")
    assert status == 200 and "answer" not in lab["steps"][0]["check"] and lab["steps"][0]["hints"] == []
    assert call(server, "POST", "/api/labs/l2/start", {})[1]["error"].startswith("this lab is locked")
    status, lab = call(server, "POST", "/api/labs/l1/start", {})
    assert status == 200 and lab["attempt"]
    attempt = lab["attempt"]["id"]
    assert server.driver.ran == ["setup"]
    status, hint = call(server, "POST", f"/api/attempts/{attempt}/hint", {"step": "q"})
    assert hint == {"hint": "h1", "index": 0, "hints_used": 1}
    status, result = call(server, "POST", f"/api/attempts/{attempt}/check", {"step": "q", "answer": 0})
    assert status == 200 and result == {"passed": False, "explain": None}
    status, result = call(server, "POST", f"/api/attempts/{attempt}/check", {"step": "q", "answer": 1})
    assert result["passed"] and result["explain"] == "e"
    assert result["finished"]["xp"] == 90 and [b["id"] for b in result["finished"]["badges"]] == ["first"]
    assert server.driver.ran == ["setup", "teardown", "heal"]
    assert call(server, "POST", f"/api/attempts/{attempt}/check", {"step": "q", "answer": 1})[0] == 409
    status, state = call(server, "GET", "/api/state")
    assert state["user"]["xp"] == 90 and state["user"]["badges"] == 1 and state["running"] is None


def test_api_abandon_tears_down(server):
    attempt = call(server, "POST", "/api/labs/l1/start", {})[1]["attempt"]["id"]
    status, result = call(server, "POST", f"/api/attempts/{attempt}/abandon", {})
    assert status == 200 and server.driver.ran == ["setup", "teardown", "heal"]
    assert call(server, "GET", "/api/state")[1]["running"] is None


def test_step_order_survives_the_database(db):
    """Lab bodies go through JSON: a step written `{run: ..., name: x}` must still be a run step."""
    conn, _ = db
    doc = mini_pack()
    doc["labs"]["l2"]["steps"][0]["check"] = {"probe": {"steps": [{"run": {"cmd": "true"}, "name": "ok"}],
                                                        "expect": {"ok.exit": "== 0"}}}
    labsdb.import_pack(conn, doc, "mini")
    step = labsdb.get_lab(conn, "l2")["body"]["steps"][0]["check"]["probe"]["steps"][0]
    assert scenario.step_type(step) == "run"
    assert scenario.step_type({"name": "ok", "run": {"cmd": "true"}}) == "run"
