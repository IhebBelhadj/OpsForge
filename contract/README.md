# OpsForge toolchain contract

OpsForge's machine interface, in two forms over the same documents and the same code. OpsForge Academy drives the
lab through it; any other tool may.

- **HTTP** (`opsforge api serve`, [`openapi.yaml`](openapi.yaml)): what a client on another machine (or in another
  repository) uses. Routes under `/v1/`; a bearer token (`.opsforge/api-token`) on every call but `GET /v1/version`;
  errors are problem documents (`application/problem+json`). A client's lab work happens in **sandboxes** (below).
  Lab work is a **job**: `202` with the job, then `GET /v1/jobs/{id}?wait=<s>` until `done` (its `output` is the
  output document) or `failed` (its `problem`); one lab job at a time (`409 /problems/lab-busy`). Binds 127.0.0.1 by
  default; another address needs `--tls-cert`/`--tls-key` (or keep loopback and use an SSH tunnel).
- **CLI** (`opsforge api <command> --json`): for people and scripts on the lab host. One JSON document on stdin (for
  `run`, `probe` and `validate-steps`), one on stdout and exit 0 on success; a problem document on stderr and exit 1
  otherwise. `run` and `probe` work outside any sandbox (authors trying steps); `sandbox [list | delete <id>]` is the
  lab owner's view and cleanup by hand.

## Sandboxes

A **sandbox** is the one place a client's lab work happens: one per lab or challenge attempt, made from the client's
requirements and always cleaned up by OpsForge.

- `POST /v1/sandboxes` (`sandboxInput`: `{kind: lab|challenge, label, requires: {layout?, needs?, faults?,
  cluster_changes?}, namespace?, files?, setup?, teardown?, expires_after?}`) checks the requirements against the
  running lab (`409 /problems/lab-down`; `409 /problems/unmet-requirements` with `unmet`, one entry per missing
  thing), then creates as a job: the workspace with `files`, a namespace `sb-<id>` on every K3s cluster
  (`namespace: true`), then `setup`. The job's `sandbox` is the id from the 202 on; its output is the sandbox,
  `ready`, or `failed` when setup failed (cleaned up at once: teardown, namespace, heal).
- Steps run on the lab host **in the workspace**, with `opsforge` on the PATH, `${ns}` the namespace and the
  namespace as `opsforge kubectl`'s default (`-n`/`-A` still win). Fault steps (partition, degrade, ...) are allowed
  only in a sandbox that declares `requires.faults`, and never in probes or actions.
- Under `/v1/sandboxes/{id}/`: `files/...` and `moves` (its workspace: 1 MiB per file, 10 MiB in all, paths
  confined), `terminal` (a WebSocket shell in the workspace), `probes` and `actions` (jobs, on a `ready` sandbox),
  `reset` (job: teardown then setup; the files are kept). `DELETE` (job) hangs its terminals up, runs `teardown`,
  deletes the namespace, heals when it had faults and deletes the workspace (output: the sandbox, `deleted`).
- A sandbox with `faults` or `cluster_changes` runs alone (`409 /problems/lab-busy` both ways); others coexist.
- OpsForge keeps them in `<data>/sandboxes/<id>.json` (steps included): they survive a restart, an idle one is
  deleted after `expires_after` seconds without a call (default a day), and lab work a killed API left halfway is
  finished by deleting that sandbox when it comes back.

| Command | HTTP | In | Out |
|---|---|---|---|
| `version` | `GET /v1/version` | | `{contract, minor, opsforge}` |
| `state` | `GET /v1/state` | | lab up, layout, sites, links (live, desired, condition), nodes, fault, `placeholders` (`${node:a}` → value) |
| `run` | | `runInput`: `{contract, phase: setup\|teardown, steps}` | `runOutput`: `{contract, metrics, log, error}` |
| `probe` | `POST /v1/sandboxes/{id}/probes` → job | `probeInput`: `{contract, steps, expect}` | `probeOutput`: `{contract, passed, results, log, error}` |
| | `POST /v1/sandboxes/{id}/actions` → job | `actionInput`: `{contract, steps}` | `runOutput` |
| `heal` | `POST /v1/heal` → job | | `healOutput`: `{contract, healed}` |
| `validate-steps` | `POST /v1/steps/validate` | `stepsInput`: `{contract, sets: [{steps, expect?, faults?}]}` | `stepsOutput`: `{contract, errors: [{set, message}]}` |
| `sandbox list` | `GET /v1/sandboxes[/{id}]` | | `sandboxesOutput`: `{contract, sandboxes}`; `sandboxOutput`: `{contract, id, kind, label, status, namespace, layout, placeholders, requires, setup, teardown, created_at, expires_at}` |
| | `POST /v1/sandboxes` → job | `sandboxInput` | the job's output: `sandboxOutput` |
| | `POST /v1/sandboxes/{id}/reset` → job | | `sandboxOutput` |
| `sandbox delete <id>` | `DELETE /v1/sandboxes/{id}` → job | | `sandboxOutput` (`deleted`, with `teardown`) |
| | `GET /v1/jobs/{id}` | | `jobOutput`: `{contract, id, kind, sandbox, status, log, output, problem}` |
| | `/v1/sandboxes/{id}/files` …, `moves` | `moveInput` | `filesOutput`: `{contract, files: [{path, size}], bytes}`; files as bytes |
| | `GET /v1/sandboxes/{id}/terminal` | WebSocket: `{"t":"i","d"}`, `{"t":"r","cols","rows"}` | the shell's output (binary frames) |

The documents are `$defs` of [`toolchain.schema.json`](toolchain.schema.json) (`problem` included); [`golden/`](golden/)
holds what this release returns for fixed inputs (written by `tests/test_api.py` and `tests/test_apiserver.py`,
`OPSFORGE_UPDATE_GOLDEN=1` to rewrite them).

## Versions

- Every document carries `contract`, the **major** version (1). `api version` adds `minor` and the OpsForge release.
- **Minor** (additive): a new command, a new optional input field, a new output field. **Major**: anything removed,
  renamed, made required or changed in meaning.
- Inputs are strict: OpsForge refuses a field it does not know. A caller sends an optional field only when `minor`
  says it exists.
- Outputs are open: a reader ignores fields it does not know, so a newer OpsForge works with an older caller of the
  same major.
- A caller refuses another major, and names the minimum release it needs when `minor` is too low.
- Over HTTP the major is also the path prefix (`/v1/`): a new route, query parameter or optional header is a minor;
  a new major is served next to the old one (`/v2/` beside `/v1/`) for at least one release.

| Minor | OpsForge | Added |
|---|---|---|
| 0 | 1.0.0 | `version`, `state` (with `placeholders`), `run`, `probe`, `heal`, `validate-steps`, `sandbox`; over HTTP: jobs and sandboxes (files, terminal, probes, actions, reset) |
