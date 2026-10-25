# OpsForge toolchain contract

`opsforge api <command>` is OpsForge's machine interface: one JSON document on stdin (for `run` and `probe`), one on
stdout and exit 0 on success; a problem document (`{contract, type: /problems/<slug>, title, detail}`) on stderr and
exit 1 otherwise. OpsForge Academy drives the lab through it; any other tool may.

| Command | In | Out |
|---|---|---|
| `version` | | `{contract, minor, opsforge}` |
| `state` | | lab up, layout, sites, links (live, desired, condition), nodes, fault, `placeholders` (`${node:a}` → value) |
| `run` | `runInput`: `{contract, phase: setup\|teardown, steps, namespace?}` | `runOutput`: `{contract, metrics, log, error}` |
| `probe` | `probeInput`: `{contract, steps, expect, namespace?}` | `probeOutput`: `{contract, passed, results, log, error}` |
| `heal` | | `healOutput`: `{contract, healed}` |

The documents are `$defs` of [`toolchain.schema.json`](toolchain.schema.json); [`golden/`](golden/) holds what this
release prints for fixed inputs (written by `tests/test_academy.py`, `OPSFORGE_UPDATE_GOLDEN=1` to rewrite them).

## Versions

- Every document carries `contract`, the **major** version (1). `api version` adds `minor` and the OpsForge release.
- **Minor** (additive): a new command, a new optional input field, a new output field. **Major**: anything removed,
  renamed, made required or changed in meaning.
- Inputs are strict: OpsForge refuses a field it does not know. A caller sends an optional field only when `minor`
  says it exists.
- Outputs are open: a reader ignores fields it does not know, so a newer OpsForge works with an older caller of the
  same major.
- A caller refuses another major, and names the minimum release it needs when `minor` is too low.

| Minor | OpsForge | Added |
|---|---|---|
| 0 | 1.0.0 | `version`, `state` (with `placeholders`), `run`, `probe`, `heal` |
