"""Teaching-lab packs (labs/<pack>/, docs/PLAN.md Phase 10): loading, validation, content hashes, progression rules
and badge rules. Pure functions; lab/labsdb.py stores packs and progress, lab/labserver.py runs checks on the lab.

A pack is a folder: `pack.yml` (id, title, version, hint_penalty), `curricula/<id>.yml` (levels of lab ids),
`labs/<id>.yml` (briefing, setup/teardown scenario steps, ordered steps each with one check) and `badges.yml`.
Texts may use the scenario placeholders (`${node:c}`, `${ip:a-gw}`, ...), resolved for the running topology.
"""

import hashlib
import json
from pathlib import Path

import jsonschema
import yaml

import render
import scenario

SCHEMA = Path(__file__).resolve().parent / "labpack.schema.json"
DEFAULT_HINT_PENALTY = 0.1
DEFAULT_XP = 100
# Scenario steps that change the lab: allowed in a lab's setup/teardown, never in a check (checks only observe)
FAULT_STEPS = ("partition", "heal", "degrade", "mtu", "flap", "stop_flap")


def load_pack(path):
    """A pack folder -> the document labpack.schema.json describes; lab and curriculum ids are their file names."""
    path = Path(path)

    def read(p):
        return yaml.safe_load(p.read_text()) or {}

    badges = path / "badges.yml"
    return {
        "pack": read(path / "pack.yml"),
        "curricula": {p.stem: read(p) for p in sorted((path / "curricula").glob("*.yml"))},
        "labs": {p.stem: read(p) for p in sorted((path / "labs").glob("*.yml"))},
        "badges": (read(badges) or []) if badges.exists() else [],
    }


def find_packs(root):
    """Pack folders (those with a pack.yml) under `root`, in name order."""
    return sorted(p.parent for p in Path(root).glob("*/pack.yml"))


def content_hash(obj):
    """Stable hash of a lab (or any JSON-able content): changes when the content does, not with key order."""
    return hashlib.sha256(json.dumps(obj, sort_keys=True, separators=(",", ":")).encode()).hexdigest()[:16]


# Check types: the step's `check` is a one-key mapping `{<type>: <argument>}`. Each validator returns error messages.
def _check_probe(arg):
    if not isinstance(arg, dict) or "steps" not in arg:
        return ["probe takes {steps: [...], expect: {...}}"]
    errs = [f"unknown key {k!r}" for k in arg if k not in ("steps", "expect")]
    for step in arg["steps"] or []:
        if isinstance(step, dict) and step and scenario.step_type(step) in FAULT_STEPS:
            errs.append(f"{scenario.step_type(step)}: a check only observes the lab (faults go in setup)")
    if not arg.get("expect"):
        errs.append("probe needs expect (what passing means)")
    return errs + scenario.errors({"description": "-", "measure": arg["steps"], "expect": arg.get("expect")})


def _check_quiz(arg):
    if not isinstance(arg, dict):
        return ["quiz takes {question, choices, answer}"]
    errs = [f"unknown key {k!r}" for k in arg if k not in ("question", "choices", "answer", "explain")]
    choices = arg.get("choices")
    if not isinstance(choices, list) or len(choices) < 2:
        return errs + ["quiz needs at least two choices"]
    if not isinstance(arg.get("answer"), int) or not 0 <= arg["answer"] < len(choices):
        errs.append(f"quiz answer must be a choice index, 0-{len(choices) - 1}")
    return errs


def _check_confirm(arg):
    return [] if isinstance(arg, str) and arg else ["confirm takes the button's label, e.g. 'I read the routes'"]


CHECKS = {"probe": _check_probe, "quiz": _check_quiz, "confirm": _check_confirm}


def check_type(check):
    return next(iter(check))


# Badge rule types: name -> (keys it takes, validator(rule, doc, catalog) -> errors, earned(rule, history) -> bool).
# `history` is {"passes": [{"lab", "hints", "seconds", "xp"}], "curricula": {id: [lab ids]}, "xp": total}.
def _ints(rule, *keys):
    return [f"{k} must be a non-negative integer" for k in keys
            if k in rule and not (isinstance(rule[k], int) and rule[k] >= 0)]


def _needs(rule, *keys):
    return [f"needs {k}" for k in keys if k not in rule]


def _known(rule, key, ids):
    return [f"unknown {key} {rule[key]!r}"] if key in rule and rule[key] not in ids else []


def _passes(rule, history, labs=None):
    """Passes of `labs` (default: any) within the rule's max_hints and max_seconds."""
    return [p for p in history["passes"]
            if (labs is None or p["lab"] in labs)
            and p["hints"] <= rule.get("max_hints", p["hints"])
            and p["seconds"] <= rule.get("max_seconds", p["seconds"])]


def _valid_labs_passed(rule, doc, catalog):
    return (_needs(rule, "min") + _ints(rule, "min", "max_hints", "max_seconds")
            + _known(rule, "curriculum", set(doc["curricula"]) | set(catalog["curricula"])))


def _earned_labs_passed(rule, history):
    labs = history["curricula"].get(rule["curriculum"], []) if "curriculum" in rule else None
    return len({p["lab"] for p in _passes(rule, history, labs)}) >= rule["min"]


def _valid_lab_passed(rule, doc, catalog):
    return (_needs(rule, "lab") + _ints(rule, "max_hints", "max_seconds")
            + _known(rule, "lab", set(doc["labs"]) | set(catalog["labs"])))


def _earned_lab_passed(rule, history):
    return bool(_passes(rule, history, {rule["lab"]}))


def _valid_curriculum_complete(rule, doc, catalog):
    return _needs(rule, "curriculum") + _known(rule, "curriculum", set(doc["curricula"]) | set(catalog["curricula"]))


def _earned_curriculum_complete(rule, history):
    labs = set(history["curricula"].get(rule["curriculum"], []))
    return bool(labs) and labs <= {p["lab"] for p in history["passes"]}


def _valid_xp(rule, doc, catalog):
    return _needs(rule, "min") + _ints(rule, "min")


RULES = {
    "labs_passed": (("min", "curriculum", "max_hints", "max_seconds"), _valid_labs_passed, _earned_labs_passed),
    "lab_passed": (("lab", "max_hints", "max_seconds"), _valid_lab_passed, _earned_lab_passed),
    "curriculum_complete": (("curriculum",), _valid_curriculum_complete, _earned_curriculum_complete),
    "xp": (("min",), _valid_xp, lambda rule, history: history["xp"] >= rule["min"]),
}


def earned(rule, history):
    return RULES[rule["type"]][2](rule, history)


def errors(doc, catalog=None):
    """Problems with a loaded pack, as messages; empty if it can be imported. `catalog` ({"labs": ids,
    "curricula": ids} of other packs already imported) lets curricula and badges refer to them."""
    catalog = catalog or {"labs": set(), "curricula": set()}
    validator = jsonschema.Draft202012Validator(json.loads(SCHEMA.read_text()))
    errs = [f"{'/'.join(map(str, e.absolute_path)) or '<root>'}: {e.message}" for e in validator.iter_errors(doc)]
    if errs:
        return errs
    labs = set(doc["labs"]) | set(catalog["labs"])
    for cid, cur in doc["curricula"].items():
        for i, level in enumerate(cur["levels"]):
            errs += [f"curricula/{cid}: level {i + 1}: unknown lab {lab!r}" for lab in level["labs"] if lab not in labs]
    for lid, lab in doc["labs"].items():
        where = f"labs/{lid}"
        errs += [f"{where}: requires unknown lab {r!r}" for r in lab.get("requires", []) if r not in labs]
        if lid in lab.get("requires", []):
            errs.append(f"{where}: a lab cannot require itself")
        for phase in ("setup", "teardown"):
            errs += [f"{where}: {phase}: {e}" for e in scenario.errors({"description": "-", phase: lab.get(phase)})]
        ids = [s["id"] for s in lab["steps"]]
        errs += [f"{where}: duplicate step id {s!r}" for s in sorted(set(ids)) if ids.count(s) > 1]
        for step in lab["steps"]:
            if "open" in step and step["open"] not in lab.get("files", []):
                errs.append(f"{where}/{step['id']}: opens {step['open']}, which is not in files")
            kind = check_type(step["check"])
            if kind not in CHECKS:
                errs.append(f"{where}/{step['id']}: unknown check {kind!r} (one of {', '.join(CHECKS)})")
            else:
                errs += [f"{where}/{step['id']}: {e}" for e in CHECKS[kind](step["check"][kind])]
    ids = [b["id"] for b in doc["badges"]]
    errs += [f"badges: duplicate id {b!r}" for b in sorted(set(ids)) if ids.count(b) > 1]
    for badge in doc["badges"]:
        rule, where = badge["rule"], f"badges/{badge['id']}"
        if rule["type"] not in RULES:
            errs.append(f"{where}: unknown rule type {rule['type']!r} (one of {', '.join(RULES)})")
            continue
        keys, valid, _ = RULES[rule["type"]]
        errs += [f"{where}: {rule['type']} has no {k!r}" for k in rule if k not in ("type", *keys)]
        errs += [f"{where}: {e}" for e in valid(rule, doc, catalog)]
    return errs


def pass_xp(xp, hints, penalty):
    """XP a pass is worth: the lab's xp less `penalty` per hint used, never below half."""
    return round(xp * max(0.5, 1 - penalty * hints))


def unmet_needs(lab, topo):
    """Why the running topology cannot host this lab, as messages; empty if it can."""
    reasons = []
    if lab.get("layout") and lab["layout"] != scenario.layout(topo):
        reasons.append(f"needs the {lab['layout']} layout")
    have = {
        "vpn": topo.get("interconnect", "none") == "vpn",
        "k3s": bool(render.k3s_clusters(topo)),
        "monitoring": render.monitoring_node(topo) is not None,
        "ci": any(n["role"] == "ci" for n in topo["nodes"].values()),
    }
    labels = {"vpn": "the VPN (interconnect: vpn)", "k3s": "a K3s cluster", "monitoring": "the monitoring node",
              "ci": "the ci node"}
    reasons += [f"needs {labels[n]}" for n in lab.get("needs", []) if not have[n]]
    return reasons


def substitute(text, topo):
    """Scenario placeholders in a lab text resolved for `topo`; left as written if they don't resolve."""
    if topo is None:
        return text
    try:
        return scenario.substitute(text, topo)
    except ValueError:
        return text
