"""OpsForge Academy content toolchain (docs/PLAN.md Phase 11, docs/courses/content-model.md): compiles packs into
the bundle that the Go platform imports (academy/api/bundle.schema.json, contract 1). Pure functions over the pack
folders; the `opsforge api` commands print the results as JSON.

A pack is Phase 10's folder (pack.yml, labs/, badges.yml, curricula/ until M2) plus `courses/<id>/`: course.yml
(sections of items), lectures/<id>.md (YAML front matter + Markdown), quizzes/<id>.yml and
challenges/<id>/{challenge.yml,starter/,solution/}. Texts keep their placeholders (`${node:c}`, `${ns}`) raw: they
depend on the running topology, not on the pack.
"""

import hashlib
import json
import re
from pathlib import Path

import jsonschema
import yaml

import labs
import scenario

# The toolchain contract `opsforge api` speaks (contract/README.md): the major in every document, the minor (additive
# revisions) in `api version`
CONTRACT = 1
CONTRACT_MINOR = 0
COURSE_SCHEMA = Path(__file__).resolve().parent / "course.schema.json"
# Inputs and outputs of `run`, `probe` and `heal` (OpsForge publishes it; callers keep a copy)
TOOLCHAIN_SCHEMA = Path(__file__).resolve().parents[1] / "contract" / "toolchain.schema.json"
ITEM_TYPES = ("lecture", "quiz", "lab", "challenge")
QUIZ_PASS = 70
QUIZ_XP = 20
CHALLENGE_XP = 100
CHALLENGE_TIME_LIMIT = 120
_IMAGE = re.compile(r"!\[[^\]]*\]\(\s*<?([^)\s>]+)")
_UNITS = {"s": 1, "m": 60, "h": 3600}


def input_errors(kind, doc):
    """Why `doc` is not a valid `kind` document (toolchain.schema.json's $defs: runInput, probeInput)."""
    schema = {**json.loads(TOOLCHAIN_SCHEMA.read_text()), "$ref": f"#/$defs/{kind}"}
    return [f"{'/'.join(map(str, e.absolute_path)) or 'document'}: {e.message}"
            for e in jsonschema.Draft202012Validator(schema).iter_errors(doc)]


def with_namespace(obj, namespace):
    """`obj` (steps, a text) with `${ns}` replaced by the attempt's namespace; ValueError if it has none."""
    if isinstance(obj, dict):
        return {k: with_namespace(v, namespace) for k, v in obj.items()}
    if isinstance(obj, list):
        return [with_namespace(v, namespace) for v in obj]
    if isinstance(obj, str) and "${ns}" in obj:
        if namespace is None:
            raise ValueError(f"{obj!r} uses ${{ns}}, but the lab has no namespace (namespace: auto)")
        return obj.replace("${ns}", namespace)
    return obj


def content_hash(obj):
    return labs.content_hash(obj)


def duration_s(value):
    """'90s', '20m', '1h' -> seconds; None stays None."""
    return None if value is None else int(value[:-1]) * _UNITS[value[-1]]


def read_files(folder):
    """{relative path: text} of every file under `folder` (empty if it does not exist), in path order."""
    folder = Path(folder)
    return {p.relative_to(folder).as_posix(): p.read_text() for p in sorted(folder.rglob("*")) if p.is_file()}


class _Reader:
    """Reads one pack's files, collecting problems (as `<path in the pack>: message`) instead of raising."""

    def __init__(self, pack):
        self.pack = Path(pack)
        self.errors = []

    def error(self, path, message):
        self.errors.append(f"{Path(path).relative_to(self.pack).as_posix()}: {message}")

    def yaml(self, path, default=None):
        try:
            doc = yaml.safe_load(Path(path).read_text())
        except (OSError, yaml.YAMLError) as e:
            self.error(path, f"cannot read: {e}".replace("\n", " "))
            return default
        return default if doc is None else doc

    def lecture(self, path):
        text = path.read_text()
        match = re.match(r"---\n(.*?)\n---\n?(.*)\Z", text, re.S)
        if not match:
            self.error(path, "needs YAML front matter (--- title, minutes ---) before the Markdown")
            return None
        try:
            meta = yaml.safe_load(match[1]) or {}
        except yaml.YAMLError as e:
            self.error(path, f"front matter: {e}".replace("\n", " "))
            return None
        if not isinstance(meta, dict):
            self.error(path, "front matter must be a mapping")
            return None
        return {**meta, "markdown": match[2].strip("\n") + "\n"}


def load_course(reader, folder):
    """A course folder -> the document lab/course.schema.json describes (None for unreadable files)."""
    return {
        "course": reader.yaml(folder / "course.yml", {}),
        "lectures": {p.stem: reader.lecture(p) for p in sorted((folder / "lectures").glob("*.md"))},
        "quizzes": {p.stem: reader.yaml(p, {}) for p in sorted((folder / "quizzes").glob("*.yml"))},
        "challenges": {
            p.parent.name: {**(reader.yaml(p, {}) or {}), "starter": read_files(p.parent / "starter"),
                            "solution": read_files(p.parent / "solution")}
            for p in sorted((folder / "challenges").glob("*/challenge.yml"))
        },
    }


def load(pack):
    """A pack folder -> (Phase 10 document, {course id: course document}, lab files, problems while reading)."""
    pack = Path(pack)
    reader = _Reader(pack)
    doc = {
        "pack": reader.yaml(pack / "pack.yml", {}),
        "curricula": {p.stem: reader.yaml(p, {}) for p in sorted((pack / "curricula").glob("*.yml"))},
        "labs": {p.stem: reader.yaml(p, {}) for p in sorted((pack / "labs").glob("*.yml"))},
        "badges": reader.yaml(pack / "badges.yml", []) if (pack / "badges.yml").exists() else [],
    }
    courses = {p.parent.name: load_course(reader, p.parent) for p in sorted((pack / "courses").glob("*/course.yml"))}
    files = {lid: read_files(pack / "labs" / lid / "files") for lid in doc["labs"]}
    return doc, courses, files, reader


def _schema_errors(validator, doc):
    return [f"{'/'.join(map(str, e.absolute_path)) or '<root>'}: {e.message}" for e in validator.iter_errors(doc)]


def _quiz_errors(qid, quiz, lectures):
    errs = []
    ids = [q["id"] for q in quiz["questions"]]
    errs += [f"duplicate question {i!r}" for i in sorted(set(ids)) if ids.count(i) > 1]
    for q in quiz["questions"]:
        answer, last = q["answer"], len(q["choices"]) - 1
        if q.get("type", "single") == "single" and not isinstance(answer, int):
            errs.append(f"{q['id']}: a single-answer question takes one index")
            continue
        if q.get("type") == "multiple" and not isinstance(answer, list):
            errs.append(f"{q['id']}: a multiple-answer question takes a list of indexes")
            continue
        errs += [f"{q['id']}: answer {a} is not a choice (0-{last})"
                 for a in (answer if isinstance(answer, list) else [answer]) if a > last]
        if "see" in q and q["see"] not in lectures:
            errs.append(f"{q['id']}: see: unknown lecture {q['see']!r} in this course")
    if quiz.get("draw", 0) > len(quiz["questions"]):
        errs.append(f"draw {quiz['draw']} is more than the {len(quiz['questions'])} questions")
    return errs


def _challenge_errors(challenge):
    errs = [f"files: {f} is not in starter/" for f in challenge["files"] if f not in challenge["starter"]]
    for test in challenge["tests"]:
        errs += [f"{test['name']}: probe: {e}" for e in labs.CHECKS["probe"](test["probe"])]
        for step in test.get("action") or []:
            if scenario.step_type(step) in labs.FAULT_STEPS:
                errs.append(f"{test['name']}: action: {scenario.step_type(step)}: grader actions may change only "
                            "the challenge namespace")
        errs += [f"{test['name']}: action: {e}"
                 for e in scenario.errors({"description": "-", "setup": test.get("action") or []})]
    return errs


def _image_errors(pack, lecture_path, markdown):
    errs = []
    assets = (pack / "assets").resolve()
    for target in _IMAGE.findall(markdown):
        resolved = (lecture_path.parent / target).resolve()
        if re.match(r"[a-z]+:", target) or assets not in resolved.parents:
            errs.append(f"image {target} is outside assets/ (lecture images are pack assets)")
        elif not resolved.is_file():
            errs.append(f"image {target}: {resolved.relative_to(assets)} is not a file in assets/")
    return errs


def _course_errors(pack, cid, cdoc, labs_by_id, course_ids, badge_ids):
    """Cross-references of a schema-valid course: items exist and are unique, needs cover the items', files."""
    course, folder, errs = cdoc["course"], f"courses/{cid}", []
    errs += [f"{folder}: requires unknown course {r!r}" for r in course.get("requires", []) if r not in course_ids]
    if "badge" in course and course["badge"] not in badge_ids:
        errs.append(f"{folder}: unknown badge {course['badge']!r}")
    seen = []
    for item in (i for s in course["sections"] for i in s["items"]):
        kind, ref = next(iter(item.items()))
        if ref in seen:
            errs.append(f"{folder}: duplicate item {ref!r} (item ids are unique within a course)")
        seen.append(ref)
        pool = labs_by_id if kind == "lab" else cdoc[f"{kind}s" if kind != "quiz" else "quizzes"]
        if ref not in pool:
            errs.append(f"{folder}: unknown {kind} {ref!r}")
            continue
        needs = pool[ref].get("needs", []) if kind in ("lab", "challenge") else []
        errs += [f"{folder}: {ref} needs {n}, which the course's needs do not list"
                 for n in needs if n not in course.get("needs", [])]
    for qid, quiz in cdoc["quizzes"].items():
        errs += [f"{folder}/quizzes/{qid}.yml: {e}" for e in _quiz_errors(qid, quiz, cdoc["lectures"])]
    for chid, challenge in cdoc["challenges"].items():
        errs += [f"{folder}/challenges/{chid}: {e}" for e in _challenge_errors(challenge)]
    for lid, lecture in cdoc["lectures"].items():
        path = pack / folder / "lectures" / f"{lid}.md"
        errs += [f"{folder}/lectures/{lid}.md: {e}" for e in _image_errors(pack, path, lecture["markdown"])]
    return errs


def _lab_entry(lid, lab, files):
    # Labs without files hash as in Phase 10 (lab/labs.py), so existing passes are not flagged as stale
    return {
        "id": lid, "title": lab["title"], "summary": lab["summary"], "minutes": lab.get("minutes", 0),
        "xp": lab.get("xp", labs.DEFAULT_XP), "layout": lab.get("layout"), "needs": lab.get("needs", []),
        "requires": lab.get("requires", []), "body": lab, "files": files,
        "content_hash": content_hash({"body": lab, "files": files}) if files else labs.content_hash(lab),
    }


def _course_entries(cid, position, cdoc, lab_items):
    lectures = [{"course": cid, "id": lid, "title": lec["title"], "minutes": lec["minutes"],
                 "markdown": lec["markdown"], "resources": lec.get("resources", []),
                 "content_hash": content_hash(lec)} for lid, lec in cdoc["lectures"].items()]
    quizzes = [{"course": cid, "id": qid, "title": q["title"], "minutes": q["minutes"], "pass": q.get("pass", QUIZ_PASS),
                "xp": q.get("xp", QUIZ_XP), "shuffle": q.get("shuffle", True),
                "time_limit_s": duration_s(q.get("time_limit")), "draw": q.get("draw"),
                "questions": [{"id": x["id"], "type": x.get("type", "single"), "prompt": x["prompt"],
                               "choices": x["choices"], "answer": x["answer"] if isinstance(x["answer"], list)
                               else [x["answer"]], "explain": x["explain"], "see": x.get("see")}
                              for x in q["questions"]],
                "content_hash": content_hash(q)} for qid, q in cdoc["quizzes"].items()]
    challenges = [{"course": cid, "id": chid, "title": c["title"], "difficulty": c["difficulty"],
                   "minutes": c["minutes"], "xp": c.get("xp", CHALLENGE_XP), "needs": c.get("needs", []),
                   "statement": c["statement"], "files": c["files"], "starter": c["starter"],
                   "solution": c["solution"], "time_limit_s": duration_s(c.get("time_limit")) or CHALLENGE_TIME_LIMIT,
                   "tests": [{"name": t["name"], "action": t.get("action") or [], "probe": t["probe"]}
                             for t in c["tests"]],
                   "hints": c.get("hints", []), "content_hash": content_hash(c)}
                  for chid, c in cdoc["challenges"].items()]
    by_kind = {"lecture": {e["id"]: e for e in lectures}, "quiz": {e["id"]: e for e in quizzes},
               "challenge": {e["id"]: e for e in challenges}}

    def item(entry):
        kind, ref = next(iter(entry.items()))
        target = lab_items[ref] if kind == "lab" else by_kind[kind][ref]
        return {"id": ref, "type": kind, "ref": ref, "title": target["title"], "minutes": target["minutes"],
                "content_hash": target["content_hash"]}

    c = cdoc["course"]
    sections = []
    for s in c["sections"]:
        items = [item(i) for i in s["items"]]
        sections.append({"title": s["title"], "layout": s.get("layout"), "needs": s.get("needs", []),
                         "minutes": sum(i["minutes"] for i in items), "items": items})
    course = {
        "id": cid, "title": c["title"], "subtitle": c.get("subtitle", ""), "level": c["level"],
        "position": c.get("position", position), "requires": c.get("requires", []), "layout": c.get("layout"),
        "needs": c.get("needs", []), "outcomes": c.get("outcomes", []), "requirements": c.get("requirements", []),
        "description": c.get("description", ""), "badge": c.get("badge"),
        "minutes": sum(s["minutes"] for s in sections), "sections": sections,
    }
    course["content_hash"] = content_hash(course)
    return course, lectures, quizzes, challenges


def _pack_id(path, doc):
    return str(doc["pack"].get("id", path.name)) if isinstance(doc["pack"], dict) else path.name


def _pack_entry(path, root, doc, courses, lab_items):
    """The bundle's entry for a valid pack."""
    entry = {"courses": [], "lectures": [], "quizzes": [], "challenges": []}
    for position, (cid, cdoc) in enumerate(courses.items()):
        course, lectures, quizzes, challenges = _course_entries(cid, position, cdoc, lab_items)
        entry["courses"].append(course)
        entry["lectures"] += lectures
        entry["quizzes"] += quizzes
        entry["challenges"] += challenges
    assets = path / "assets"
    entry.update({
        "labs": [lab_items[lid] for lid in doc["labs"]],
        "curricula": [{"id": k, "title": c["title"], "summary": c.get("summary", ""), "position": c.get("position", i),
                       "levels": c["levels"]} for i, (k, c) in enumerate(doc["curricula"].items())],
        "badges": [{"id": b["id"], "title": b["title"], "description": b["description"], "icon": b.get("icon"),
                    "position": i, "rule": b["rule"]} for i, b in enumerate(doc["badges"])],
        "assets": [{"path": f.relative_to(assets).as_posix(), "sha256": hashlib.sha256(f.read_bytes()).hexdigest(),
                    "bytes": f.stat().st_size} for f in sorted(assets.rglob("*")) if f.is_file()],
    })
    meta = doc["pack"]
    head = {"id": _pack_id(path, doc), "title": meta["title"], "version": str(meta["version"]),
            "author": meta.get("author", ""), "description": meta.get("description", ""),
            "hint_penalty": meta.get("hint_penalty", labs.DEFAULT_HINT_PENALTY)}
    head["content_hash"] = content_hash({**head, **entry})
    resolved = path.resolve()
    head["path"] = resolved.relative_to(root).as_posix() if root in resolved.parents else str(resolved)
    return {**head, **entry}


def compile_bundle(paths, root, compiled_at):
    """Pack folders -> the bundle (contract 1). Invalid packs are left out and their problems listed in `errors`;
    courses may use labs of the other packs compiled together (not of an invalid one)."""
    root = Path(root).resolve()
    validator = jsonschema.Draft202012Validator(json.loads(COURSE_SCHEMA.read_text()))
    loaded = [(Path(p), *load(p)) for p in paths]
    owners = {"course": {}, "lab": {}}  # id -> the first pack that has it
    for path, doc, courses, _, _ in loaded:
        for kind, ids in (("course", courses), ("lab", doc["labs"])):
            for i in ids:
                owners[kind].setdefault(i, _pack_id(path, doc))
    all_labs = {lid: lab for _, doc, _, _, _ in loaded for lid, lab in doc["labs"].items()}
    all_curricula = {c for _, doc, _, _, _ in loaded for c in doc["curricula"]}
    all_badges = {b.get("id") for _, doc, _, _, _ in loaded if isinstance(doc["badges"], list)
                  for b in doc["badges"] if isinstance(b, dict)}

    # Pass 1: each pack on its own (and against the others' ids)
    problems, lab_items = {}, {}
    for path, doc, courses, files, reader in loaded:
        pid, errs = _pack_id(path, doc), list(reader.errors)
        for kind, ids in (("course", courses), ("lab", doc["labs"])):
            errs += [f"{kind} {i!r} is also in pack {owners[kind][i]}" for i in ids if owners[kind][i] != pid]
        if not errs:
            others = {"labs": set(all_labs) - set(doc["labs"]), "curricula": all_curricula - set(doc["curricula"])}
            errs += labs.errors(doc, others)
        if not errs:
            for lid, lab in doc["labs"].items():
                errs += [f"labs/{lid}: files: {f} is not in labs/{lid}/files/"
                         for f in lab.get("files", []) if f not in files[lid]]
            for cid, cdoc in courses.items():
                if schema_errs := _schema_errors(validator, cdoc):
                    errs += [f"courses/{cid}: {e}" for e in schema_errs]
                else:
                    errs += _course_errors(path, cid, cdoc, all_labs, set(owners["course"]), all_badges)
        problems[pid] = errs
        if not errs:
            lab_items.update({lid: _lab_entry(lid, lab, files[lid]) for lid, lab in doc["labs"].items()})

    # Pass 2: a course using a lab of an invalid pack fails too; the rest is assembled
    packs, errors = [], []
    for path, doc, courses, _, _ in loaded:
        pid = _pack_id(path, doc)
        errs = problems[pid] or [
            f"courses/{cid}: lab {ref!r} is in pack {owners['lab'][ref]}, which has errors"
            for cid, cdoc in courses.items() for s in cdoc["course"]["sections"] for i in s["items"]
            for kind, ref in i.items() if kind == "lab" and ref not in lab_items]
        if errs:
            errors += [{"pack": pid, "message": e} for e in errs]
        else:
            packs.append(_pack_entry(path, root, doc, courses, lab_items))
    return {"contract": CONTRACT, "compiled_at": compiled_at, "packs": packs, "errors": errors}
