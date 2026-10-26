"""`opsforge api serve` (docs/PLAN.md Phase 11, split step 4): OpsForge's machine interface over HTTP, so a client
(OpsForge Academy) never needs this checkout or this machine. contract/openapi.yaml maps the documents of
contract/toolchain.schema.json to routes under /v1/. Stdlib only: a threading HTTP server, lab work as jobs (a setup or
a probe waits minutes for the cluster), the learners' workspaces under .opsforge/workspaces/ and a WebSocket terminal
on a pty (Phase 10's wire format) started in one of them.

Every call but GET /v1/version needs the bearer token of .opsforge/api-token: the terminal is a shell on this host.
Requests from browsers (any Origin header) are refused, and on a loopback address the Host must name it (DNS
rebinding).
"""

import base64
import errno
import fcntl
import hashlib
import hmac
import ipaddress
import json
import os
import re
import secrets
import shutil
import socket
import signal
import ssl
import stat
import struct
import sys
import subprocess
import termios
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, unquote, urlparse

WS_GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
DEFAULT_PORT = 8091
MAX_BODY = 2 << 20  # a request body (a file is at most MAX_FILE)
MAX_FILE = 1 << 20  # Academy's workspace limits (internal/workspace)
MAX_TOTAL = 10 << 20
MAX_PATH = 512
JOB_KEEP = 3600  # seconds a finished job stays readable
MAX_WAIT = 60  # longest ?wait= on GET /v1/jobs/{id}
CLOSE_WAIT = 5  # seconds a terminal client gets to answer the server's close
_WORKSPACE_ID = re.compile(r"[a-z0-9][a-z0-9-]{0,62}")


class ApiError(Exception):
    """A request that fails with a problem document."""

    def __init__(self, status, slug, title, detail):
        super().__init__(detail)
        self.status, self.slug, self.title, self.detail = status, slug, title, detail


def not_found(detail):
    return ApiError(404, "not-found", "Not found", detail)


def bad_path(detail):
    return ApiError(400, "bad-path", "Not a path inside the workspace", detail)


def load_token(path):
    """The API token, created (0600) on first use."""
    path = Path(path)
    if not path.exists():
        path.parent.mkdir(parents=True, exist_ok=True)
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "w") as f:
            f.write(secrets.token_urlsafe(32) + "\n")
    return path.read_text().strip()


# Workspaces: one folder per id. Every path is opened component by component from the workspace's folder with
# O_NOFOLLOW, so a symlink made in the terminal never leads the API out of the tree.


def clean_path(p):
    """`p` as a relative path inside a workspace (Academy's workspace.CleanPath rules), else ApiError."""
    if not p or len(p.encode()) > MAX_PATH or "\\" in p or "\0" in p or p.startswith("/"):
        raise bad_path(f"{p!r} is not a relative file path")
    parts = p.split("/")
    if any(seg in ("", ".", "..") for seg in parts):
        raise bad_path(f"{p!r} is not a relative file path")
    return parts


class Workspaces:
    def __init__(self, base):
        self.base = Path(base)
        self.lock = threading.Lock()  # writes are checked against the tree's size

    def dir(self, ws):
        if not _WORKSPACE_ID.fullmatch(ws or ""):
            raise ApiError(400, "bad-workspace", "Bad workspace id", f"{ws!r} is not a workspace id")
        return self.base / ws

    def _root(self, ws):
        try:
            return os.open(self.dir(ws), os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        except FileNotFoundError:
            raise not_found(f"no workspace {ws}") from None

    def _parent(self, root, parts, create=False):
        """An fd of the folder holding parts[-1] (the caller closes it)."""
        fd = os.dup(root)
        try:
            for seg in parts[:-1]:
                if create:
                    try:
                        os.mkdir(seg, 0o755, dir_fd=fd)
                    except FileExistsError:
                        pass
                nxt = os.open(seg, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=fd)
                os.close(fd)
                fd = nxt
        except FileNotFoundError:
            os.close(fd)
            raise not_found(f"no {'/'.join(parts)}") from None
        except OSError as e:
            os.close(fd)
            if e.errno in (errno.ELOOP, errno.ENOTDIR):
                raise bad_path(f"{'/'.join(parts)} goes through a link or a file") from None
            raise
        return fd

    def _regular(self, fd, name):
        try:
            st = os.stat(name, dir_fd=fd, follow_symlinks=False)
        except FileNotFoundError:
            return None
        return st if stat.S_ISREG(st.st_mode) else None

    def create(self, ws):
        self.base.mkdir(parents=True, exist_ok=True)
        try:
            os.mkdir(self.dir(ws), 0o755)
        except FileExistsError:
            raise ApiError(409, "exists", "Already exists", f"workspace {ws} exists") from None

    def delete(self, ws):
        path = self.dir(ws)
        if not path.is_dir() or path.is_symlink():
            raise not_found(f"no workspace {ws}")
        shutil.rmtree(path)  # never follows links (fd-based on Linux)

    def list(self, ws):
        root = self._root(ws)
        files = []

        def walk(fd, prefix):
            with os.scandir(fd) as it:
                entries = list(it)
            for e in entries:
                if e.is_dir(follow_symlinks=False):
                    sub = os.open(e.name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=fd)
                    try:
                        walk(sub, prefix + e.name + "/")
                    finally:
                        os.close(sub)
                elif e.is_file(follow_symlinks=False):
                    files.append({"path": prefix + e.name, "size": e.stat(follow_symlinks=False).st_size})

        try:
            walk(root, "")
        finally:
            os.close(root)
        files.sort(key=lambda f: f["path"])
        return files

    def read(self, ws, path):
        parts = clean_path(path)
        root = self._root(ws)
        try:
            fd = self._parent(root, parts)
            try:
                if not self._regular(fd, parts[-1]):
                    raise not_found(f"no file {path}")
                f = os.open(parts[-1], os.O_RDONLY | os.O_NOFOLLOW, dir_fd=fd)
                with os.fdopen(f, "rb") as fh:
                    return fh.read(MAX_FILE + 1)
            finally:
                os.close(fd)
        finally:
            os.close(root)

    def write(self, ws, path, data):
        parts = clean_path(path)
        if len(data) > MAX_FILE:
            raise ApiError(413, "too-large", "Too large", f"{path}: files are at most {MAX_FILE} bytes")
        with self.lock:
            files = self.list(ws)
            total = sum(f["size"] for f in files if f["path"] != path) + len(data)
            if total > MAX_TOTAL:
                raise ApiError(413, "too-large", "Too large", f"workspace {ws} would be over {MAX_TOTAL} bytes")
            root = self._root(ws)
            try:
                fd = self._parent(root, parts, create=True)
                try:
                    try:
                        if not stat.S_ISREG(os.stat(parts[-1], dir_fd=fd, follow_symlinks=False).st_mode):
                            raise bad_path(f"{path} is not a regular file")
                    except FileNotFoundError:
                        pass
                    f = os.open(parts[-1], os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW, 0o644, dir_fd=fd)
                    with os.fdopen(f, "wb") as fh:
                        fh.write(data)
                finally:
                    os.close(fd)
            finally:
                os.close(root)

    def remove(self, ws, path):
        parts = clean_path(path)
        root = self._root(ws)
        try:
            fd = self._parent(root, parts)
            try:
                if not self._regular(fd, parts[-1]):
                    raise not_found(f"no file {path}")
                os.unlink(parts[-1], dir_fd=fd)
            finally:
                os.close(fd)
        finally:
            os.close(root)

    def move(self, ws, src, dst):
        sparts, dparts = clean_path(src), clean_path(dst)
        root = self._root(ws)
        try:
            sfd = self._parent(root, sparts)
            try:
                if not self._regular(sfd, sparts[-1]):
                    raise not_found(f"no file {src}")
                dfd = self._parent(root, dparts, create=True)
                try:
                    try:
                        os.stat(dparts[-1], dir_fd=dfd, follow_symlinks=False)
                        raise ApiError(409, "exists", "Already exists", f"{dst} exists")
                    except FileNotFoundError:
                        pass
                    os.rename(sparts[-1], dparts[-1], src_dir_fd=sfd, dst_dir_fd=dfd)
                finally:
                    os.close(dfd)
            finally:
                os.close(sfd)
        finally:
            os.close(root)


# Jobs: lab work runs in a thread, one at a time (the lab is one); clients poll GET /v1/jobs/{id}?wait=s


class Job:
    def __init__(self, kind):
        self.id, self.kind = secrets.token_hex(8), kind
        self.status, self.log, self.output, self.problem = "running", [], None, None
        self.finished_at = None
        self.done = threading.Event()

    def doc(self, contract):
        return {"contract": contract, "id": self.id, "kind": self.kind, "status": self.status, "log": list(self.log),
                "output": self.output, "problem": self.problem}


class Jobs:
    def __init__(self, cli):
        self.cli = cli
        self.jobs = {}
        self.lock = threading.Lock()
        self.running = None

    def start(self, kind, work):
        """Starts `work(lines)` (returns the output document) as a job; ApiError 409 while another one runs."""
        with self.lock:
            if self.running and not self.running.done.is_set():
                raise ApiError(409, "lab-busy", "The lab is busy",
                               f"{self.running.kind} job {self.running.id} is running; one lab job at a time")
            now = time.monotonic()
            self.jobs = {i: j for i, j in self.jobs.items() if not j.finished_at or now - j.finished_at < JOB_KEEP}
            job = self.running = Job(kind)
            self.jobs[job.id] = job
        threading.Thread(target=self._run, args=(job, work), daemon=True).start()
        return job

    def _run(self, job, work):
        cli = self.cli
        try:
            job.output = work(job.log)
            job.status = "done"
        except cli.Problem as e:
            job.problem, job.status = e.doc, "failed"
        except cli.LabError as e:
            job.problem, job.status = cli.Problem("lab-error", "Lab error", str(e)).doc, "failed"
        except Exception as e:  # noqa: BLE001 (a job must always end; the server logs it)
            job.problem = cli.Problem("internal", "Internal error", f"{type(e).__name__}: {e}").doc
            job.status = "failed"
        finally:
            job.finished_at = time.monotonic()
            job.done.set()

    def get(self, job_id, wait=0.0):
        job = self.jobs.get(job_id)
        if not job:
            raise not_found(f"no job {job_id} (finished jobs are kept {JOB_KEEP} s, and none survive a restart)")
        job.done.wait(wait)
        return job

    def wait_idle(self):
        job = self.running
        if job and not job.done.is_set():
            print(f"waiting for {job.kind} job {job.id} to finish", flush=True)
            job.done.wait()


class Server(ThreadingHTTPServer):
    daemon_threads = True

    def handle_error(self, request, client_address):
        if not isinstance(sys.exc_info()[1], (ConnectionResetError, BrokenPipeError, TimeoutError)):
            super().handle_error(request, client_address)

    def __init__(self, address, cli, token, workspaces, rcfile):
        super().__init__(address, Handler)
        self.cli, self.token = cli, token
        self.jobs, self.workspaces, self.rcfile = Jobs(cli), workspaces, rcfile
        host = address[0]
        try:
            loopback = ipaddress.ip_address(host).is_loopback
        except ValueError:
            loopback = host == "localhost"
        port = self.server_address[1]
        self.hosts = {f"127.0.0.1:{port}", f"localhost:{port}", f"[::1]:{port}"} if loopback else None


BASHRC = """# Generated by opsforge api serve: the lab terminal's shell
HISTFILE={history}
PS1='\\[\\e[1m\\]lab\\[\\e[0m\\] \\W \\$ '
opsforge() {{ {cli} "$@"; }}
if [ -n "${{OPSFORGE_WORKDIR:-}}" ]; then cd "$OPSFORGE_WORKDIR"; else cd {root}; fi
printf 'Lab terminal in %s. Lab commands start with opsforge, e.g. opsforge kubectl get nodes\\n' "$PWD"
"""


def shell_quote(s):
    return "'" + str(s).replace("'", "'\\''") + "'"


class Handler(BaseHTTPRequestHandler):
    server_version = "opsforge-api"
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *args):
        if not str(args[0]).startswith("GET /v1/jobs/"):
            super().log_message(fmt, *args)

    @property
    def contract(self):
        return self.server.cli.contract.CONTRACT

    def send_doc(self, status, doc, content_type="application/json"):
        data = doc if isinstance(doc, bytes) else json.dumps(doc).encode()
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(data)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(data)

    def send_problem(self, e):
        self.send_doc(e.status, {"contract": self.contract, "type": f"/problems/{e.slug}", "title": e.title,
                                 "detail": e.detail}, "application/problem+json")

    def body(self):
        try:
            length = int(self.headers.get("Content-Length") or 0)
        except ValueError:
            raise ApiError(400, "bad-input", "Bad input", "Content-Length is not a number") from None
        if length > MAX_BODY:
            raise ApiError(413, "too-large", "Too large", f"request bodies are at most {MAX_BODY} bytes")
        return self.rfile.read(length)

    def json_input(self, kind):
        try:
            return self.server.cli.api_input(kind, self.body().decode())
        except UnicodeDecodeError:
            raise ApiError(400, "bad-input", "Bad input", "the body is not UTF-8") from None
        except self.server.cli.Problem as e:
            raise ApiError(400, e.doc["type"].rsplit("/", 1)[1], e.doc["title"], e.doc["detail"]) from None

    def guard(self, public):
        if self.server.hosts is not None and self.headers.get("Host", "") not in self.server.hosts:
            raise ApiError(421, "wrong-host", "Wrong host", "this API answers only for its loopback address")
        if self.headers.get("Origin") is not None:
            raise ApiError(403, "cross-origin", "Cross-origin request", "browsers cannot call this API")
        if public:
            return
        given = self.headers.get("Authorization", "")
        if not (given.startswith("Bearer ") and hmac.compare_digest(given[7:].encode(), self.server.token.encode())):
            raise ApiError(401, "no-token", "Unauthorized", "send the token of .opsforge/api-token as a Bearer token")

    def do_GET(self):
        self.dispatch("GET")

    def do_POST(self):
        self.dispatch("POST")

    def do_PUT(self):
        self.dispatch("PUT")

    def do_DELETE(self):
        self.dispatch("DELETE")

    def dispatch(self, method):
        url = urlparse(self.path)
        try:
            for pattern, verb, handler, public in ROUTES:
                m = re.fullmatch(pattern, url.path)
                if m and verb == method:
                    self.guard(public)
                    handler(self, parse_qs(url.query), *(unquote(g) for g in m.groups()))
                    return
            if any(re.fullmatch(p, url.path) for p, *_ in ROUTES):
                raise ApiError(405, "method-not-allowed", "Method not allowed", f"{method} {url.path}")
            raise not_found(f"no route {url.path}")
        except ApiError as e:
            self.send_problem(e)
        except (BrokenPipeError, ConnectionResetError):
            pass
        except Exception as e:  # noqa: BLE001 (answered as a problem, logged)
            self.log_error("%s %s: %s: %s", method, url.path, type(e).__name__, e)
            self.send_problem(ApiError(500, "internal", "Internal error", f"{type(e).__name__}: {e}"))

    # Routes
    def version(self, _query):
        cli = self.server.cli
        self.send_doc(200, {"contract": cli.contract.CONTRACT, "minor": cli.contract.CONTRACT_MINOR,
                            "opsforge": cli.VERSION})

    def state(self, _query):
        self.send_doc(200, self.server.cli.api_state())

    def validate_steps(self, _query):
        self.send_doc(200, self.server.cli.api_validate_steps(self.json_input("stepsInput")))

    def start(self, kind, work):
        job = self.server.jobs.start(kind, work)
        self.send_response(202)
        data = json.dumps(job.doc(self.contract)).encode()
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.send_header("Location", f"/v1/jobs/{job.id}")
        self.end_headers()
        self.wfile.write(data)

    def run(self, _query):
        doc = self.json_input("runInput")
        self.start("run", lambda lines: self.server.cli.api_run(doc, lines=lines))

    def probe(self, _query):
        doc = self.json_input("probeInput")
        self.start("probe", lambda lines: self.server.cli.api_probe(doc, lines=lines))

    def heal(self, _query):
        self.start("heal", lambda _lines: self.server.cli.api_heal())

    def job(self, query, job_id):
        try:
            wait = min(float((query.get("wait") or ["0"])[0]), MAX_WAIT)
        except ValueError:
            raise ApiError(400, "bad-input", "Bad input", "wait is a number of seconds") from None
        self.send_doc(200, self.server.jobs.get(job_id, max(wait, 0)).doc(self.contract))

    def workspace_create(self, _query, ws):
        self.server.workspaces.create(ws)
        self.send_response(201)
        self.send_header("Content-Length", "0")
        self.end_headers()

    def workspace_delete(self, _query, ws):
        self.server.workspaces.delete(ws)
        self.no_content()

    def files(self, _query, ws):
        files = self.server.workspaces.list(ws)
        self.send_doc(200, {"contract": self.contract, "files": files, "bytes": sum(f["size"] for f in files)})

    def file_read(self, _query, ws, path):
        self.send_doc(200, self.server.workspaces.read(ws, path), "application/octet-stream")

    def file_write(self, _query, ws, path):
        self.server.workspaces.write(ws, path, self.body())
        self.no_content()

    def file_delete(self, _query, ws, path):
        self.server.workspaces.remove(ws, path)
        self.no_content()

    def move(self, _query, ws):
        doc = self.json_input("moveInput")
        self.server.workspaces.move(ws, doc["from"], doc["to"])
        self.no_content()

    def no_content(self):
        self.send_response(204)
        self.end_headers()

    # Terminal: RFC 6455 framing over the request's socket, bash on a pty. Client frames (text):
    # {"t": "i", "d": input} or {"t": "r", "cols", "rows"}; server frames (binary): output; close 1000 when the
    # shell exits. Closing the socket hangs the shell up.
    def terminal(self, query):
        def number(name, default):
            try:
                return max(1, min(int((query.get(name) or [default])[0]), 1000))
            except ValueError:
                raise ApiError(400, "bad-input", "Bad input", f"{name} is a number") from None

        cols, rows = number("cols", 80), number("rows", 24)
        workdir = None
        if ws := (query.get("workspace") or [None])[0]:
            workdir = self.server.workspaces.dir(ws)
            if not workdir.is_dir() or workdir.is_symlink():
                raise not_found(f"no workspace {ws}")
        key = self.headers.get("Sec-WebSocket-Key")
        if self.headers.get("Upgrade", "").lower() != "websocket" or not key:
            raise ApiError(400, "not-websocket", "Not a WebSocket", "expected a WebSocket upgrade")
        accept = base64.b64encode(hashlib.sha1((key + WS_GUID).encode()).digest()).decode()
        self.send_response(101)
        self.send_header("Upgrade", "websocket")
        self.send_header("Connection", "Upgrade")
        self.send_header("Sec-WebSocket-Accept", accept)
        self.end_headers()
        self.close_connection = True
        env = dict(os.environ, TERM="xterm-256color")
        if workdir:
            env["OPSFORGE_WORKDIR"] = str(workdir)
        # No pty.fork: forking a threaded server may deadlock the child. setsid --ctty makes bash a session leader
        # with the pty as its controlling terminal (job control, Ctrl-C), from a plain subprocess.
        fd, child = os.openpty()
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
        try:
            shell = subprocess.Popen(["setsid", "--ctty", "bash", "--rcfile", str(self.server.rcfile), "-i"],
                                     stdin=child, stdout=child, stderr=child, cwd=self.server.cli.ROOT, env=env)
        finally:
            os.close(child)
        pid = shell.pid
        lock = threading.Lock()

        def send(opcode, payload):
            n = len(payload)
            header = bytes([0x80 | opcode])
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
            # A client that never answers the close must not hold this thread and the pty: hang up after a while
            threading.Timer(CLOSE_WAIT, hang_up).start()

        def hang_up():
            try:
                self.connection.shutdown(socket.SHUT_RDWR)
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
                        size = struct.pack("HHHH", int(msg["rows"]), int(msg["cols"]), 0, 0)
                        fcntl.ioctl(fd, termios.TIOCSWINSZ, size)
        except (OSError, ValueError, KeyError, TypeError, AttributeError):
            pass
        finally:
            try:
                os.killpg(pid, signal.SIGHUP)
            except OSError:
                pass
            try:
                shell.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(pid, signal.SIGKILL)
                shell.wait()
            os.close(fd)

    def read_frame(self):
        """(opcode, payload) of the next client frame, or (None, None) when the socket closes."""
        head = self.rfile.read(2)
        if len(head) < 2:
            return None, None
        opcode, n = head[0] & 0x0F, head[1] & 0x7F
        if n == 126:
            n = struct.unpack("!H", self.rfile.read(2))[0]
        elif n == 127:
            n = struct.unpack("!Q", self.rfile.read(8))[0]
        if n > MAX_BODY:
            return None, None
        mask = self.rfile.read(4) if head[1] & 0x80 else b"\0\0\0\0"
        data = self.rfile.read(n)
        return opcode, bytes(b ^ mask[i % 4] for i, b in enumerate(data))


_WS = r"/v1/workspaces/([a-z0-9][a-z0-9-]{0,62})"
# (path pattern, method, handler, public: no token needed)
ROUTES = [
    (r"/v1/version", "GET", Handler.version, True),
    (r"/v1/state", "GET", Handler.state, False),
    (r"/v1/steps/validate", "POST", Handler.validate_steps, False),
    (r"/v1/runs", "POST", Handler.run, False),
    (r"/v1/probes", "POST", Handler.probe, False),
    (r"/v1/heal", "POST", Handler.heal, False),
    (r"/v1/jobs/([a-f0-9]{16})", "GET", Handler.job, False),
    (_WS, "PUT", Handler.workspace_create, False),
    (_WS, "DELETE", Handler.workspace_delete, False),
    (_WS + r"/files", "GET", Handler.files, False),
    (_WS + r"/files/(.+)", "GET", Handler.file_read, False),
    (_WS + r"/files/(.+)", "PUT", Handler.file_write, False),
    (_WS + r"/files/(.+)", "DELETE", Handler.file_delete, False),
    (_WS + r"/moves", "POST", Handler.move, False),
    (r"/v1/terminal", "GET", Handler.terminal, False),
]


def make_server(cli, host, port, tls_cert=None, tls_key=None, data=None):
    """The API server for this checkout (`cli` is the opsforge module): token, workspaces and the shell's rcfile
    in `data` (default .opsforge/; tests and tools give their own)."""
    data = Path(data or cli.STATE)
    data.mkdir(parents=True, exist_ok=True)
    token = load_token(data / "api-token")
    rcfile = data / "api-bashrc"
    rcfile.write_text(BASHRC.format(history=shell_quote(data / "api-history"), cli=shell_quote(cli.ROOT / "opsforge"),
                                    root=shell_quote(cli.ROOT)))
    server = Server((host, port), cli, token, Workspaces(data / "workspaces"), rcfile)
    if tls_cert:
        ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        ctx.load_cert_chain(tls_cert, tls_key)
        server.socket = ctx.wrap_socket(server.socket, server_side=True)
    return server


def serve(cli, host, port, tls_cert=None, tls_key=None, data=None):
    """Serves until SIGINT/SIGTERM; a running lab job is waited for (stopping halfway through a setup would leave
    the lab half-changed)."""
    server = make_server(cli, host, port, tls_cert, tls_key, data)
    scheme = "https" if tls_cert else "http"
    print(f"opsforge api on {scheme}://{host}:{server.server_address[1]}/v1/ (token: "
          f"{Path(data or cli.STATE) / 'api-token'})", flush=True)

    def stop(*_):
        threading.Thread(target=server.shutdown, daemon=True).start()

    signal.signal(signal.SIGTERM, stop)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.jobs.wait_idle()
        server.server_close()
