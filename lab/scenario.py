"""Scenario files (scenarios/<name>.yml, docs/PLAN.md Phase 6): validation, placeholders, measurement parsing and
expectations. Pure functions; `opsforge scenario` runs the steps against the lab.

A scenario has phases `setup`, `fault`, `measure` and `teardown`, each a list of steps run in order, and `expect`,
a map of metric -> condition (or a list of them) checked at the end. Teardown always runs, after `heal`, even when a step failed.
A step with `name` records metrics as <name>.<metric>, e.g. base.avg_ms; conditions compare a metric with a number
or an expression of other metrics: "< 150", ">= slow.p50_ms * 2", "== 1".
"""

import ast
import operator
import re

import render

PHASES = ("setup", "fault", "measure", "teardown")
# Step type -> (required keys, optional keys). The type is the step's first key; `name` is optional on any step.
STEPS = {
    "sleep": ((), ()),
    "partition": ((), ()),
    "heal": ((), ()),
    "degrade": (("link",), ("rtt", "jitter", "loss", "rate", "mtu")),
    "mtu": (("link", "bytes"), ()),
    "flap": (("link",), ("period",)),
    "stop_flap": ((), ()),
    "run": (("cmd",), ("host", "timeout")),
    "ping": (("from", "to"), ("count", "interval")),
    "http": (("from", "url"), ("count", "timeout")),
    "transfer": (("from", "url"), ("timeout",)),
    "wait_until": ((), ("cmd", "host", "promql", "until", "timeout", "interval")),
    "promql": (("query",), ()),
}
# Steps whose value is the argument itself rather than a mapping: `sleep: 30s`, `partition: c`, `heal: true`
SCALAR_STEPS = ("sleep", "partition", "heal", "stop_flap")
_PLACEHOLDER = re.compile(r"\$\{(ip|public):([a-z0-9-]+)\}")
_METRIC = re.compile(r"\b[a-z_][a-z0-9_]*\.[a-z_][a-z0-9_]*\b")
_OPS = {"<": operator.lt, "<=": operator.le, ">": operator.gt, ">=": operator.ge, "==": operator.eq, "!=": operator.ne}
_ARITH = {ast.Add: operator.add, ast.Sub: operator.sub, ast.Mult: operator.mul, ast.Div: operator.truediv}


def step_type(step):
    return next(iter(step))


def errors(doc):
    """Problems with a parsed scenario file, as messages; empty if it can run."""
    if not isinstance(doc, dict):
        return ["a scenario is a mapping with setup/fault/measure/teardown/expect"]
    errs = [f"unknown key {k!r}" for k in doc if k not in (*PHASES, "description", "expect")]
    if not doc.get("description"):
        errs.append("missing description")
    names = set()
    for phase in PHASES:
        for i, step in enumerate(doc.get(phase) or []):
            where = f"{phase}[{i}]"
            if not isinstance(step, dict) or not step:
                errs.append(f"{where}: a step is a mapping like {{sleep: 10s}}")
                continue
            kind = step_type(step)
            if kind not in STEPS:
                errs.append(f"{where}: unknown step {kind!r} (one of {', '.join(STEPS)})")
                continue
            if "name" in step:
                names.add(step["name"])
            if kind in SCALAR_STEPS:
                continue
            args = step[kind]
            if not isinstance(args, dict):
                errs.append(f"{where}: {kind} takes a mapping")
                continue
            required, optional = STEPS[kind]
            errs += [f"{where}: {kind} needs {key!r}" for key in required if key not in args]
            errs += [f"{where}: {kind} has no {key!r}" for key in args if key not in (*required, *optional)]
            if kind == "wait_until" and ("cmd" in args) == ("promql" in args):
                errs.append(f"{where}: wait_until needs exactly one of 'cmd' and 'promql'")
            if kind == "wait_until" and "promql" in args and "until" not in args:
                errs.append(f"{where}: wait_until with promql needs 'until', e.g. '> 150'")
    for metric, condition in expectations(doc):
        if metric.split(".")[0] not in names:
            errs.append(f"expect: {metric}: no step is named {metric.split('.')[0]!r}")
        try:
            parse_condition(condition)
        except ValueError as e:
            errs.append(f"expect: {metric}: {e}")
    return errs


def expectations(doc):
    """[(metric, condition)] from `expect`, where a metric may have one condition or a list of them."""
    return [
        (metric, str(c))
        for metric, conditions in (doc.get("expect") or {}).items()
        for c in (conditions if isinstance(conditions, list) else [conditions])
    ]


def seconds(value):
    """'30s', '1.5s', '500ms' or a plain number of seconds."""
    return float(value) if isinstance(value, (int, float)) else render.time_us(str(value)) / 1e6


def substitute(text, topo):
    """${ip:<host>} -> a lab host's private address (gateway: its VPC address); ${public:<site>} -> a public IP."""
    nodes = render.node_addresses(topo)

    def resolve(m):
        kind, name = m.groups()
        if kind == "public" and name in topo["sites"]:
            return topo["sites"][name]["public_ip"]
        if kind == "ip" and name in nodes:
            return str(nodes[name].ip)
        if kind == "ip" and name.endswith("-gw") and name[:-3] in topo["sites"]:
            return str(render.gateway_address(topo["sites"][name[:-3]]).ip)
        raise ValueError(f"unknown {kind} {name!r} in {m.group(0)}")

    return _PLACEHOLDER.sub(resolve, text)


def parse_condition(condition):
    """'>= base.p50_ms * 2' -> ('>=', 'base.p50_ms * 2'); the right side is checked when evaluated."""
    m = re.fullmatch(r"\s*(<=|>=|==|!=|<|>)\s*(.+?)\s*", condition)
    if not m:
        raise ValueError(f"condition {condition!r} is not '<op> <value>' (op: {', '.join(_OPS)})")
    _evaluate(m[2], None)
    return m[1], m[2]


def _evaluate(expr, metrics):
    """Arithmetic on numbers and metric names; metrics None only checks the syntax."""
    names = {}

    def name_for(m):
        names[f"m{len(names)}"] = m.group(0)
        return f"m{len(names) - 1}"

    tree = ast.parse(_METRIC.sub(name_for, expr), mode="eval")

    def ev(node):
        if isinstance(node, ast.Expression):
            return ev(node.body)
        if isinstance(node, ast.Constant) and isinstance(node.value, (int, float)):
            return node.value
        if isinstance(node, ast.Name) and node.id in names:
            if metrics is None:
                return 1.0
            if names[node.id] not in metrics:
                raise KeyError(names[node.id])
            return metrics[names[node.id]]
        if isinstance(node, ast.BinOp) and type(node.op) in _ARITH:
            return _ARITH[type(node.op)](ev(node.left), ev(node.right))
        if isinstance(node, ast.UnaryOp) and isinstance(node.op, ast.USub):
            return -ev(node.operand)
        raise ValueError(f"unsupported expression {expr!r}")

    return ev(tree)


def check(metric, condition, metrics):
    """(passed, measured value or None, threshold or None) for one expectation."""
    op, expr = parse_condition(str(condition))
    value = metrics.get(metric)
    try:
        threshold = _evaluate(expr, metrics)
    except KeyError:
        return False, value, None
    return value is not None and _OPS[op](value, threshold), value, threshold


def percentile(values, q):
    ordered = sorted(values)
    return ordered[min(len(ordered) - 1, round(q / 100 * (len(ordered) - 1)))] if ordered else None


def parse_ping(out):
    """iputils ping summary -> {avg_ms, mdev_ms, loss_pct}; loss only if no reply came back."""
    loss = re.search(r"([\d.]+)% packet loss", out)
    rtt = re.search(r"= [\d.]+/([\d.]+)/[\d.]+/([\d.]+) ms", out)
    metrics = {"loss_pct": float(loss[1]) if loss else 100.0}
    if rtt:
        metrics |= {"avg_ms": float(rtt[1]), "mdev_ms": float(rtt[2])}
    return metrics


def parse_http(out):
    """Lines '<status> <seconds>' (curl -w) -> success rate and latency percentiles of the successful requests."""
    results = [line.split() for line in out.splitlines() if len(line.split()) == 2]
    ok = [float(t) * 1000 for code, t in results if code.startswith(("2", "3"))]
    metrics = {"requests": len(results), "success_pct": 100.0 * len(ok) / len(results) if results else 0.0}
    if ok:
        metrics |= {"p50_ms": percentile(ok, 50), "p95_ms": percentile(ok, 95), "max_ms": max(ok)}
    return metrics


def parse_transfer(out, exit_code):
    """curl -w '%{http_code} %{size_download} %{time_total}' -> ok (1/0), bytes, seconds, mbit (if completed)."""
    fields = out.split()
    if len(fields) != 3:
        return {"ok": 0, "bytes": 0}
    code, size, total = fields[0], int(float(fields[1])), float(fields[2])
    ok = int(exit_code == 0 and code.startswith("2"))
    metrics = {"ok": ok, "bytes": size, "seconds": total}
    if ok and total:
        metrics["mbit"] = size * 8 / total / 1e6
    return metrics


def compare(previous, current):
    """Lines describing how each metric moved since the previous run of the same scenario."""
    lines = []
    for metric, value in sorted(current.items()):
        if metric in previous and isinstance(value, (int, float)) and isinstance(previous[metric], (int, float)):
            delta = value - previous[metric]
            if abs(delta) > 1e-9:
                lines.append(f"{metric}: {previous[metric]:g} -> {value:g} ({delta:+g})")
    return lines
