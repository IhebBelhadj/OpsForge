# OpsForge toolchain contract

OpsForge's machine interface, in two forms over the same documents and the same code. OpsForge Academy drives the
lab through it; any other tool may.

- **HTTP** (`opsforge api serve`, [`openapi.yaml`](openapi.yaml)): what a client on another machine (or in another
  repository) uses. Routes under `/v1/`; a bearer token (`.opsforge/api-token`) on every call but `GET /v1/version`;
  errors are problem documents (`application/problem+json`). Lab work (`run`, `probe`, `heal`) is a **job**: `202`
  with the job, then `GET /v1/jobs/{id}?wait=<s>` until `done` (its `output` is the command's output document) or
  `failed` (its `problem`); one lab job at a time (`409 /problems/lab-busy`). The learners' **workspaces** live
  here (`/v1/workspaces/{ws}/files/...`, where the shell runs), and `GET /v1/terminal?workspace=` is a **WebSocket
  shell** on the lab host. Binds 127.0.0.1 by default; another address needs `--tls-cert`/`--tls-key` (or keep
  loopback and use an SSH tunnel).
- **CLI** (`opsforge api <command> --json`): for people and scripts on the lab host. One JSON document on stdin (for
  `run`, `probe` and `validate-steps`), one on stdout and exit 0 on success; a problem document on stderr and exit 1
  otherwise.

| Command | HTTP | In | Out |
|---|---|---|---|
| `version` | `GET /v1/version` | | `{contract, minor, opsforge}` |
| `state` | `GET /v1/state` | | lab up, layout, sites, links (live, desired, condition), nodes, fault, `placeholders` (`${node:a}` → value) |
| `run` | `POST /v1/runs` → job | `runInput`: `{contract, phase: setup\|teardown, steps, namespace?}` | `runOutput`: `{contract, metrics, log, error}` |
| `probe` | `POST /v1/probes` → job | `probeInput`: `{contract, steps, expect, namespace?}` | `probeOutput`: `{contract, passed, results, log, error}` |
| `heal` | `POST /v1/heal` → job | | `healOutput`: `{contract, healed}` |
| `validate-steps` | `POST /v1/steps/validate` | `stepsInput`: `{contract, sets: [{steps, expect?, faults?}]}` | `stepsOutput`: `{contract, errors: [{set, message}]}` |
| | `GET /v1/jobs/{id}` | | `jobOutput`: `{contract, id, kind, status, log, output, problem}` |
| | `/v1/workspaces/{ws}` … | `moveInput` | `filesOutput`: `{contract, files: [{path, size}], bytes}`; files as bytes |
| | `GET /v1/terminal` | WebSocket: `{"t":"i","d"}`, `{"t":"r","cols","rows"}` | the shell's output (binary frames) |

The documents are `$defs` of [`toolchain.schema.json`](toolchain.schema.json) (`problem` included); [`golden/`](golden/)
holds what this release returns for fixed inputs (written by `tests/test_academy.py` and `tests/test_apiserver.py`,
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
| 0 | 1.0.0 | `version`, `state` (with `placeholders`), `run`, `probe`, `heal`, `validate-steps`; over HTTP: jobs, workspaces, terminal |
