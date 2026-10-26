"""OpsForge's machine interface `opsforge api` (contract/README.md): the contract version, the input checks of its
documents and the helpers its commands share. OpsForge Academy calls it to drive the lab.
"""

import json
import re
from pathlib import Path

import jsonschema

import scenario

# The toolchain contract `opsforge api` speaks (contract/README.md): the major in every document, the minor (additive
# revisions) in `api version`
CONTRACT = 1
CONTRACT_MINOR = 0
# Inputs and outputs of the API's documents (OpsForge publishes it; callers keep a copy)
TOOLCHAIN_SCHEMA = Path(__file__).resolve().parents[1] / "contract" / "toolchain.schema.json"


def input_errors(kind, doc):
    """Why `doc` is not a valid `kind` document (toolchain.schema.json's $defs: sandboxInput, probeInput, ...)."""
    schema = {**json.loads(TOOLCHAIN_SCHEMA.read_text()), "$ref": f"#/$defs/{kind}"}
    return [f"{'/'.join(map(str, e.absolute_path)) or 'document'}: {e.message}"
            for e in jsonschema.Draft202012Validator(schema).iter_errors(doc)]


def with_namespace(obj, namespace):
    """`obj` (steps, a text) with `${ns}` replaced by the sandbox's namespace; ValueError if it has none."""
    if isinstance(obj, dict):
        return {k: with_namespace(v, namespace) for k, v in obj.items()}
    if isinstance(obj, list):
        return [with_namespace(v, namespace) for v in obj]
    if isinstance(obj, str) and "${ns}" in obj:
        if namespace is None:
            raise ValueError(f"{obj!r} uses ${{ns}}, but there is no namespace (a sandbox with namespace: true)")
        return obj.replace("${ns}", namespace)
    return obj


def step_errors(sets):
    """`validate-steps`: why each set of steps (with its `expect`) would not run, as [{set, message}] in set order.
    `faults: false` refuses fault steps (scenario.FAULT_STEPS): checks only observe the lab."""
    errors = []
    for n, s in enumerate(sets):
        steps = s["steps"]
        msgs = [f"steps[{i}]: {scenario.step_type(step)}: fault steps are not allowed here (only a lab's setup and "
                "teardown change the lab)" for i, step in enumerate(steps)
                if not s.get("faults", True) and isinstance(step, dict) and step
                and scenario.step_type(step) in scenario.FAULT_STEPS]
        msgs += [re.sub(r"^measure\[", "steps[", e)
                 for e in scenario.errors({"description": "-", "measure": steps, "expect": s.get("expect")})]
        errors += [{"set": n, "message": m} for m in msgs]
    return errors
