package compiler

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/IhebBelhadj/opsforge/academy/api"
	"github.com/IhebBelhadj/opsforge/academy/internal/lab"
)

// problems collects a pack's errors. Scenario steps are OpsForge's to check: they are queued as step sets and
// their errors, prefixed with where the steps are, take the place of the set once the toolchain has answered.
type problems struct {
	items []problem
	sets  *[]lab.StepSet // shared by every pack of a compile: one toolchain call
}

type problem struct {
	msg string
	set int // -1: msg is the error; else msg prefixes the errors of that step set
}

func (p *problems) add(format string, a ...any) {
	p.items = append(p.items, problem{fmt.Sprintf(format, a...), -1})
}

func (p *problems) addAll(prefix string, msgs []string) {
	for _, m := range msgs {
		p.items = append(p.items, problem{prefix + m, -1})
	}
}

// steps queues steps for the toolchain to check; a set that cannot fail (no steps, no expectations) is not sent.
func (p *problems) steps(prefix string, set lab.StepSet) {
	if len(set.Steps) == 0 && len(set.Expect) == 0 {
		return
	}
	*p.sets = append(*p.sets, set)
	p.items = append(p.items, problem{prefix, len(*p.sets) - 1})
}

// found reports an error found here (queued steps are not errors yet).
func (p *problems) found() bool {
	return slices.ContainsFunc(p.items, func(x problem) bool { return x.set < 0 })
}

// messages are the errors, with each queued set replaced by its errors (stepErrs: by set index).
func (p *problems) messages(stepErrs map[int][]string) []string {
	var out []string
	for _, x := range p.items {
		if x.set < 0 {
			out = append(out, x.msg)
			continue
		}
		for _, m := range stepErrs[x.set] {
			out = append(out, x.msg+m)
		}
	}
	return out
}

// Defaults of the pack formats (lab/labs.py, docs/courses/content-model.md).
const (
	defaultHintPenalty = 0.1
	defaultXP          = 100
	quizPass           = 70
	quizXP             = 20
	challengeXP        = 100
	challengeTimeLimit = 120
)

// checks are a lab step's check types; each returns its argument's errors and queues its steps.
var checks = []string{"probe", "quiz", "confirm"}

func checkErrors(kind string, arg any, prefix string, p *problems) {
	switch kind {
	case "probe":
		probeErrors(arg, prefix, p)
	case "quiz":
		m, ok := arg.(map[string]any)
		if !ok {
			p.add("%squiz takes {question, choices, answer}", prefix)
			return
		}
		for _, k := range sortedKeys(m) {
			if !slices.Contains([]string{"question", "choices", "answer", "explain"}, k) {
				p.add("%sunknown key %s", prefix, pyRepr(k))
			}
		}
		choices, ok := m["choices"].([]any)
		if !ok || len(choices) < 2 {
			p.add("%squiz needs at least two choices", prefix)
			return
		}
		if a, ok := m["answer"].(int64); !ok || a < 0 || a >= int64(len(choices)) {
			p.add("%squiz answer must be a choice index, 0-%d", prefix, len(choices)-1)
		}
	case "confirm":
		if s, ok := arg.(string); !ok || s == "" {
			p.add("%sconfirm takes the button's label, e.g. 'I read the routes'", prefix)
		}
	}
}

// probeErrors checks a probe ({steps, expect}): checks only observe the lab, so fault steps are refused.
func probeErrors(arg any, prefix string, p *problems) {
	m, ok := arg.(map[string]any)
	if _, has := m["steps"]; !ok || !has {
		p.add("%sprobe takes {steps: [...], expect: {...}}", prefix)
		return
	}
	for _, k := range sortedKeys(m) {
		if k != "steps" && k != "expect" {
			p.add("%sunknown key %s", prefix, pyRepr(k))
		}
	}
	steps, ok := stepList(m["steps"])
	if !ok {
		p.add("%sprobe steps must be a list", prefix)
		return
	}
	expect, ok := m["expect"].(map[string]any)
	if !truthy(m["expect"]) {
		p.add("%sprobe needs expect (what passing means)", prefix)
		expect = nil
	} else if !ok {
		p.add("%sprobe expect maps metrics to conditions", prefix)
		return
	}
	p.steps(prefix, lab.StepSet{Steps: steps, Expect: expect, NoFaults: true})
}

// stepList is a list of steps (none: an empty list).
func stepList(v any) ([]any, bool) {
	if v == nil {
		return nil, true
	}
	l, ok := v.([]any)
	return l, ok
}

// Badge rule types (rewards.Rules decodes them; keep both): the keys each takes and its checks.
type ruleType struct {
	keys  []string
	valid func(rule map[string]any, labs, curricula map[string]bool) []string
}

var ruleTypes = map[string]ruleType{
	"labs_passed": {[]string{"min", "curriculum", "max_hints", "max_seconds"}, func(r map[string]any, _, curricula map[string]bool) []string {
		return slices.Concat(needs(r, "min"), ints(r, "min", "max_hints", "max_seconds"), known(r, "curriculum", curricula))
	}},
	"lab_passed": {[]string{"lab", "max_hints", "max_seconds"}, func(r map[string]any, labs, _ map[string]bool) []string {
		return slices.Concat(needs(r, "lab"), ints(r, "max_hints", "max_seconds"), known(r, "lab", labs))
	}},
	"curriculum_complete": {[]string{"curriculum"}, func(r map[string]any, _, curricula map[string]bool) []string {
		return slices.Concat(needs(r, "curriculum"), known(r, "curriculum", curricula))
	}},
	"xp": {[]string{"min"}, func(r map[string]any, _, _ map[string]bool) []string {
		return slices.Concat(needs(r, "min"), ints(r, "min"))
	}},
}

// ruleNames in the order messages list them.
var ruleNames = []string{"labs_passed", "lab_passed", "curriculum_complete", "xp"}

func needs(rule map[string]any, keys ...string) []string {
	var out []string
	for _, k := range keys {
		if _, ok := rule[k]; !ok {
			out = append(out, "needs "+k)
		}
	}
	return out
}

func ints(rule map[string]any, keys ...string) []string {
	var out []string
	for _, k := range keys {
		if v, ok := rule[k]; ok {
			if n, isInt := v.(int64); !isInt || n < 0 {
				out = append(out, k+" must be a non-negative integer")
			}
		}
	}
	return out
}

func known(rule map[string]any, key string, ids map[string]bool) []string {
	v, ok := rule[key]
	if !ok {
		return nil
	}
	if s, isStr := v.(string); isStr && ids[s] {
		return nil
	}
	return []string{fmt.Sprintf("unknown %s %s", key, pyRepr(v))}
}

// packErrors checks Phase 10's part of a pack (lab/labs.py errors): the schema, then cross-references within the
// pack and to the labs and curricula of the other packs compiled with it.
func packErrors(pk *loaded, otherLabs, otherCurricula map[string]bool, p *problems) {
	if errs := schemaErrors(labpackSchema(), pk.doc); len(errs) > 0 {
		p.addAll("", errs)
		return
	}
	doc := pk.doc
	labs, curricula := union(keySet(doc["labs"]), otherLabs), union(keySet(doc["curricula"]), otherCurricula)
	for _, cid := range pk.curriculumIDs {
		levels := doc["curricula"].(map[string]any)[cid].(map[string]any)["levels"].([]any)
		for i, level := range levels {
			for _, l := range level.(map[string]any)["labs"].([]any) {
				if !labs[l.(string)] {
					p.add("curricula/%s: level %d: unknown lab %s", cid, i+1, pyRepr(l))
				}
			}
		}
	}
	for _, lid := range pk.labIDs {
		lb := doc["labs"].(map[string]any)[lid].(map[string]any)
		where := "labs/" + lid
		requires := stringList(lb["requires"])
		for _, r := range requires {
			if !labs[r] {
				p.add("%s: requires unknown lab %s", where, pyRepr(r))
			}
		}
		if slices.Contains(requires, lid) {
			p.add("%s: a lab cannot require itself", where)
		}
		for _, phase := range []string{"setup", "teardown"} {
			steps, _ := stepList(lb[phase])
			p.steps(where+": "+phase+": ", lab.StepSet{Steps: steps})
		}
		steps := lb["steps"].([]any)
		for _, id := range duplicates(steps, "id") {
			p.add("%s: duplicate step id %s", where, pyRepr(id))
		}
		files := stringList(lb["files"])
		for _, s := range steps {
			step := s.(map[string]any)
			at := fmt.Sprintf("%s/%s: ", where, step["id"])
			if open, ok := step["open"]; ok && !slices.Contains(files, open.(string)) {
				p.add("%sopens %s, which is not in files", at, open)
			}
			check := step["check"].(map[string]any)
			kind := sortedKeys(check)[0]
			if !slices.Contains(checks, kind) {
				p.add("%sunknown check %s (one of %s)", at, pyRepr(kind), strings.Join(checks, ", "))
				continue
			}
			checkErrors(kind, check[kind], at, p)
		}
	}
	badges := doc["badges"].([]any)
	for _, id := range duplicates(badges, "id") {
		p.add("badges: duplicate id %s", pyRepr(id))
	}
	for _, b := range badges {
		badge := b.(map[string]any)
		rule := badge["rule"].(map[string]any)
		where := fmt.Sprintf("badges/%s", badge["id"])
		name, _ := rule["type"].(string)
		rt, ok := ruleTypes[name]
		if !ok {
			p.add("%s: unknown rule type %s (one of %s)", where, pyRepr(rule["type"]), strings.Join(ruleNames, ", "))
			continue
		}
		for _, k := range sortedKeys(rule) {
			if k != "type" && !slices.Contains(rt.keys, k) {
				p.add("%s: %s has no %s", where, name, pyRepr(k))
			}
		}
		p.addAll(where+": ", rt.valid(rule, labs, curricula))
	}
}

// image is a Markdown image's target.
var image = regexp.MustCompile(`!\[[^\]]*\]\(\s*<?([^)\s>]+)`)

// courseErrors checks a course that passed its schema: the course's references, its items (they exist, are unique,
// and the course's needs cover theirs), quizzes, challenges and lecture images.
func courseErrors(pk *loaded, c *course, allLabs map[string]any, courseIDs, badgeIDs map[string]bool, p *problems) {
	doc := c.doc["course"].(map[string]any)
	dir := "courses/" + c.id
	for _, r := range stringList(doc["requires"]) {
		if !courseIDs[r] {
			p.add("%s: requires unknown course %s", dir, pyRepr(r))
		}
	}
	if b, ok := doc["badge"].(string); ok && !badgeIDs[b] {
		p.add("%s: unknown badge %s", dir, pyRepr(b))
	}
	courseNeeds := stringList(doc["needs"])
	var seen []string
	for _, s := range doc["sections"].([]any) {
		for _, it := range s.(map[string]any)["items"].([]any) {
			kind, ref := item(it)
			if slices.Contains(seen, ref) {
				p.add("%s: duplicate item %s (item ids are unique within a course)", dir, pyRepr(ref))
			}
			seen = append(seen, ref)
			pool := allLabs
			if kind != "lab" {
				pool = c.doc[plural(kind)].(map[string]any)
			}
			target, ok := pool[ref]
			if !ok {
				p.add("%s: unknown %s %s", dir, kind, pyRepr(ref))
				continue
			}
			if kind != "lab" && kind != "challenge" {
				continue
			}
			t, _ := target.(map[string]any)
			for _, n := range stringList(t["needs"]) {
				if !slices.Contains(courseNeeds, n) {
					p.add("%s: %s needs %s, which the course's needs do not list", dir, ref, n)
				}
			}
		}
	}
	lectures := c.doc["lectures"].(map[string]any)
	for _, qid := range c.quizzes {
		quizErrors(c.doc["quizzes"].(map[string]any)[qid].(map[string]any), lectures,
			fmt.Sprintf("%s/quizzes/%s.yml: ", dir, qid), p)
	}
	for _, chid := range c.challenges {
		challengeErrors(c.doc["challenges"].(map[string]any)[chid].(map[string]any),
			fmt.Sprintf("%s/challenges/%s: ", dir, chid), p)
	}
	for _, lid := range c.lectures {
		md := lectures[lid].(map[string]any)["markdown"].(string)
		at := fmt.Sprintf("%s/lectures/%s.md: ", dir, lid)
		for _, m := range image.FindAllStringSubmatch(md, -1) {
			imageErrors(pk, dir+"/lectures", m[1], at, p)
		}
	}
}

// scheme starts a URL.
var scheme = regexp.MustCompile(`^[a-z]+:`)

// imageErrors checks a lecture image: a file in the pack's assets/ (the platform serves nothing else).
func imageErrors(pk *loaded, dir, target, at string, p *problems) {
	resolved := path.Join(dir, target)
	if scheme.MatchString(target) || strings.HasPrefix(target, "/") || !strings.HasPrefix(resolved, "assets/") {
		p.add("%simage %s is outside assets/ (lecture images are pack assets)", at, target)
		return
	}
	if info, err := fs.Stat(pk.src.FS, resolved); err != nil || !info.Mode().IsRegular() {
		p.add("%simage %s: %s is not a file in assets/", at, target, strings.TrimPrefix(resolved, "assets/"))
	}
}

func quizErrors(quiz, lectures map[string]any, at string, p *problems) {
	questions := quiz["questions"].([]any)
	for _, id := range duplicates(questions, "id") {
		p.add("%sduplicate question %s", at, pyRepr(id))
	}
	for _, x := range questions {
		q := x.(map[string]any)
		last := len(q["choices"].([]any)) - 1
		answers, isList := q["answer"].([]any)
		_, isInt := q["answer"].(int64)
		qtype, _ := q["type"].(string)
		if qtype == "" {
			qtype = "single"
		}
		if qtype == "single" && !isInt {
			p.add("%s%s: a single-answer question takes one index", at, q["id"])
			continue
		}
		if qtype == "multiple" && !isList {
			p.add("%s%s: a multiple-answer question takes a list of indexes", at, q["id"])
			continue
		}
		if !isList {
			answers = []any{q["answer"]}
		}
		for _, a := range answers {
			if n, _ := number(a); n > float64(last) {
				p.add("%s%s: answer %s is not a choice (0-%d)", at, q["id"], pyRepr(a), last)
			}
		}
		if see, ok := q["see"].(string); ok {
			if _, found := lectures[see]; !found {
				p.add("%s%s: see: unknown lecture %s in this course", at, q["id"], pyRepr(see))
			}
		}
	}
	if draw, ok := quiz["draw"].(int64); ok && draw > int64(len(questions)) {
		p.add("%sdraw %d is more than the %d questions", at, draw, len(questions))
	}
}

func challengeErrors(ch map[string]any, at string, p *problems) {
	starter := ch["starter"].(map[string]any)
	for _, f := range stringList(ch["files"]) {
		if _, ok := starter[f]; !ok {
			p.add("%sfiles: %s is not in starter/", at, f)
		}
	}
	for _, t := range ch["tests"].([]any) {
		test := t.(map[string]any)
		name := test["name"].(string)
		probeErrors(test["probe"], at+name+": probe: ", p)
		action, _ := stepList(test["action"])
		p.steps(at+name+": action: ", lab.StepSet{Steps: action, NoFaults: true})
	}
}

// item is a course item's kind and id ({lecture: id}, one key: the schema says so).
func item(v any) (kind, ref string) {
	m := v.(map[string]any)
	kind = sortedKeys(m)[0]
	ref, _ = m[kind].(string)
	return kind, ref
}

func plural(kind string) string {
	if kind == "quiz" {
		return "quizzes"
	}
	return kind + "s"
}

// duplicates are the values of key that more than one of items has, sorted.
func duplicates(items []any, key string) []string {
	count := map[string]int{}
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			if s, ok := m[key].(string); ok {
				count[s]++
			}
		}
	}
	var out []string
	for s, n := range count {
		if n > 1 {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// stringList is a list of strings (validated by a schema), nil when absent.
func stringList(v any) []string {
	l, _ := v.([]any)
	out := make([]string, 0, len(l))
	for _, x := range l {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func keySet(v any) map[string]bool {
	m, _ := v.(map[string]any)
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

func union(a, b map[string]bool) map[string]bool {
	out := make(map[string]bool, len(a)+len(b))
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}

// The pack schemas (academy/api), compiled once.
var (
	courseSchema  = sync.OnceValue(func() *jsonschema.Schema { return mustSchema("course.schema.json", api.CourseSchema) })
	labpackSchema = sync.OnceValue(func() *jsonschema.Schema { return mustSchema("labpack.schema.json", api.LabpackSchema) })
)

func mustSchema(name string, data []byte) *jsonschema.Schema {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		panic(err) // embedded at build time; every compile test covers it
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(name, doc); err != nil {
		panic(err)
	}
	return c.MustCompile(name)
}

var printer = message.NewPrinter(language.English)

// schemaErrors are why doc does not match the schema, one per failing keyword: "<where>: <why>", in location order.
func schemaErrors(s *jsonschema.Schema, doc any) []string {
	err := s.Validate(doc)
	if err == nil {
		return nil
	}
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return []string{"<root>: " + err.Error()}
	}
	var out []string
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			where := strings.Join(e.InstanceLocation, "/")
			if where == "" {
				where = "<root>"
			}
			out = append(out, where+": "+e.ErrorKind.LocalizedString(printer))
			return
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(ve)
	sort.Strings(out)
	return slices.Compact(out)
}

// number is a JSON number as a float64 (false: not a number).
func number(v any) (float64, bool) {
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}
