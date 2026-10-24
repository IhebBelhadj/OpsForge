"""`opsforge serve` (docs/PLAN.md Phase 10): the teaching-lab web interface on 127.0.0.1. A stdlib HTTP server
with a JSON API over lab/labsdb.py, checks run on the lab with the CLI's ScenarioRun, and a browser terminal (a pty
running bash in the repo, over a stdlib WebSocket). Static files come from lab/web/.

Only requests whose Host is 127.0.0.1/localhost on our port are served, and changes (POST, the terminal) also need
a matching Origin: any web page can open a WebSocket to localhost, and the terminal is a shell.
"""

import base64
import fcntl
import hashlib
import json
import mimetypes
import os
import pty
import re
import signal
import struct
import subprocess
import termios
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlparse

import labs
import labsdb
import scenario

WS_GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
# The panel polls the lab state; Incus is asked at most this often
STATE_TTL = 2.0


class LabDriver:
    """What the server needs from the lab, through the CLI module (`opsforge`, loaded by cmd_serve)."""

    def __init__(self, cli):
        self.cli = cli
        # One lab operation (setup, check, teardown) at a time: they share the links and the cluster
        self.busy = threading.Lock()
        self._state_lock = threading.Lock()
        self._state = (0.0, None)

    def topology(self):
        try:
            return self.cli.load_valid_topology()
        except (self.cli.LabError, OSError):
            return None

    def state(self):
        with self._state_lock:
            stamp, cached = self._state
            if cached is None or time.monotonic() - stamp > STATE_TTL:
                cached = self._read_state()
                self._state = (time.monotonic(), cached)
            return cached

    def forget_state(self):
        with self._state_lock:
            self._state = (0.0, None)

    def _read_state(self):
        return self.cli.lab_state()

    def run(self, steps, phase, stop=None):
        """Runs scenario steps (the CLI's lab_run): (metrics, log lines, error message or None)."""
        try:
            return self.cli.lab_run(steps, phase, stop)
        finally:
            self.forget_state()

    def probe(self, arg):
        """Runs a probe check (the CLI's lab_probe)."""
        try:
            return self.cli.lab_probe(arg)
        finally:
            self.forget_state()

    def heal(self):
        try:
            self.cli.heal_links()
        except self.cli.LabError:
            pass
        self.forget_state()


class ApiError(Exception):
    def __init__(self, status, message):
        super().__init__(message)
        self.status = status


def lab_view(conn, user, lab_id, topo):
    """A lab as the workspace shows it: texts resolved for the topology, quiz answers withheld, the running
    attempt's progress and the hints already opened."""
    lab = labsdb.get_lab(conn, lab_id)
    if lab is None or lab["retired"]:
        raise ApiError(404, f"no lab {lab_id}")
    body = lab["body"]
    sub = lambda text: labs.substitute(text, topo)  # noqa: E731
    attempt = labsdb.running_attempt(conn, user)
    attempt = attempt if attempt and attempt["lab_id"] == lab_id else None
    done = labsdb.passed_steps(conn, attempt["id"]) if attempt else set()
    shown = labsdb.shown_hints(conn, attempt["id"]) if attempt else {}
    steps = []
    for step in body["steps"]:
        kind = labs.check_type(step["check"])
        arg = step["check"][kind]
        check = {"type": kind}
        if kind == "quiz":
            check |= {"question": sub(arg.get("question", "")), "choices": [sub(str(c)) for c in arg["choices"]]}
        elif kind == "confirm":
            check["label"] = arg
        hints = step.get("hints", [])
        steps.append({"id": step["id"], "title": sub(step["title"]), "body": sub(step["body"]), "check": check,
                      "hints_total": len(hints), "hints": [sub(h) for h in hints[:shown.get(step["id"], 0)]],
                      "passed": step["id"] in done})
    passes = labsdb.best_passes(conn, user)
    return {
        "id": lab_id, "title": lab["title"], "summary": lab["summary"], "xp": lab["xp"], "minutes": lab["minutes"],
        "briefing": sub(body["briefing"]), "debrief": sub(body.get("debrief", "")),
        "concepts": body.get("concepts", []), "layout": body.get("layout"), "needs": body.get("needs", []),
        "unavailable": labs.unmet_needs(body, topo) if topo else ["the topology is invalid"],
        "locked": labsdb.lab_locked(conn, user, lab_id), "hint_penalty": lab["hint_penalty"],
        "steps": steps, "attempt": attempt and {"id": attempt["id"], "started_at": attempt["started_at"],
                                                "hints_used": attempt["hints_used"]},
        "passed": lab_id in passes, "best_xp": passes.get(lab_id, {}).get("xp", 0),
    }


class Server(ThreadingHTTPServer):
    daemon_threads = True

    def __init__(self, address, driver, db_path, root, web):
        super().__init__(address, Handler)
        self.driver, self.db_path, self.root, self.web = driver, db_path, root, web
        self.rcfile = Path(db_path).parent / "labs-bashrc"
        self.rcfile.write_text(BASHRC.format(root=root))
        conn = labsdb.connect(db_path)
        self.user = labsdb.local_user(conn)
        conn.close()

    def allowed_hosts(self):
        port = self.server_address[1]
        return {f"127.0.0.1:{port}", f"localhost:{port}"}


BASHRC = """# Generated by opsforge serve: the lab terminal's shell
HISTFILE="{root}/.opsforge/labs-history"
PS1='\\[\\e[1m\\]lab\\[\\e[0m\\] \\W \\$ '
opsforge() {{ "{root}/opsforge" "$@"; }}
cd "{root}"
printf 'Lab terminal in %s. Lab commands start with opsforge, e.g. opsforge kubectl get nodes\\n' "$PWD"
"""


class Handler(BaseHTTPRequestHandler):
    server_version = "opsforge-labs"
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *args):
        if not str(args[0]).startswith(("GET /api/state", "GET /static")):
            super().log_message(fmt, *args)

    # Plumbing
    def send_json(self, status, payload):
        data = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(data)

    def body(self):
        length = int(self.headers.get("Content-Length") or 0)
        if not length:
            return {}
        try:
            return json.loads(self.rfile.read(length))
        except json.JSONDecodeError:
            raise ApiError(400, "the request body is not JSON") from None

    def trusted(self, change):
        host = self.headers.get("Host", "")
        if host not in self.server.allowed_hosts():
            return False
        return not change or self.headers.get("Origin") == f"http://{host}"

    def do_GET(self):
        self.dispatch("GET")

    def do_POST(self):
        self.dispatch("POST")

    def dispatch(self, method):
        path = urlparse(self.path).path
        terminal = path == "/ws/terminal"
        if not self.trusted(change=method == "POST" or terminal):
            self.send_json(403, {"error": "only the lab's own page on 127.0.0.1 may use this server"})
            return
        if terminal:
            self.terminal()
            return
        if not path.startswith("/api/"):
            self.static(path)
            return
        conn = labsdb.connect(self.server.db_path)
        try:
            for pattern, route_method, handler in ROUTES:
                m = re.fullmatch(pattern, path)
                if m and route_method == method:
                    self.send_json(200, handler(self, conn, *m.groups()))
                    return
            raise ApiError(404, f"no route {method} {path}")
        except ApiError as e:
            self.send_json(e.status, {"error": str(e)})
        finally:
            conn.close()

    def static(self, path):
        name = "index.html" if path in ("/", "") or not path.startswith("/static/") else path[len("/static/"):]
        target = (self.server.web / name).resolve()
        if self.server.web.resolve() not in target.parents or not target.is_file():
            self.send_json(404, {"error": f"no file {path}"})
            return
        data = target.read_bytes()
        self.send_response(200)
        self.send_header("Content-Type", mimetypes.guess_type(target.name)[0] or "application/octet-stream")
        self.send_header("Content-Length", str(len(data)))
        self.send_header("Cache-Control", "no-cache")
        self.end_headers()
        self.wfile.write(data)

    # API
    def api_state(self, conn):
        user = self.server.user
        earned = [b for b in labsdb.badges(conn, user) if b["awarded_at"]]
        running = labsdb.running_attempt(conn, user)
        return {
            "user": {"name": labsdb.LOCAL_USER, "xp": labsdb.total_xp(conn, user), "badges": len(earned),
                     "badges_total": len(labsdb.badges(conn, user))},
            "running": running and {"attempt": running["id"], "lab": running["lab_id"]},
            "lab": self.server.driver.state(),
        }

    def api_catalog(self, conn):
        topo = self.server.driver.topology()
        curricula = labsdb.catalog(conn, self.server.user)
        for lab in (lab for c in curricula for level in c["levels"] for lab in level["labs"]):
            lab["unavailable"] = labs.unmet_needs(lab, topo) if topo else ["the topology is invalid"]
        return {"curricula": curricula}

    def api_lab(self, conn, lab_id):
        return lab_view(conn, self.server.user, lab_id, self.server.driver.topology())

    def api_badges(self, conn):
        return {"badges": labsdb.badges(conn, self.server.user)}

    def api_start(self, conn, lab_id):
        user, driver = self.server.user, self.server.driver
        view = lab_view(conn, user, lab_id, driver.topology())
        if view["locked"]:
            raise ApiError(409, "this lab is locked: pass the labs before it first")
        if view["unavailable"]:
            raise ApiError(409, f"this lab cannot run on {driver.cli.TOPOLOGY.name}: " + "; ".join(view["unavailable"]))
        if not driver.state()["up"]:
            raise ApiError(409, "the lab is down: run ./opsforge up")
        if labsdb.running_attempt(conn, user):
            raise ApiError(409, "another lab is running: finish or abandon it first")
        if not driver.busy.acquire(blocking=False):
            raise ApiError(409, "the lab is busy with another check")
        try:
            attempt = labsdb.start_attempt(conn, user, lab_id)
            _, log, error = driver.run(labsdb.get_lab(conn, lab_id)["body"].get("setup"), "setup")
            if error:
                driver.run(labsdb.get_lab(conn, lab_id)["body"].get("teardown"), "teardown")
                labsdb.abandon(conn, attempt)
                raise ApiError(500, f"setting up the lab failed: {error}\n" + "\n".join(log))
        finally:
            driver.busy.release()
        return lab_view(conn, user, lab_id, driver.topology())

    def attempt(self, conn, attempt_id):
        a = labsdb.get_attempt(conn, int(attempt_id))
        if a is None or a["user_id"] != self.server.user:
            raise ApiError(404, f"no attempt {attempt_id}")
        if a["status"] != "running":
            raise ApiError(409, f"this attempt is already {a['status']}")
        return a, labsdb.get_lab(conn, a["lab_id"])

    def api_check(self, conn, attempt_id):
        a, lab = self.attempt(conn, attempt_id)
        req = self.body()
        steps = lab["body"]["steps"]
        done = labsdb.passed_steps(conn, a["id"])
        current = next((s for s in steps if s["id"] not in done), None)
        if current is None or req.get("step") != current["id"]:
            raise ApiError(409, "check the steps in order" if current else "every step is already passed")
        kind = labs.check_type(current["check"])
        arg = current["check"][kind]
        if kind == "quiz":
            ok = req.get("answer") == arg["answer"]
            detail = {"passed": ok, "explain": labs.substitute(arg.get("explain", ""), self.server.driver.topology())
                      if ok else None}
        elif kind == "confirm":
            detail = {"passed": True}
        else:
            driver = self.server.driver
            if not driver.busy.acquire(blocking=False):
                raise ApiError(409, "the lab is busy with another check")
            try:
                detail = driver.probe(arg)
            finally:
                driver.busy.release()
        labsdb.record_check(conn, a["id"], current["id"], detail["passed"], detail)
        if detail["passed"] and all(s["id"] in done | {current["id"]} for s in steps):
            detail["finished"] = self.finish_attempt(conn, a, lab)
        return detail

    def finish_attempt(self, conn, a, lab):
        driver = self.server.driver
        with driver.busy:
            _, log, error = driver.run(lab["body"].get("teardown"), "teardown")
            driver.heal()
        awarded = labsdb.finish(conn, a["id"])
        passed = labsdb.get_attempt(conn, a["id"])
        return {"xp": passed["xp"], "seconds": passed["seconds"], "badges": awarded,
                "teardown_error": error and f"{error}\n" + "\n".join(log)}

    def api_hint(self, conn, attempt_id):
        a, lab = self.attempt(conn, attempt_id)
        req = self.body()
        step = next((s for s in lab["body"]["steps"] if s["id"] == req.get("step")), None)
        if step is None:
            raise ApiError(404, f"no step {req.get('step')}")
        index = labsdb.shown_hints(conn, a["id"]).get(step["id"], 0)
        hints = step.get("hints", [])
        if index >= len(hints):
            raise ApiError(409, "no more hints for this step")
        labsdb.show_hint(conn, a["id"], step["id"], index)
        return {"hint": labs.substitute(hints[index], self.server.driver.topology()), "index": index,
                "hints_used": labsdb.get_attempt(conn, a["id"])["hints_used"]}

    def api_abandon(self, conn, attempt_id):
        a, lab = self.attempt(conn, attempt_id)
        driver = self.server.driver
        with driver.busy:
            _, log, error = driver.run(lab["body"].get("teardown"), "teardown")
            driver.heal()
        labsdb.abandon(conn, a["id"])
        return {"abandoned": a["id"], "teardown_error": error and f"{error}\n" + "\n".join(log)}

    # Terminal: RFC 6455 framing over the request's socket, bash on a pty
    def terminal(self):
        key = self.headers.get("Sec-WebSocket-Key")
        if self.headers.get("Upgrade", "").lower() != "websocket" or not key:
            self.send_json(400, {"error": "expected a WebSocket upgrade"})
            return
        accept = base64.b64encode(hashlib.sha1((key + WS_GUID).encode()).digest()).decode()
        self.send_response(101)
        self.send_header("Upgrade", "websocket")
        self.send_header("Connection", "Upgrade")
        self.send_header("Sec-WebSocket-Accept", accept)
        self.end_headers()
        self.close_connection = True
        pid, fd = pty.fork()
        if pid == 0:
            env = dict(os.environ, TERM="xterm-256color")
            os.chdir(self.server.root)
            os.execvpe("bash", ["bash", "--rcfile", str(self.server.rcfile), "-i"], env)
        lock = threading.Lock()

        def send(opcode, payload):
            header = bytes([0x80 | opcode])
            n = len(payload)
            header += bytes([n]) if n < 126 else struct.pack("!BH", 126, n) if n < 65536 else struct.pack("!BQ", 127, n)
            with lock:
                self.wfile.write(header + payload)
                self.wfile.flush()

        def pump():
            try:
                while data := os.read(fd, 65536):
                    send(0x2, data)
            except OSError:
                pass
            try:
                send(0x8, struct.pack("!H", 1000))
            except OSError:
                pass

        threading.Thread(target=pump, daemon=True).start()
        try:
            while True:
                opcode, payload = self.read_frame()
                if opcode is None or opcode == 0x8:
                    break
                if opcode == 0x9:
                    send(0xA, payload)
                elif opcode in (0x1, 0x2):
                    msg = json.loads(payload)
                    if msg.get("t") == "i":
                        os.write(fd, msg["d"].encode())
                    elif msg.get("t") == "r":
                        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", int(msg["rows"]), int(msg["cols"]), 0, 0))
        except (OSError, ValueError, KeyError):
            pass
        finally:
            try:
                os.kill(pid, signal.SIGHUP)
                os.waitpid(pid, 0)
            except (OSError, ChildProcessError):
                pass
            os.close(fd)

    def read_frame(self):
        """(opcode, payload) of the next client frame, or (None, None) when the socket closes. Clients always mask."""
        head = self.rfile.read(2)
        if len(head) < 2:
            return None, None
        opcode, n = head[0] & 0x0F, head[1] & 0x7F
        if n == 126:
            n = struct.unpack("!H", self.rfile.read(2))[0]
        elif n == 127:
            n = struct.unpack("!Q", self.rfile.read(8))[0]
        mask = self.rfile.read(4) if head[1] & 0x80 else b"\0\0\0\0"
        data = self.rfile.read(n)
        return opcode, bytes(b ^ mask[i % 4] for i, b in enumerate(data))


ROUTES = [
    (r"/api/state", "GET", Handler.api_state),
    (r"/api/catalog", "GET", Handler.api_catalog),
    (r"/api/badges", "GET", Handler.api_badges),
    (r"/api/labs/([a-z0-9-]+)", "GET", Handler.api_lab),
    (r"/api/labs/([a-z0-9-]+)/start", "POST", Handler.api_start),
    (r"/api/attempts/(\d+)/check", "POST", Handler.api_check),
    (r"/api/attempts/(\d+)/hint", "POST", Handler.api_hint),
    (r"/api/attempts/(\d+)/abandon", "POST", Handler.api_abandon),
]


def serve(cli, port, db_path, root, web):
    server = Server(("127.0.0.1", port), LabDriver(cli), db_path, root, web)
    print(f"OpsForge labs on http://127.0.0.1:{port}/  (Ctrl-C stops it)", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
