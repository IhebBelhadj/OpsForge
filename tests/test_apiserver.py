"""`opsforge api serve` (lab/apiserver.py, contract/openapi.yaml): the contract over HTTP, on a real server in a
thread. Lab work runs host `run` steps (no Incus) in sandboxes (tests/test_sandboxes.py has their lifecycle); the
lab state, namespaces and heal are faked."""

import base64
import http.client
import json
import os
import re
import socket
import struct
import subprocess
import threading
import time

import pytest
import yaml

import apiserver
from conftest import ROOT, cli
from test_api import contract_doc, echo, golden, timeless

OPENAPI = yaml.safe_load((ROOT / "contract" / "openapi.yaml").read_text())


@pytest.fixture
def lab(monkeypatch):
    """A running stretched lab (topology.yml): `lab.state` is what lab_facts reports, `lab.kube` the commands run
    in instances (namespaces), `lab.healed` counts heals."""
    topo = cli.load_topology()

    class Lab:
        state = {"up": True, "error": None, "topology": "topology.yml", "layout": "stretched",
                 "placeholders": cli.scenario.placeholders(topo)}
        kube = []
        healed = []

    def run_in(name, *cmd):
        Lab.kube.append((name, " ".join(cmd)))
        return subprocess.CompletedProcess(cmd, 0, "", "")

    monkeypatch.setattr(cli, "lab_facts", lambda: (Lab.state, Lab.topo))
    monkeypatch.setattr(cli, "load_valid_topology", lambda: Lab.topo)
    monkeypatch.setattr(cli, "run_in", run_in)
    monkeypatch.setattr(cli, "heal_links", lambda: Lab.healed.append(1) or True)
    Lab.topo = topo
    return Lab


@pytest.fixture
def api(tmp_path, monkeypatch, lab):
    """A server on a free port with its state in tmp_path; `api.call(method, path, body=None, token=True, headers)`
    returns (status, headers, decoded body); `api.sandbox(**input)` creates one and waits for it."""
    monkeypatch.setattr(cli, "STATE", tmp_path)
    monkeypatch.setattr(cli, "api_state", lambda: {"contract": 1, "up": False})
    server = apiserver.make_server(cli, "127.0.0.1", 0)
    threading.Thread(target=server.serve_forever, daemon=True).start()

    class Client:
        port = server.server_address[1]
        token = (tmp_path / "api-token").read_text().strip()
        workspaces = tmp_path / "workspaces"
        sandboxes = server.sandboxes
        data = tmp_path

        def sandbox(self, **doc):
            """A sandbox from a sandboxInput (kind lab by default), once its setup ended: its document."""
            status, _, job = self.call("POST", "/v1/sandboxes", {"contract": 1, "kind": "lab", "label": "test", **doc})
            assert status == 202, job
            return wait_job(self, job["id"])["output"]

        def call(self, method, path, body=None, token=True, headers=None):
            conn = http.client.HTTPConnection("127.0.0.1", self.port, timeout=30)
            hdrs = {"Authorization": f"Bearer {self.token}"} if token else {}
            data = body if isinstance(body, bytes) or body is None else json.dumps(body).encode()
            conn.request(method, path, body=data, headers={**hdrs, **(headers or {})})
            resp = conn.getresponse()
            raw = resp.read()
            conn.close()
            kind = resp.getheader("Content-Type") or ""
            return resp.status, resp, json.loads(raw) if "json" in kind else raw

    yield Client()
    server.shutdown()
    server.server_close()


def problem(status, body, slug):
    assert contract_doc("problem", body)["type"] == f"/problems/{slug}", body
    return status


# Access: a token, no browsers, the loopback Host


def test_version_needs_no_token(api):
    status, _, doc = api.call("GET", "/v1/version", token=False)
    assert status == 200 and contract_doc("versionOutput", doc)["opsforge"] == cli.VERSION


def test_every_other_call_needs_the_token(api):
    status, _, doc = api.call("GET", "/v1/state", token=False)
    assert problem(status, doc, "no-token") == 401
    status, _, doc = api.call("GET", "/v1/state", token=False, headers={"Authorization": "Bearer nope"})
    assert problem(status, doc, "no-token") == 401
    assert api.call("GET", "/v1/state")[0] == 200


def test_browsers_and_other_hosts_are_refused(api):
    status, _, doc = api.call("GET", "/v1/state", headers={"Origin": "http://127.0.0.1:8090"})
    assert problem(status, doc, "cross-origin") == 403
    status, _, doc = api.call("GET", "/v1/version", headers={"Host": f"evil.example:{api.port}"})
    assert problem(status, doc, "wrong-host") == 421


def test_the_token_file_is_private(api, tmp_path):
    assert (tmp_path / "api-token").stat().st_mode & 0o777 == 0o600 and len(api.token) >= 40


def test_unknown_routes_and_methods(api):
    status, _, doc = api.call("GET", "/v1/nope")
    assert problem(status, doc, "not-found") == 404
    status, _, doc = api.call("DELETE", "/v1/state")
    assert problem(status, doc, "method-not-allowed") == 405
    for route in ("/v1/runs", "/v1/probes", "/v1/workspaces/lab-7", "/v1/terminal"):
        assert api.call("POST", route)[0] == 404, f"{route}: replaced by sandboxes"


def test_serve_refuses_other_addresses_without_tls():
    args = cli.argparse.Namespace(action="serve", host="0.0.0.0", port=0, tls_cert=None, tls_key=None, data=None)
    with pytest.raises(cli.LabError, match="not a loopback address"):
        cli.cmd_api_serve(args)


# Jobs: a sandbox's lab work, heal


def wait_job(api, job_id):
    status, _, doc = api.call("GET", f"/v1/jobs/{job_id}?wait=20")
    assert status == 200
    return contract_doc("jobOutput", doc)


def test_lab_work_is_a_job(api):
    sb = api.sandbox()
    status, resp, doc = api.call("POST", f"/v1/sandboxes/{sb['id']}/actions",
                                 {"contract": 1, "steps": [echo("x", 42), {"sleep": "1s"}]})
    assert status == 202 and contract_doc("jobOutput", doc)["status"] == "running" and doc["sandbox"] == sb["id"]
    assert resp.getheader("Location") == f"/v1/jobs/{doc['id']}"
    status, _, busy = api.call("POST", "/v1/heal")
    assert problem(status, busy, "lab-busy") == 409, "one lab job at a time"
    job = wait_job(api, doc["id"])
    assert job["status"] == "done" and job["problem"] is None and job["kind"] == "action"
    assert contract_doc("runOutput", job["output"])["metrics"]["x.value"] == 42 and job["log"] == job["output"]["log"]
    golden("job.json", {**timeless(job), "id": "0123456789abcdef", "sandbox": "0123abcd",
                        "output": timeless(job["output"])})


def test_the_log_grows_while_the_job_runs(api):
    sb = api.sandbox()
    _, _, doc = api.call("POST", f"/v1/sandboxes/{sb['id']}/actions",
                         {"contract": 1, "steps": [echo("x", 1), {"sleep": "2s"}, echo("y", 2)]})
    time.sleep(1)
    _, _, running = api.call("GET", f"/v1/jobs/{doc['id']}")
    assert running["status"] == "running" and any("echo 1" in line for line in running["log"])
    assert len(wait_job(api, doc["id"])["log"]) > len(running["log"])


def test_a_probe_is_a_job(api):
    sb = api.sandbox()
    _, _, doc = api.call("POST", f"/v1/sandboxes/{sb['id']}/probes",
                         {"contract": 1, "steps": [echo("a", 41)], "expect": {"a.value": "> 40"}})
    job = wait_job(api, doc["id"])
    assert job["kind"] == "probe" and contract_doc("probeOutput", job["output"])["passed"] is True


def test_heal_and_a_failed_job(api, monkeypatch):
    _, _, doc = api.call("POST", "/v1/heal")
    job = wait_job(api, doc["id"])
    assert job["output"] == {"contract": 1, "healed": True} and job["sandbox"] is None

    def down():
        raise cli.LabError("lab is down (./opsforge up)")

    monkeypatch.setattr(cli, "heal_links", down)
    _, _, doc = api.call("POST", "/v1/heal")
    job = wait_job(api, doc["id"])
    assert job["status"] == "failed" and job["output"] is None
    assert job["problem"]["type"] == "/problems/lab-error" and "lab is down" in job["problem"]["detail"]


def test_bad_job_inputs_are_problems(api):
    sb = api.sandbox()
    for body, slug in ((b"not json", "bad-input"), ({"contract": 2, "steps": []}, "contract-mismatch"),
                       ({"contract": 1, "steps": [], "namespace": "default"}, "bad-input")):
        status, _, doc = api.call("POST", f"/v1/sandboxes/{sb['id']}/actions", body)
        assert problem(status, doc, slug) == 400, body
    status, _, doc = api.call("GET", "/v1/jobs/0123456789abcdef")
    assert problem(status, doc, "not-found") == 404


def test_validate_steps(api):
    doc = {"contract": 1, "sets": [{"steps": [{"pign": {}}]}, {"steps": [{"partition": "c"}], "faults": False}]}
    status, _, out = api.call("POST", "/v1/steps/validate", doc)
    assert status == 200 and contract_doc("stepsOutput", out) == cli.api_validate_steps(doc)
    assert [e["set"] for e in out["errors"]] == [0, 1]


# A sandbox's workspace


def test_workspace_files(api):
    sb = api.sandbox(files={"k8s/pod.yaml": "kind: Pod\n"})
    ws = f"/v1/sandboxes/{sb['id']}"
    assert api.call("PUT", f"{ws}/files/notes.md", "é\n".encode())[0] == 204
    status, _, doc = api.call("GET", f"{ws}/files")
    assert contract_doc("filesOutput", doc) == {"contract": 1, "bytes": 13, "files": [
        {"path": "k8s/pod.yaml", "size": 10}, {"path": "notes.md", "size": 3}]}
    assert api.call("GET", f"{ws}/files/k8s/pod.yaml")[2] == b"kind: Pod\n"
    assert api.call("POST", f"{ws}/moves", {"contract": 1, "from": "notes.md", "to": "doc/notes.md"})[0] == 204
    status, _, doc = api.call("POST", f"{ws}/moves", {"contract": 1, "from": "doc/notes.md", "to": "k8s/pod.yaml"})
    assert problem(status, doc, "exists") == 409
    assert api.call("DELETE", f"{ws}/files/doc/notes.md")[0] == 204
    status, _, doc = api.call("GET", f"{ws}/files/doc/notes.md")
    assert problem(status, doc, "not-found") == 404
    status, _, doc = api.call("GET", "/v1/sandboxes/0123abcd/files")
    assert problem(status, doc, "not-found") == 404


@pytest.mark.parametrize("path", ["..%2Fescape", "a/../../escape", "a//b", "./a", "%2Fetc%2Fpasswd", "a%5Cb", "x" * 600])
def test_paths_stay_inside_the_workspace(api, path):
    sb = api.sandbox()
    status, _, doc = api.call("PUT", f"/v1/sandboxes/{sb['id']}/files/{path}", b"x")
    assert problem(status, doc, "bad-path") == 400


def test_links_made_in_the_terminal_lead_nowhere(api, tmp_path):
    sb = api.sandbox()
    base = f"/v1/sandboxes/{sb['id']}/files"
    outside = tmp_path / "outside"
    outside.mkdir()
    (outside / "secret").write_text("no")
    ws = api.workspaces / sb["id"]
    os.symlink(outside, ws / "dir")
    os.symlink(outside / "secret", ws / "file")
    assert api.call("GET", base)[2]["files"] == [], "links are not listed"
    assert api.call("GET", f"{base}/dir/secret")[0] == 400
    assert api.call("GET", f"{base}/file")[0] == 404
    assert api.call("PUT", f"{base}/dir/new", b"x")[0] == 400
    assert api.call("PUT", f"{base}/file", b"x")[0] == 400
    assert api.call("DELETE", f"{base}/file")[0] == 404
    assert (outside / "secret").read_text() == "no" and not (outside / "new").exists()
    wait_job(api, api.call("DELETE", f"/v1/sandboxes/{sb['id']}")[2]["id"])
    assert not ws.exists() and (outside / "secret").exists()


def test_size_limits(api, monkeypatch):
    monkeypatch.setattr(apiserver, "MAX_FILE", 10)
    monkeypatch.setattr(apiserver, "MAX_TOTAL", 15)
    sb = api.sandbox()
    base = f"/v1/sandboxes/{sb['id']}/files"
    status, _, doc = api.call("PUT", f"{base}/a", b"x" * 11)
    assert problem(status, doc, "too-large") == 413
    assert api.call("PUT", f"{base}/a", b"x" * 10)[0] == 204
    assert api.call("PUT", f"{base}/a", b"y" * 10)[0] == 204, "a rewrite replaces its own size"
    status, _, doc = api.call("PUT", f"{base}/b", b"x" * 6)
    assert problem(status, doc, "too-large") == 413
    status, _, doc = api.call("POST", "/v1/sandboxes", {"contract": 1, "kind": "lab", "label": "big",
                                                        "files": {"a": "x" * 11}})
    assert problem(status, doc, "too-large") == 413, "a sandbox's first files too"
    assert len(api.call("GET", "/v1/sandboxes")[2]["sandboxes"]) == 1, "and it was not made"


# Terminal


def ws_connect(api, sid, query=""):
    sock = socket.create_connection(("127.0.0.1", api.port), timeout=10)
    key = base64.b64encode(os.urandom(16)).decode()
    sock.sendall((f"GET /v1/sandboxes/{sid}/terminal?{query} HTTP/1.1\r\nHost: 127.0.0.1:{api.port}\r\nUpgrade: websocket\r\n"
                  f"Connection: Upgrade\r\nSec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\n"
                  f"Authorization: Bearer {api.token}\r\n\r\n").encode())
    head = b""
    while b"\r\n\r\n" not in head:
        head += sock.recv(1)
    return sock, head.decode()


def ws_send(sock, payload):
    data, mask = json.dumps(payload).encode(), os.urandom(4)
    size = bytes([0x80 | len(data)]) if len(data) < 126 else struct.pack("!BH", 0x80 | 126, len(data))
    sock.sendall(bytes([0x81]) + size + mask + bytes(b ^ mask[i % 4] for i, b in enumerate(data)))


def ws_frames(sock):
    """(opcode, payload) frames until the server closes."""
    buf = b""

    def need(n):
        nonlocal buf
        while len(buf) < n:
            chunk = sock.recv(65536)
            if not chunk:
                raise EOFError
            buf += chunk

    while True:
        need(2)
        opcode, n = buf[0] & 0x0F, buf[1] & 0x7F
        skip = 2
        if n == 126:
            need(4)
            n, skip = struct.unpack("!H", buf[2:4])[0], 4
        elif n == 127:
            need(10)
            n, skip = struct.unpack("!Q", buf[2:10])[0], 10
        need(skip + n)
        payload, buf = buf[skip:skip + n], buf[skip + n:]
        yield opcode, payload
        if opcode == 0x8:
            return


def shell(api, sid, command, query=""):
    """What the sandbox's terminal prints for `command` (then exit), and the close code."""
    sock, head = ws_connect(api, sid, query)
    assert head.startswith("HTTP/1.1 101"), head
    ws_send(sock, {"t": "i", "d": command + "; exit\n"})
    out, close = b"", None
    for opcode, payload in ws_frames(sock):
        if opcode == 0x2:
            out += payload
        elif opcode == 0x8:
            close = struct.unpack("!H", payload[:2])[0]
    sock.settimeout(5)
    assert sock.recv(1) == b"", "a client that does not answer the close is hung up"
    sock.close()
    return out.decode(errors="replace"), close


def test_the_terminal_is_a_shell_in_the_workspace(api, monkeypatch):
    monkeypatch.setattr(apiserver, "CLOSE_WAIT", 0.5)
    sb = api.sandbox(files={"hello.yaml": "kind: ConfigMap\n"}, namespace=True)
    text, close = shell(api, sb["id"], "type opsforge | head -1; pwd; cat hello.yaml; stty size; echo ns=$OPSFORGE_NAMESPACE; "
                        "set -o | grep -q 'monitor *on' && echo JOB-CONTROL", "cols=100&rows=30")
    assert "opsforge is a function" in text and str(api.workspaces / sb["id"]) in text
    assert "kind: ConfigMap" in text and "30 100" in text and "JOB-CONTROL" in text and close == 1000
    assert f"ns=sb-{sb['id']}" in text, "opsforge kubectl defaults to the sandbox's namespace"
    assert "no job control" not in text, "the pty is the shell's controlling terminal"


def test_deleting_the_sandbox_hangs_its_terminals_up(api, monkeypatch):
    monkeypatch.setattr(apiserver, "CLOSE_WAIT", 0.5)
    sb = api.sandbox()
    sock, head = ws_connect(api, sb["id"])
    assert head.startswith("HTTP/1.1 101")
    ws_send(sock, {"t": "i", "d": "echo READY\n"})
    frames = ws_frames(sock)
    out = b""
    while b"READY\r\n" not in out.split(b"echo READY", 1)[-1]:
        out += next(frames)[1]
    wait_job(api, api.call("DELETE", f"/v1/sandboxes/{sb['id']}")[2]["id"])
    assert [op for op, _ in frames][-1] == 0x8, "the shell was hung up: the server closed"
    sock.close()
    assert not api.sandboxes.items


def test_the_terminal_needs_the_token_and_a_sandbox(api):
    sock = socket.create_connection(("127.0.0.1", api.port), timeout=10)
    sock.sendall(f"GET /v1/sandboxes/0123abcd/terminal HTTP/1.1\r\nHost: 127.0.0.1:{api.port}\r\n"
                 "Upgrade: websocket\r\n\r\n".encode())
    assert sock.recv(4096).startswith(b"HTTP/1.1 401")
    sock.close()
    sock, head = ws_connect(api, "0123abcd")
    assert head.startswith("HTTP/1.1 404")
    sock.close()


# The OpenAPI document and the server agree


def test_every_route_is_documented_and_every_documented_route_served():
    served = set()
    for pattern, method, _, public in apiserver.ROUTES:
        path = pattern.replace("([a-f0-9]{8})", "{sandbox}").replace("([a-f0-9]{16})", "{job}")
        path = path.replace("(.+)", "{path}")
        served.add((path, method.lower()))
        op = OPENAPI["paths"][path][method.lower()]
        assert (op.get("security") == []) == public, f"{method} {path}: token requirement"
    documented = {(p, m) for p, item in OPENAPI["paths"].items() for m in item if m != "parameters"}
    assert served == documented


def test_openapi_refs_resolve():
    schema = json.loads((ROOT / "contract" / "toolchain.schema.json").read_text())
    refs = re.findall(r"\./toolchain\.schema\.json#/\$defs/(\w+)", (ROOT / "contract" / "openapi.yaml").read_text())
    assert refs and all(r in schema["$defs"] for r in refs)
