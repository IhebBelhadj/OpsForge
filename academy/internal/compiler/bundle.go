// Package compiler compiles content packs (docs/courses/content-model.md) into the bundle that contentimport
// imports (api/bundle.schema.json): Phase 10's labs, curricula and badges plus courses of lectures, quizzes, labs and
// challenges. Packs are read through fs.FS; scenario steps are checked by the toolchain (StepChecker).
package compiler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/lab"
)

// Version of the bundle format (api/bundle.schema.json's `contract`).
const Version = 1

// Compile compiles packs into the bundle (api/bundle.schema.json), written as pyJSON. Invalid packs are left out
// and their problems listed in `errors`; a course may use labs of the other packs compiled with it (not of an
// invalid one). Scenario steps are checked by steps in one call; an error is only a failed call.
func Compile(ctx context.Context, sources []Source, steps StepChecker, compiledAt time.Time) ([]byte, error) {
	packs := make([]*loaded, len(sources))
	for i, src := range sources {
		packs[i] = load(src)
	}
	owners := map[string]map[string]string{"course": {}, "lab": {}} // id -> the first pack that has it
	allLabs, allCurricula, allBadges := map[string]any{}, map[string]bool{}, map[string]bool{}
	for _, pk := range packs {
		for kind, ids := range map[string][]string{"course": courseIDs(pk), "lab": pk.labIDs} {
			for _, id := range ids {
				if _, ok := owners[kind][id]; !ok {
					owners[kind][id] = packID(pk)
				}
			}
		}
		for _, lid := range pk.labIDs {
			allLabs[lid] = pk.doc["labs"].(map[string]any)[lid]
		}
		for _, cid := range pk.curriculumIDs {
			allCurricula[cid] = true
		}
		badges, _ := pk.doc["badges"].([]any)
		for _, b := range badges {
			if m, ok := b.(map[string]any); ok {
				if id, ok := m["id"].(string); ok {
					allBadges[id] = true
				}
			}
		}
	}
	courseOwners := map[string]bool{}
	for id := range owners["course"] {
		courseOwners[id] = true
	}

	// Pass 1: each pack on its own (and against the others' ids); scenario steps are queued
	var sets []lab.StepSet
	probs := make([]*problems, len(packs))
	for i, pk := range packs {
		pid, p := packID(pk), &problems{sets: &sets}
		probs[i] = p
		p.addAll("", pk.errs)
		for _, kind := range []string{"course", "lab"} {
			ids := pk.labIDs
			if kind == "course" {
				ids = courseIDs(pk)
			}
			for _, id := range ids {
				if owners[kind][id] != pid {
					p.add("%s %s is also in pack %s", kind, pyRepr(id), owners[kind][id])
				}
			}
		}
		if p.found() {
			continue
		}
		otherLabs, otherCurricula := map[string]bool{}, map[string]bool{}
		for id := range allLabs {
			otherLabs[id] = true
		}
		for id := range allCurricula {
			otherCurricula[id] = true
		}
		for _, id := range pk.labIDs {
			delete(otherLabs, id)
		}
		for _, id := range pk.curriculumIDs {
			delete(otherCurricula, id)
		}
		packErrors(pk, otherLabs, otherCurricula, p)
		if p.found() {
			continue
		}
		for _, lid := range pk.labIDs {
			for _, f := range stringList(pk.doc["labs"].(map[string]any)[lid].(map[string]any)["files"]) {
				if _, ok := pk.files[lid][f]; !ok {
					p.add("labs/%s: files: %s is not in labs/%s/files/", lid, f, lid)
				}
			}
		}
		for _, c := range pk.courses {
			if errs := schemaErrors(courseSchema(), c.doc); len(errs) > 0 {
				p.addAll("courses/"+c.id+": ", errs)
			} else {
				courseErrors(pk, c, allLabs, courseOwners, allBadges, p)
			}
		}
	}
	stepErrs := map[int][]string{}
	if len(sets) > 0 {
		errs, err := steps.CheckSteps(ctx, sets)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrSteps, err)
		}
		for _, e := range errs {
			stepErrs[e.Set] = append(stepErrs[e.Set], e.Message)
		}
	}
	messages := make([][]string, len(packs))
	labItems := map[string]map[string]any{}
	for i, pk := range packs {
		messages[i] = probs[i].messages(stepErrs)
		if len(messages[i]) > 0 {
			continue
		}
		for _, lid := range pk.labIDs {
			entry, err := labEntry(lid, pk.doc["labs"].(map[string]any)[lid].(map[string]any), pk.files[lid])
			if err != nil {
				return nil, err
			}
			labItems[lid] = entry
		}
	}

	// Pass 2: a course using a lab of an invalid pack fails too; the rest is assembled
	bundlePacks, bundleErrors := []any{}, []any{}
	for i, pk := range packs {
		pid, errs := packID(pk), messages[i]
		if len(errs) == 0 {
			for _, c := range pk.courses {
				for _, s := range c.doc["course"].(map[string]any)["sections"].([]any) {
					for _, it := range s.(map[string]any)["items"].([]any) {
						if kind, ref := item(it); kind == "lab" && labItems[ref] == nil {
							errs = append(errs, fmt.Sprintf("courses/%s: lab %s is in pack %s, which has errors", c.id,
								pyRepr(ref), owners["lab"][ref]))
						}
					}
				}
			}
		}
		if len(errs) > 0 {
			for _, m := range errs {
				bundleErrors = append(bundleErrors, map[string]any{"pack": pid, "message": m})
			}
			continue
		}
		entry, err := packEntry(pk, labItems)
		if err != nil {
			return nil, err
		}
		bundlePacks = append(bundlePacks, entry)
	}
	return pyJSON(map[string]any{
		"contract": Version, "compiled_at": compiledAt.UTC().Format("2006-01-02T15:04:05Z"), "packs": bundlePacks,
		"errors": bundleErrors,
	})
}

func courseIDs(pk *loaded) []string {
	ids := make([]string, len(pk.courses))
	for i, c := range pk.courses {
		ids[i] = c.id
	}
	return ids
}

// packID is pack.yml's id, else the folder's name.
func packID(pk *loaded) string {
	if meta, ok := pk.doc["pack"].(map[string]any); ok {
		if id, ok := meta["id"]; ok {
			if s, ok := id.(string); ok {
				return s
			}
			return pyRepr(id)
		}
	}
	return path.Base(pk.src.Path)
}

// get is Python's dict.get: the value when the key is there (even null), else def.
func get(m map[string]any, key string, def any) any {
	if v, ok := m[key]; ok {
		return v
	}
	return def
}

// or is Python's `value or default`.
func or(v, def any) any {
	if truthy(v) {
		return v
	}
	return def
}

func labEntry(lid string, lb map[string]any, files map[string]any) (map[string]any, error) {
	// Labs without files hash as in Phase 10, so the passes recorded there are not flagged as stale
	var hashed any = lb
	if len(files) > 0 {
		hashed = map[string]any{"body": lb, "files": files}
	}
	hash, err := contentHash(hashed)
	if err != nil {
		return nil, fmt.Errorf("labs/%s: %w", lid, err)
	}
	return map[string]any{
		"id": lid, "title": lb["title"], "summary": lb["summary"], "minutes": get(lb, "minutes", 0),
		"xp": get(lb, "xp", defaultXP), "layout": get(lb, "layout", nil), "needs": get(lb, "needs", []any{}),
		"requires": get(lb, "requires", []any{}), "body": lb, "files": files, "content_hash": hash,
	}, nil
}

// durationS is "90s", "20m", "1h" in seconds; nil stays nil.
func durationS(v any) any {
	s, ok := v.(string)
	if !ok || s == "" {
		return nil
	}
	n, _ := strconv.Atoi(s[:len(s)-1])
	return n * map[byte]int{'s': 1, 'm': 60, 'h': 3600}[s[len(s)-1]]
}

// total adds minutes as Python's sum does: integers stay integers.
func total(vals []any) any {
	var ints int64
	var floats float64
	isFloat := false
	for _, v := range vals {
		switch x := v.(type) {
		case int64:
			ints += x
		case int:
			ints += int64(x)
		case float64:
			floats += x
			isFloat = true
		}
	}
	if isFloat {
		return float64(ints) + floats
	}
	return ints
}

func withHash(m map[string]any, what string) (map[string]any, error) {
	hash, err := contentHash(m)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	m["content_hash"] = hash
	return m, nil
}

// courseEntries are a course's entry and its lectures', quizzes' and challenges'.
func courseEntries(c *course, position int, labItems map[string]map[string]any) (map[string]any, []any, []any,
	[]any, error) {
	var lectures, quizzes, challenges []any
	byKind := map[string]map[string]map[string]any{"lecture": {}, "quiz": {}, "challenge": {}}
	for _, lid := range c.lectures {
		lec := c.doc["lectures"].(map[string]any)[lid].(map[string]any)
		hash, err := contentHash(lec)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		e := map[string]any{"course": c.id, "id": lid, "title": lec["title"], "minutes": lec["minutes"],
			"markdown": lec["markdown"], "resources": get(lec, "resources", []any{}), "content_hash": hash}
		lectures = append(lectures, e)
		byKind["lecture"][lid] = e
	}
	for _, qid := range c.quizzes {
		q := c.doc["quizzes"].(map[string]any)[qid].(map[string]any)
		hash, err := contentHash(q)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		var questions []any
		for _, x := range q["questions"].([]any) {
			qq := x.(map[string]any)
			answer := qq["answer"]
			if _, isList := answer.([]any); !isList {
				answer = []any{answer}
			}
			questions = append(questions, map[string]any{"id": qq["id"], "type": get(qq, "type", "single"),
				"prompt": qq["prompt"], "choices": qq["choices"], "answer": answer, "explain": qq["explain"],
				"see": get(qq, "see", nil)})
		}
		e := map[string]any{"course": c.id, "id": qid, "title": q["title"], "minutes": q["minutes"],
			"pass": get(q, "pass", quizPass), "xp": get(q, "xp", quizXP), "shuffle": get(q, "shuffle", true),
			"time_limit_s": durationS(get(q, "time_limit", nil)), "draw": get(q, "draw", nil), "questions": questions,
			"content_hash": hash}
		quizzes = append(quizzes, e)
		byKind["quiz"][qid] = e
	}
	for _, chid := range c.challenges {
		ch := c.doc["challenges"].(map[string]any)[chid].(map[string]any)
		hash, err := contentHash(ch)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		var tests []any
		for _, t := range ch["tests"].([]any) {
			tt := t.(map[string]any)
			tests = append(tests, map[string]any{"name": tt["name"], "action": or(tt["action"], []any{}),
				"probe": tt["probe"]})
		}
		e := map[string]any{"course": c.id, "id": chid, "title": ch["title"], "difficulty": ch["difficulty"],
			"minutes": ch["minutes"], "xp": get(ch, "xp", challengeXP), "needs": get(ch, "needs", []any{}),
			"statement": ch["statement"], "files": ch["files"], "starter": ch["starter"], "solution": ch["solution"],
			"time_limit_s": or(durationS(get(ch, "time_limit", nil)), challengeTimeLimit), "tests": tests,
			"hints": get(ch, "hints", []any{}), "content_hash": hash}
		challenges = append(challenges, e)
		byKind["challenge"][chid] = e
	}

	doc := c.doc["course"].(map[string]any)
	var sections, sectionMinutes []any
	for _, s := range doc["sections"].([]any) {
		sec := s.(map[string]any)
		var items, minutes []any
		for _, it := range sec["items"].([]any) {
			kind, ref := item(it)
			target := labItems[ref]
			if kind != "lab" {
				target = byKind[kind][ref]
			}
			items = append(items, map[string]any{"id": ref, "type": kind, "ref": ref, "title": target["title"],
				"minutes": target["minutes"], "content_hash": target["content_hash"]})
			minutes = append(minutes, target["minutes"])
		}
		m := total(minutes)
		sectionMinutes = append(sectionMinutes, m)
		sections = append(sections, map[string]any{"title": sec["title"], "layout": get(sec, "layout", nil),
			"needs": get(sec, "needs", []any{}), "minutes": m, "items": items})
	}
	entry, err := withHash(map[string]any{
		"id": c.id, "title": doc["title"], "subtitle": get(doc, "subtitle", ""), "level": doc["level"],
		"position": get(doc, "position", position), "requires": get(doc, "requires", []any{}),
		"layout": get(doc, "layout", nil), "needs": get(doc, "needs", []any{}),
		"outcomes": get(doc, "outcomes", []any{}), "requirements": get(doc, "requirements", []any{}),
		"description": get(doc, "description", ""), "badge": get(doc, "badge", nil),
		"minutes": total(sectionMinutes), "sections": sections,
	}, "courses/"+c.id)
	return entry, lectures, quizzes, challenges, err
}

// packEntry is the bundle's entry for a valid pack.
func packEntry(pk *loaded, labItems map[string]map[string]any) (map[string]any, error) {
	entry := map[string]any{"courses": []any{}, "lectures": []any{}, "quizzes": []any{}, "challenges": []any{}}
	for i, c := range pk.courses {
		course, lectures, quizzes, challenges, err := courseEntries(c, i, labItems)
		if err != nil {
			return nil, err
		}
		entry["courses"] = append(entry["courses"].([]any), course)
		entry["lectures"] = append(entry["lectures"].([]any), lectures...)
		entry["quizzes"] = append(entry["quizzes"].([]any), quizzes...)
		entry["challenges"] = append(entry["challenges"].([]any), challenges...)
	}
	labs := []any{}
	for _, lid := range pk.labIDs {
		labs = append(labs, labItems[lid])
	}
	curricula := []any{}
	for i, cid := range pk.curriculumIDs {
		cur := pk.doc["curricula"].(map[string]any)[cid].(map[string]any)
		curricula = append(curricula, map[string]any{"id": cid, "title": cur["title"],
			"summary": get(cur, "summary", ""), "position": get(cur, "position", i), "levels": cur["levels"]})
	}
	badges := []any{}
	for i, b := range pk.doc["badges"].([]any) {
		badge := b.(map[string]any)
		badges = append(badges, map[string]any{"id": badge["id"], "title": badge["title"],
			"description": badge["description"], "icon": get(badge, "icon", nil), "position": i, "rule": badge["rule"]})
	}
	assets := []any{}
	err := fs.WalkDir(pk.src.FS, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil || !isFile(pk.src.FS, p, d) {
			return nil //nolint:nilerr // no assets/ folder: no assets
		}
		data, err := fs.ReadFile(pk.src.FS, p)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		sum := sha256.Sum256(data)
		assets = append(assets, map[string]any{"path": p[len("assets/"):], "sha256": hex.EncodeToString(sum[:]),
			"bytes": len(data)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	entry["labs"], entry["curricula"], entry["badges"], entry["assets"] = labs, curricula, badges, assets

	meta := pk.doc["pack"].(map[string]any)
	version, ok := meta["version"].(string)
	if !ok {
		version = pyRepr(meta["version"])
	}
	head := map[string]any{"id": packID(pk), "title": meta["title"], "version": version,
		"author": get(meta, "author", ""), "description": get(meta, "description", ""),
		"hint_penalty": get(meta, "hint_penalty", defaultHintPenalty)}
	whole := map[string]any{}
	for _, m := range []map[string]any{head, entry} {
		for k, v := range m {
			whole[k] = v
		}
	}
	hash, err := contentHash(whole)
	if err != nil {
		return nil, fmt.Errorf("pack %s: %w", packID(pk), err)
	}
	whole["content_hash"] = hash
	whole["path"] = pk.src.Path
	return whole, nil
}
