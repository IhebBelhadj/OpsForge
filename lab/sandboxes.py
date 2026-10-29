"""Sandboxes (docs/PLAN.md Phase 11, split step 7): the one place a client's lab work happens. A sandbox is made for
one lab or challenge attempt from the client's requirements: a workspace (its files, where its steps and terminal
run), a namespace sb-<id> on every K3s cluster when asked, and its setup. OpsForge, not the client, cleans it up:
a failing setup at once; deleting it runs its teardown, deletes the namespace, heals when it had faults and deletes
the workspace; an idle one expires. Records live in <data>/sandboxes/<id>.json (steps included), so cleanup survives
a restart: lab work a stopped API left halfway is finished by deleting that sandbox when it comes back.

Lab work is a job (apiserver.Jobs: one at a time); the lab side of it is the CLI's (`sandbox_up`, `sandbox_down`,
`sandbox_probe`, `sandbox_action`), so `opsforge api sandbox delete` cleans up without the server.
"""

import json
import os
import secrets
import signal
import threading
import time
from datetime import datetime, timezone
from pathlib import Path

import render

DEFAULT_EXPIRY = 86400  # seconds without a call
REAP_EVERY = 30  # seconds between expiry checks
TOUCH_EVERY = 60  # the last call is saved at most this often
ALIVE = ("creating", "ready", "resetting", "deleting")
FEATURES = ("vpn", "k3s", "monitoring", "ci")


def now():
    return time.time()


def iso(t):
    return datetime.fromtimestamp(t, timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def features(topo):
    """What a topology offers of `requires.needs`."""
    roles = {node["role"] for node in topo["nodes"].values()}
    return {f for f, ok in (("vpn", topo.get("interconnect") == "vpn"), ("k3s", bool(render.k3s_clusters(topo))),
                            ("monitoring", "monitoring" in roles), ("ci", "ci" in roles)) if ok}


def unmet(requires, namespace, state, topo):
    """What the running lab lacks for a sandbox, one sentence each (empty: it can host it)."""
    if topo is None:
        return [state.get("error") or "the topology is invalid"]
    missing = []
    if (want := requires.get("layout")) and want != state.get("layout"):
        missing.append(f"layout {want} (the lab is {state.get('layout')}: OPSFORGE_TOPOLOGY)")
    have = features(topo)
    for need in requires.get("needs", []):
        if need not in have:
            missing.append(f"{need} (not in {Path(state.get('topology') or 'the topology').name})")
    if namespace and "k3s" not in have and "k3s" not in requires.get("needs", []):
        missing.append("k3s (a namespace needs a K3s cluster)")
    return missing


class Sandbox:
    """One sandbox's record. `dirty`: its lab side (namespace, setup's changes) may exist; `terminals`: the process
    groups of its open shells (not saved: a restart ends them)."""

    FIELDS = ("id", "kind", "label", "requires", "namespace", "setup_steps", "teardown_steps", "expires_after",
              "status", "setup", "teardown", "created", "used", "layout", "placeholders", "dirty", "environment")

    def __init__(self, **fields):
        for f in self.FIELDS:
            setattr(self, f, fields.get(f))
        self.terminals = set()
        self.saved_used = self.used

    @property
    def exclusive(self):
        """Runs alone: it changes the lab outside its namespace (faults, cluster changes, its own environment)."""
        return bool(self.requires.get("faults") or self.requires.get("cluster_changes") or self.environment)

    def place(self, workdir):
        return {"sandbox": self.id, "namespace": self.namespace, "workdir": workdir, "environment": self.environment}

    def doc(self, contract, status=None):
        return {"contract": contract, "id": self.id, "kind": self.kind, "label": self.label,
                "status": status or self.status, "namespace": self.namespace, "layout": self.layout,
                "placeholders": self.placeholders, "requires": self.requires, "setup": self.setup,
                "teardown": self.teardown, "created_at": iso(self.created),
                "expires_at": iso(self.used + self.expires_after)}


class Sandboxes:
    def __init__(self, cli, data, workspaces, jobs):
        self.cli, self.workspaces, self.jobs = cli, workspaces, jobs
        self.dir = Path(data) / "sandboxes"
        self.dir.mkdir(parents=True, exist_ok=True)
        self.lock = threading.RLock()  # the records; lab work is serialized by jobs
        self.items = {}
        for path in sorted(self.dir.glob("*.json")):
            sb = Sandbox(**json.loads(path.read_text()))
            if sb.status in ("creating", "resetting", "deleting"):
                # Lab work stopped halfway (the API was killed): the reaper deletes it, teardown included
                sb.status, sb.dirty, sb.used = "deleting", True, 0
            self.items[sb.id] = sb
        self.stop = threading.Event()

    @property
    def contract(self):
        return self.cli.contract.CONTRACT

    def save(self, sb):
        sb.saved_used = sb.used
        tmp = self.dir / f".{sb.id}.tmp"
        tmp.write_text(json.dumps({f: getattr(sb, f) for f in Sandbox.FIELDS}, indent=1) + "\n")
        os.replace(tmp, self.dir / f"{sb.id}.json")

    def forget(self, sb):
        self.items.pop(sb.id, None)
        (self.dir / f"{sb.id}.json").unlink(missing_ok=True)

    def workdir(self, sb):
        return self.workspaces.dir(sb.id)

    def problem(self, status, slug, title, detail, **extra):
        err = self.cli.apiserver.ApiError(status, slug, title, detail)
        err.extra = extra
        return err

    def get(self, sid, touch=True):
        """The sandbox `sid` (404 otherwise); a call keeps it from expiring."""
        with self.lock:
            sb = self.items.get(sid)
            if sb is None:
                raise self.cli.apiserver.not_found(f"no sandbox {sid}")
            if touch and sb.status != "deleting":
                sb.used = now()
                if sb.used - (sb.saved_used or 0) >= TOUCH_EVERY:
                    self.save(sb)
            return sb

    def list(self):
        with self.lock:
            return sorted(self.items.values(), key=lambda sb: (sb.created, sb.id))

    def need(self, sb, *statuses):
        if sb.status not in statuses:
            raise self.problem(409, "sandbox-state", "Sandbox not ready",
                               f"sandbox {sb.id} is {sb.status}; this needs it {' or '.join(statuses)}")

    def steps_errors(self, sets):
        """400 when steps would not run (fault steps where the sandbox did not declare them included, environment
        steps without an environment)."""
        errors = self.cli.contract.step_errors(sets, self.cli.topology_errors)
        if errors:
            names = {0: "setup", 1: "teardown"} if len(sets) == 2 else {0: "steps"}
            raise self.problem(400, "bad-input", "Bad input",
                               "; ".join(f"{names.get(e['set'], 'steps')}: {e['message']}" for e in errors))

    # Lab work: each a job on one sandbox

    def create(self, doc):
        """Starts creating a sandbox from a sandboxInput: (job, sandbox)."""
        requires, want_ns, env = doc.get("requires", {}), bool(doc.get("namespace")), doc.get("environment")
        faults = bool(requires.get("faults"))
        if env is not None:
            # Its own environment: checked here, made by the job; requirements are the document's to meet
            if errors := self.cli.topology_errors(env):
                raise self.problem(400, "bad-input", "Bad input", "environment: " + "; ".join(errors))
            state = {"up": True, "layout": self.cli.scenario.layout(env), "topology": "the sandbox's environment",
                     "placeholders": self.cli.scenario.placeholders(env)}
            topo = env
        sets = [{"steps": doc.get(part, []), "faults": faults} for part in ("setup", "teardown")]
        self.steps_errors([s | ({"environment": env} if env is not None else {}) for s in sets])
        if env is None:
            state, topo = self.cli.lab_facts()
            if not state.get("up"):
                raise self.problem(409, "lab-down", "The lab is down",
                                   state.get("error") or "the lab is down (./opsforge up)")
        if missing := unmet(requires, want_ns, state, topo):
            raise self.problem(409, "unmet-requirements", "Unmet requirements",
                               "the running lab cannot host this sandbox: " + "; ".join(missing), unmet=missing)
        with self.lock:
            others = [sb for sb in self.items.values() if sb.status in ALIVE or sb.dirty]
            alone = next((sb for sb in others if sb.exclusive), None)
            if alone or (others and (faults or requires.get("cluster_changes") or env is not None)):
                who = alone or others[0]
                raise self.problem(409, "lab-busy", "The lab is busy",
                                   f"sandbox {who.id} ({who.label}) is {who.status}; a sandbox that changes the lab "
                                   "outside its namespace (faults, cluster_changes, its own environment) runs alone")
            sid = secrets.token_hex(4)
            while sid in self.items:
                sid = secrets.token_hex(4)
            t = now()
            ns = f"sb-{sid}" if want_ns else None
            sb = Sandbox(id=sid, kind=doc["kind"], label=doc["label"], requires=requires, namespace=ns,
                         setup_steps=doc.get("setup", []), teardown_steps=doc.get("teardown", []),
                         expires_after=doc.get("expires_after", DEFAULT_EXPIRY), status="creating", setup=None,
                         teardown=None, created=t, used=t, layout=state.get("layout"),
                         placeholders={**state.get("placeholders", {}), **({"${ns}": ns} if ns else {})}, dirty=False,
                         environment=env)
            self.workspaces.create(sid)
            try:
                for path, text in sorted(doc.get("files", {}).items()):
                    self.workspaces.write(sid, path, text.encode())
                self.items[sid] = sb
                self.save(sb)
                job = self.jobs.start("create", lambda lines: self._create(sb, lines), sandbox=sid)
            except BaseException:
                self.forget(sb)
                self.workspaces.delete(sid)
                raise
            return job, sb

    def _settle(self, sb, out, lines):
        """After a setup: ready, or failed and cleaned up at once."""
        sb.setup = out
        if out["error"] is None:
            sb.status = "ready"
        else:
            sb.teardown = self.cli.sandbox_down(sb.place(self.workdir(sb)), sb.teardown_steps,
                                                sb.requires.get("faults"), lines)
            sb.status, sb.dirty = "failed", False

    def _create(self, sb, lines):
        with self.lock:
            sb.dirty = True
            self.save(sb)
        try:
            out = self.cli.sandbox_up(sb.place(self.workdir(sb)), sb.setup_steps, lines)
            self._settle(sb, out, lines)
        except BaseException:
            sb.status = "failed"  # dirty: deleting it cleans up
            raise
        finally:
            with self.lock:
                sb.used = now()
                self.save(sb)
        return sb.doc(self.contract)

    def reset(self, sid):
        with self.lock:
            sb = self.get(sid)
            self.need(sb, "ready")
            job = self.jobs.start("reset", lambda lines: self._reset(sb, lines), sandbox=sid)
            sb.status = "resetting"
            self.save(sb)
            return job

    def _reset(self, sb, lines):
        try:
            place = sb.place(self.workdir(sb))
            sb.teardown = self.cli.sandbox_down(place, sb.teardown_steps, sb.requires.get("faults"), lines)
            sb.setup = None
            self._settle(sb, self.cli.sandbox_up(place, sb.setup_steps, lines), lines)
            if sb.status == "ready":
                sb.teardown = None
        except BaseException:
            sb.status = "failed"
            raise
        finally:
            with self.lock:
                sb.used = now()
                self.save(sb)
        return sb.doc(self.contract)

    def probe(self, sid, doc):
        return self._observe(sid, "probe", doc, lambda place, lines: self.cli.sandbox_probe(place, doc, lines))

    def action(self, sid, doc):
        return self._observe(sid, "action", doc,
                             lambda place, lines: self.cli.sandbox_action(place, doc["steps"], lines))

    def _observe(self, sid, kind, doc, work):
        check = {"steps": doc["steps"], "expect": doc.get("expect"), "faults": False}
        with self.lock:
            sb = self.get(sid)
        if kind == "action" and sb.environment is not None:
            # Its environment as it runs now (setup or earlier actions may have patched it): it runs alone
            check["environment"] = self.cli.load_valid_topology()
        self.steps_errors([check])
        with self.lock:
            sb = self.get(sid)
            self.need(sb, "ready")
            place = sb.place(self.workdir(sb))
            return self.jobs.start(kind, lambda lines: work(place, lines), sandbox=sid)

    def delete(self, sid, touch=True):
        with self.lock:
            sb = self.get(sid, touch=touch)
            if sb.status == "deleting" and touch:
                raise self.problem(409, "sandbox-state", "Sandbox being deleted", f"sandbox {sid} is being deleted")
            job = self.jobs.start("delete", lambda lines: self._delete(sb, lines), sandbox=sid)
            sb.status = "deleting"
            self.save(sb)
            self.hang_up(sb)
            return job

    def _delete(self, sb, lines):
        if sb.dirty:
            sb.teardown = self.cli.sandbox_down(sb.place(self.workdir(sb)), sb.teardown_steps,
                                                sb.requires.get("faults"), lines)
        with self.lock:
            if self.workdir(sb).is_dir():
                self.workspaces.delete(sb.id)
            sb.dirty = False
            self.forget(sb)
        return sb.doc(self.contract, status="deleted")

    # Terminals: deleting a sandbox hangs its shells up

    def opened(self, sb, pgid):
        with self.lock:
            sb.terminals.add(pgid)

    def closed(self, sb, pgid):
        with self.lock:
            sb.terminals.discard(pgid)

    def hang_up(self, sb):
        for pgid in list(sb.terminals):
            try:
                os.killpg(pgid, signal.SIGHUP)
            except OSError:
                pass

    # Expiry

    def reap(self):
        """Deletes every sandbox idle past its expiry (and those a stopped API left halfway); a busy lab waits for
        the next round. The deletions started, by sandbox id."""
        started = {}
        with self.lock:
            due = [sb for sb in self.items.values() if now() >= sb.used + sb.expires_after]
        for sb in due:
            try:
                started[sb.id] = self.delete(sb.id, touch=False)
            except self.cli.apiserver.ApiError:
                break  # lab busy: next round
        return started

    def reaper(self):
        def loop():
            while not self.stop.wait(REAP_EVERY):
                self.reap()

        threading.Thread(target=loop, daemon=True).start()
