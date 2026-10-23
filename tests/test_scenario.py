"""lab/scenario.py: scenario validation, placeholders, measurement parsing and expectations (no Incus)."""

import pytest
import yaml

import scenario
from conftest import ROOT


@pytest.mark.parametrize("path", sorted((ROOT / "scenarios").glob("*.yml")), ids=lambda p: p.stem)
def test_repo_scenarios_are_valid(path):
    assert scenario.errors(yaml.safe_load(path.read_text())) == []


def test_errors_catch_unknown_steps_keys_and_metrics():
    doc = {
        "description": "x",
        "setup": [{"teleport": {}}, {"ping": {"from": "b-w1"}, "name": "p"}, {"run": {"cmd": "true", "on": "x"}}],
        "expect": {"q.avg_ms": "< 10", "p.avg_ms": "about 10"},
    }
    errs = scenario.errors(doc)
    assert any("unknown step 'teleport'" in e for e in errs)
    assert any("ping needs 'to'" in e for e in errs)
    assert any("run has no 'on'" in e for e in errs)
    assert any("no step is named 'q'" in e for e in errs)
    assert any("p.avg_ms" in e and "not '<op> <value>'" in e for e in errs)


def test_wait_until_needs_one_probe():
    doc = {"description": "x", "measure": [{"wait_until": {"cmd": "true", "promql": "up"}}]}
    assert any("exactly one" in e for e in scenario.errors(doc))
    doc = {"description": "x", "measure": [{"wait_until": {"promql": "up"}}]}
    assert any("needs 'until'" in e for e in scenario.errors(doc))


def test_substitute_placeholders(topo):
    assert scenario.substitute("http://${public:a}/ ${ip:a-cp1} ${ip:b-gw}", topo) == (
        "http://203.0.113.10/ 10.10.0.11 10.20.0.1"
    )
    with pytest.raises(ValueError):
        scenario.substitute("${ip:nope}", topo)


def test_check_numbers_and_metric_expressions():
    metrics = {"base.p50_ms": 250.0, "slow.p50_ms": 610.0, "large.ok": 0}
    assert scenario.check("slow.p50_ms", ">= base.p50_ms + 300", metrics) == (True, 610.0, 550.0)
    assert scenario.check("large.ok", "== 0", metrics)[0]
    assert not scenario.check("slow.p50_ms", "< base.p50_ms * 2", metrics)[0]
    assert scenario.check("missing.x", "== 1", metrics) == (False, None, 1)
    assert scenario.check("slow.p50_ms", "> nope.x", metrics) == (False, 610.0, None)


def test_conditions_reject_code():
    with pytest.raises(ValueError):
        scenario.parse_condition("> __import__('os').system('true')")


def test_expectations_accept_lists():
    doc = {"expect": {"stall.value": [">= 50", "< 120"], "done.ok": "== 1"}}
    assert scenario.expectations(doc) == [("stall.value", ">= 50"), ("stall.value", "< 120"), ("done.ok", "== 1")]


def test_parse_ping():
    out = "20 packets transmitted, 19 received, 5% packet loss, time 3805ms\nrtt min/avg/max/mdev = 36.1/41.2/52.3/4.4 ms"
    assert scenario.parse_ping(out) == {"loss_pct": 5.0, "avg_ms": 41.2, "mdev_ms": 4.4}
    assert scenario.parse_ping("") == {"loss_pct": 100.0}


def test_parse_http_ignores_failures_in_latency():
    out = "200 0.250\n200 0.300\n000 5.001\n200 0.280\n"
    m = scenario.parse_http(out)
    assert m["requests"] == 4 and m["success_pct"] == 75.0
    assert m["p50_ms"] == pytest.approx(280.0) and m["max_ms"] == pytest.approx(300.0)


def test_parse_transfer():
    assert scenario.parse_transfer("200 2000000 0.800000", 0) == {"ok": 1, "bytes": 2000000, "seconds": 0.8, "mbit": 20.0}
    assert scenario.parse_transfer("200 512 15.001", 28) == {"ok": 0, "bytes": 512, "seconds": 15.001}
    assert scenario.parse_transfer("", 7) == {"ok": 0, "bytes": 0}


def test_compare_reports_moved_metrics_only():
    assert scenario.compare({"a.x": 1.0, "a.y": 2.0}, {"a.x": 1.0, "a.y": 3.5, "a.z": 9}) == ["a.y: 2 -> 3.5 (+1.5)"]
