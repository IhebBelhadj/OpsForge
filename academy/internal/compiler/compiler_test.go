package compiler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"

	"github.com/IhebBelhadj/opsforge/academy/api"
	"github.com/IhebBelhadj/opsforge/academy/internal/lab"
)

const (
	testdata = "../../testdata"
	packs    = testdata + "/packs"
	demo     = packs + "/demo"
	repo     = "../../../labs" // the repository's packs
)

var epoch = time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)

// toolchain answers validate-steps like OpsForge for the steps these tests use: an unknown step type, and fault
// steps where they are refused.
type toolchain struct {
	sets [][]lab.StepSet
	err  error
}

func (tc *toolchain) CheckSteps(_ context.Context, sets []lab.StepSet) ([]lab.StepError, error) {
	tc.sets = append(tc.sets, sets)
	var out []lab.StepError
	for n, s := range sets {
		for i, step := range s.Steps {
			m, _ := step.(map[string]any)
			if _, ok := m["pign"]; ok {
				out = append(out, lab.StepError{Set: n, Message: fmt.Sprintf("steps[%d]: unknown step 'pign'", i)})
			}
			for _, fault := range []string{"partition", "heal"} {
				if _, ok := m[fault]; ok && s.NoFaults {
					out = append(out, lab.StepError{Set: n, Message: fmt.Sprintf("steps[%d]: %s: fault steps are not "+
						"allowed here", i, fault)})
				}
			}
		}
	}
	return out, tc.err
}

type bundle struct {
	Contract   int              `json:"contract"`
	CompiledAt string           `json:"compiled_at"`
	Packs      []map[string]any `json:"packs"`
	Errors     []struct {
		Pack    string `json:"pack"`
		Message string `json:"message"`
	} `json:"errors"`
}

func (b bundle) messages() []string {
	out := make([]string, len(b.Errors))
	for i, e := range b.Errors {
		out[i] = e.Message
	}
	return out
}

// compileRaw compiles the pack folders, named relative to dir.
func compileRaw(t *testing.T, dir string, paths ...string) []byte {
	t.Helper()
	src, err := Folders(dir, paths)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	data, err := Compile(context.Background(), src.List, &toolchain{}, epoch)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func compile(t *testing.T, dir string, paths ...string) bundle {
	t.Helper()
	var b bundle
	if err := json.Unmarshal(compileRaw(t, dir, paths...), &b); err != nil {
		t.Fatal(err)
	}
	return b
}

// pack is a writable copy of the demo pack; its folder is <tmp>/demo.
func pack(t *testing.T) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "demo")
	if err := os.CopyFS(dst, os.DirFS(demo)); err != nil {
		t.Fatal(err)
	}
	return dst
}

// edit rewrites a YAML file of a pack after change.
func edit(t *testing.T, file string, change func(map[string]any)) {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := decodeYAML(data)
	if err != nil {
		t.Fatal(err)
	}
	change(doc.(map[string]any))
	out, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	write(t, file, string(out))
}

func write(t *testing.T, file, text string) {
	t.Helper()
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func onlyPack(t *testing.T, b bundle) map[string]any {
	t.Helper()
	if len(b.Errors) > 0 || len(b.Packs) != 1 {
		t.Fatalf("errors %v, %d pack(s)", b.messages(), len(b.Packs))
	}
	return b.Packs[0]
}

func list(v any) []any          { return v.([]any) }
func obj(v any) map[string]any  { return v.(map[string]any) }
func at(v any, path ...any) any { return dig(v, path) }
func dig(v any, path []any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			v = obj(v)[k]
		case int:
			v = list(v)[k]
		}
	}
	return v
}

// The golden bundle feeds contentimport's tests and the e2e suite: ACADEMY_UPDATE_GOLDEN=1 rewrites it (review the
// diff).
func TestDemoPackCompilesToTheGoldenBundle(t *testing.T) {
	got := compileRaw(t, packs, demo)
	var indented bytes.Buffer
	if err := json.Indent(&indented, got, "", "  "); err != nil {
		t.Fatal(err)
	}
	indented.WriteString("\n")
	path := filepath.Join(testdata, "bundle.golden.json")
	if os.Getenv("ACADEMY_UPDATE_GOLDEN") != "" {
		write(t, path, indented.String())
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(indented.Bytes(), want) {
		t.Fatal("bundle.golden.json is out of date (ACADEMY_UPDATE_GOLDEN=1, review the diff)")
	}
}

func TestBundlesMatchTheSchema(t *testing.T) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(api.BundleSchema))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("bundle.schema.json", doc); err != nil {
		t.Fatal(err)
	}
	schema := c.MustCompile("bundle.schema.json")
	for _, paths := range [][]string{{demo}, {repo + "/core"}, {demo, repo + "/core"}} {
		inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(compileRaw(t, packs, paths...)))
		if err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(inst); err != nil {
			t.Errorf("%v: %v", paths, err)
		}
	}
}

func TestTheRepoPacksCompileWithoutErrors(t *testing.T) {
	src, err := Packs(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	data, err := Compile(context.Background(), src.List, &toolchain{}, epoch)
	if err != nil {
		t.Fatal(err)
	}
	var b bundle
	if err := json.Unmarshal(data, &b); err != nil {
		t.Fatal(err)
	}
	p := onlyPack(t, b)
	if p["id"] != "core" || p["path"] != "core" || len(list(p["labs"])) != 6 {
		t.Fatalf("pack %v at %v with %d labs", p["id"], p["path"], len(list(p["labs"])))
	}
}

func TestCompileIsDeterministic(t *testing.T) {
	if a, b := compileRaw(t, packs, demo), compileRaw(t, packs, demo); !bytes.Equal(a, b) {
		t.Fatal("two compiles of the same packs differ")
	}
}

func TestCompiledItemsCarryDefaultsAndDurations(t *testing.T) {
	p := onlyPack(t, compile(t, packs, demo))
	var items []string
	for _, it := range list(at(p, "courses", 0, "sections", 0, "items")) {
		items = append(items, fmt.Sprint(obj(it)["type"], ":", obj(it)["id"]))
	}
	if want := []string{"lecture:what-is-a-pod", "quiz:pods-check", "lab:meet-the-cluster", "challenge:first-pod"}; !slices.Equal(items, want) {
		t.Fatalf("items %v", items)
	}
	if m := at(p, "courses", 0, "minutes"); m != float64(8+5+10+10) || at(p, "courses", 0, "sections", 0, "minutes") != m {
		t.Fatalf("minutes %v", m)
	}
	quiz := obj(at(p, "quizzes", 0))
	if quiz["pass"] != 70.0 || quiz["xp"] != 20.0 || quiz["shuffle"] != true || quiz["time_limit_s"] != nil ||
		quiz["draw"] != nil {
		t.Fatalf("quiz defaults %v", quiz)
	}
	if !reflect.DeepEqual(at(quiz, "questions", 0, "answer"), []any{1.0}) || at(quiz, "questions", 0, "type") != "single" {
		t.Fatalf("question %v", at(quiz, "questions", 0))
	}
	ch := obj(at(p, "challenges", 0))
	if ch["time_limit_s"] != 60.0 || len(list(at(ch, "tests", 0, "action"))) != 0 ||
		!strings.Contains(at(ch, "solution", "pod.yaml").(string), "traefik/whoami") {
		t.Fatalf("challenge %v", ch)
	}
	hello, _ := os.ReadFile(demo + "/labs/meet-the-cluster/files/hello.yaml")
	if at(p, "labs", 0, "files", "hello.yaml") != string(hello) {
		t.Fatal("lab files")
	}
	if at(p, "assets", 0, "path") != "pod.svg" || at(p, "assets", 0, "bytes").(float64) <= 0 {
		t.Fatalf("assets %v", p["assets"])
	}
	if !strings.Contains(at(p, "lectures", 0, "markdown").(string), "${node:c}") {
		t.Fatal("texts keep their placeholders raw")
	}
}

// Content hashes are Python's (Phase 10's content_hash): Phase 10 and the Python compiler recorded them, and a
// pass whose hash differs from the lab's is flagged as stale. These values were computed by Python.
func TestContentHashesArePythons(t *testing.T) {
	for _, c := range []struct {
		v    any
		want string
	}{
		{map[string]any{"é": []any{int64(1), 1.5, true, nil, "😀 <&>\n"}}, "ad5b3543debd8170"},
		{map[string]any{"b": 1e16, "a": 0.0001, "c": 1e-05, "d": 100.0, "e": math.Copysign(0, -1)}, "20d3c254ff3728a8"},
		{[]any{}, "4f53cda18c2baa0c"},
	} {
		if got, err := contentHash(c.v); err != nil || got != c.want {
			t.Errorf("%v: %s, %v; want %s", c.v, got, err, c.want)
		}
	}
	p := onlyPack(t, compile(t, repo, repo+"/core"))
	hashes := map[string]any{}
	for _, l := range list(p["labs"]) {
		hashes[obj(l)["id"].(string)] = obj(l)["content_hash"]
	}
	// The Phase 10 hash of two core labs at the time of the port: update them when the lab files change
	if hashes["losing-a-site"] != "0211d33e7648f0f6" || hashes["private-networks"] != "1562687b5fbd7b74" {
		t.Fatalf("core lab hashes %v", hashes)
	}
}

// YAML is read as PyYAML's safe_load read it (YAML 1.1): testdata/yaml11.json is what Python made of yaml11.yml.
func TestYAMLIsReadAsPyYAMLReadIt(t *testing.T) {
	data, err := os.ReadFile("testdata/yaml11.yml")
	if err != nil {
		t.Fatal(err)
	}
	v, err := decodeYAML(data)
	if err != nil {
		t.Fatal(err)
	}
	got, err := pyJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/yaml11.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(got)+"\n" != string(want) {
		t.Fatalf("\ngot  %s\nwant %s", got, want)
	}
	for _, bad := range []string{"day: 2026-10-24", "x: .inf", "x: [unclosed"} {
		if _, err := decodeYAML([]byte(bad)); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func TestContentHashesFollowTheContent(t *testing.T) {
	dir := pack(t)
	before := onlyPack(t, compile(t, filepath.Dir(dir), dir))
	md := dir + "/courses/k8s-intro/lectures/what-is-a-pod.md"
	text, _ := os.ReadFile(md)
	write(t, md, strings.Replace(string(text), "network namespace", "network namespace and volumes", 1))
	after := onlyPack(t, compile(t, filepath.Dir(dir), dir))
	hash := func(p map[string]any, path ...any) any { return at(p, append(path, "content_hash")...) }
	if hash(after, "lectures", 0) == hash(before, "lectures", 0) || hash(after, "quizzes", 0) != hash(before, "quizzes", 0) ||
		hash(after, "courses", 0, "sections", 0, "items", 0) != hash(after, "lectures", 0) || after["content_hash"] == before["content_hash"] {
		t.Fatal("hashes do not follow the lecture")
	}

	write(t, dir+"/labs/meet-the-cluster/files/hello.yaml", "kind: ConfigMap\n")
	if hash(onlyPack(t, compile(t, filepath.Dir(dir), dir)), "labs", 0) == hash(after, "labs", 0) {
		t.Fatal("a lab's files count in its hash")
	}
}

const (
	courseFile = "/courses/k8s-intro/course.yml"
	quiz       = "/courses/k8s-intro/quizzes/pods-check.yml"
	challenge  = "/courses/k8s-intro/challenges/first-pod/challenge.yml"
	labFile    = "/labs/meet-the-cluster.yml"
	lecture    = "/courses/k8s-intro/lectures/what-is-a-pod.md"
)

func addItem(kind, ref string) func(map[string]any) {
	return func(d map[string]any) {
		items := at(d, "sections", 0, "items").([]any)
		obj(at(d, "sections", 0))["items"] = append(items, map[string]any{kind: ref})
	}
}

func set(key string, v any) func(map[string]any) {
	return func(d map[string]any) { d[key] = v }
}

func question(key string, v any) func(map[string]any) {
	return func(d map[string]any) { obj(at(d, "questions", 0))[key] = v }
}

func probeSteps(d map[string]any) map[string]any { return obj(at(d, "tests", 0, "probe")) }

var errorCases = []struct {
	name, file string
	change     func(map[string]any)
	want       string
}{
	{"unknown lab", courseFile, addItem("lab", "nope"), "courses/k8s-intro: unknown lab 'nope'"},
	{"unknown lecture", courseFile, addItem("lecture", "nope"), "unknown lecture 'nope'"},
	{"duplicate item", courseFile, addItem("quiz", "pods-check"), "duplicate item 'pods-check'"},
	{"unknown badge", courseFile, set("badge", "nope"), "unknown badge 'nope'"},
	{"needs too narrow", courseFile, set("needs", []any{}), "meet-the-cluster needs k3s, which the course's needs do not list"},
	{"unknown level", courseFile, set("level", "expert"), "courses/k8s-intro: course/level: value must be one of"},
	{"unknown requires", courseFile, set("requires", []any{"nope"}), "requires unknown course 'nope'"},
	{"answer out of range", quiz, question("answer", 7), "pods-check.yml: shared: answer 7 is not a choice (0-2)"},
	{"single with a list", quiz, question("answer", []any{0, 1}), "shared: a single-answer question takes one index"},
	{"multiple with one", quiz, func(d map[string]any) { obj(at(d, "questions", 1))["answer"] = 0 },
		"phases: a multiple-answer question takes a list of indexes"},
	{"unknown see", quiz, question("see", "nope"), "shared: see: unknown lecture 'nope' in this course"},
	{"duplicate question", quiz, func(d map[string]any) { d["questions"] = append(list(d["questions"]), at(d, "questions", 0)) },
		"duplicate question 'shared'"},
	{"no explanation", quiz, func(d map[string]any) { delete(obj(at(d, "questions", 0)), "explain") },
		"quizzes/pods-check/questions/0: missing property 'explain'"},
	{"draw too many", quiz, set("draw", 5), "draw 5 is more than the 2 questions"},
	{"missing starter file", challenge, set("files", []any{"missing.yaml"}), "files: missing.yaml is not in starter/"},
	{"fault in a test", challenge, func(d map[string]any) {
		p := probeSteps(d)
		p["steps"] = append([]any{map[string]any{"partition": "c"}}, list(p["steps"])...)
	}, "challenges/first-pod: pod is ready: probe: steps[0]: partition: fault steps are not allowed here"},
	{"fault in an action", challenge, func(d map[string]any) {
		obj(at(d, "tests", 0))["action"] = []any{map[string]any{"heal": nil}}
	}, "challenges/first-pod: pod is ready: action: steps[0]: heal: fault steps are not allowed here"},
	{"probe without expect", challenge, func(d map[string]any) { delete(probeSteps(d), "expect") },
		"pod is ready: probe: probe needs expect (what passing means)"},
	{"lab open not in files", labFile, func(d map[string]any) { obj(at(d, "steps", 1))["open"] = "other.yaml" },
		"labs/meet-the-cluster/apply: opens other.yaml, which is not in files"},
	{"lab file missing", labFile, set("files", []any{"hello.yaml", "gone.yaml"}),
		"labs/meet-the-cluster: files: gone.yaml is not in labs/meet-the-cluster/files/"},
	{"lab setup step", labFile, set("setup", []any{map[string]any{"pign": map[string]any{}}}),
		"labs/meet-the-cluster: setup: steps[0]: unknown step 'pign'"},
	{"lab probe step", labFile, func(d map[string]any) {
		p := obj(at(d, "steps", 0, "check", "probe"))
		p["steps"] = []any{map[string]any{"pign": nil, "name": "nodes"}}
	}, "labs/meet-the-cluster/nodes: steps[0]: unknown step 'pign'"},
	{"unknown check", labFile, func(d map[string]any) { obj(at(d, "steps", 1))["check"] = map[string]any{"vibe": 1} },
		"labs/meet-the-cluster/apply: unknown check 'vibe' (one of probe, quiz, confirm)"},
	{"quiz check", labFile, func(d map[string]any) {
		obj(at(d, "steps", 1))["check"] = map[string]any{"quiz": map[string]any{"question": "?", "choices": []any{"a", "b"}, "answer": 2}}
	}, "labs/meet-the-cluster/apply: quiz answer must be a choice index, 0-1"},
	{"lab requires itself", labFile, set("requires", []any{"meet-the-cluster"}), "labs/meet-the-cluster: a lab cannot require itself"},
	{"lab schema", labFile, set("xp", -1), "labs/meet-the-cluster/xp: minimum: got -1, want 0"},
}

func TestCompileReportsErrors(t *testing.T) {
	for _, c := range errorCases {
		t.Run(c.name, func(t *testing.T) {
			dir := pack(t)
			edit(t, dir+c.file, c.change)
			b := compile(t, filepath.Dir(dir), dir)
			if len(b.Packs) != 0 {
				t.Fatal("an invalid pack is left out of the bundle")
			}
			if !slices.ContainsFunc(b.messages(), func(m string) bool { return strings.Contains(m, c.want) }) {
				t.Fatalf("no %q in %q", c.want, b.messages())
			}
			for _, e := range b.Errors {
				if e.Pack != "demo" {
					t.Fatalf("error of pack %q", e.Pack)
				}
			}
		})
	}
}

func TestBadgeRules(t *testing.T) {
	for _, c := range []struct {
		rule map[string]any
		want string
	}{
		{map[string]any{"type": "streak"}, "badges/intro-done: unknown rule type 'streak' (one of labs_passed, lab_passed, curriculum_complete, xp)"},
		{map[string]any{"type": "xp"}, "badges/intro-done: needs min"},
		{map[string]any{"type": "xp", "min": -5}, "badges/intro-done: min must be a non-negative integer"},
		{map[string]any{"type": "xp", "min": 5, "lab": "x"}, "badges/intro-done: xp has no 'lab'"},
		{map[string]any{"type": "lab_passed", "lab": "nope"}, "badges/intro-done: unknown lab 'nope'"},
		{map[string]any{"type": "curriculum_complete", "curriculum": "nope"}, "badges/intro-done: unknown curriculum 'nope'"},
	} {
		dir := pack(t)
		write(t, dir+"/badges.yml", "- {id: intro-done, title: T, description: D, rule: "+flow(c.rule)+"}\n")
		if b := compile(t, filepath.Dir(dir), dir); !slices.Contains(b.messages(), c.want) {
			t.Errorf("%v: no %q in %q", c.rule, c.want, b.messages())
		}
	}
}

func flow(m map[string]any) string {
	data, _ := json.Marshal(m)
	return string(data)
}

func TestStepsAreCheckedByTheToolchainInOneCall(t *testing.T) {
	src, err := Folders(packs, []string{demo})
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	tc := &toolchain{}
	if _, err := Compile(context.Background(), src.List, tc, epoch); err != nil {
		t.Fatal(err)
	}
	if len(tc.sets) != 1 || len(tc.sets[0]) != 2 {
		t.Fatalf("calls %v", tc.sets)
	}
	for _, s := range tc.sets[0] {
		if !s.NoFaults || len(s.Expect) != 1 {
			t.Fatalf("a check's probe: faults refused, expect sent: %+v", s)
		}
	}

	tc = &toolchain{err: errors.New("opsforge: not found")}
	if _, err := Compile(context.Background(), src.List, tc, epoch); !errors.Is(err, ErrSteps) {
		t.Fatalf("a failed toolchain call: err = %v", err)
	}
}

func TestLectureImagesMustBePackAssets(t *testing.T) {
	dir := pack(t)
	text, _ := os.ReadFile(dir + lecture)
	for image, want := range map[string]string{
		"../../../assets/missing.svg": "image ../../../assets/missing.svg: missing.svg is not a file in assets/",
		"../../../pack.yml":           "is outside assets/",
		"../../../../outside.svg":     "is outside assets/",
		"https://example.com/x.png":   "is outside assets/",
	} {
		write(t, dir+lecture, strings.Replace(string(text), "../../../assets/pod.svg", image, 1))
		if b := compile(t, filepath.Dir(dir), dir); !slices.ContainsFunc(b.messages(), func(m string) bool {
			return strings.Contains(m, want)
		}) {
			t.Errorf("%s: no %q in %q", image, want, b.messages())
		}
	}
}

func TestFilesThatCannotBeRead(t *testing.T) {
	for name, c := range map[string]struct{ file, text, want string }{
		"no front matter": {lecture, "No front matter here.\n", "courses/k8s-intro/lectures/what-is-a-pod.md: needs YAML front matter"},
		"broken yaml":     {quiz, "title: [unclosed\n", "courses/k8s-intro/quizzes/pods-check.yml: cannot read: yaml: line"},
		"not utf-8":       {labFile, "title: \xff\n", "labs/meet-the-cluster.yml: cannot read: not UTF-8 text"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := pack(t)
			write(t, dir+c.file, c.text)
			if b := compile(t, filepath.Dir(dir), dir); !slices.ContainsFunc(b.messages(), func(m string) bool {
				return strings.HasPrefix(m, c.want)
			}) {
				t.Fatalf("no %q in %q", c.want, b.messages())
			}
		})
	}
}

func TestIDsAreUniqueAcrossPacks(t *testing.T) {
	dir := pack(t)
	other := filepath.Join(t.TempDir(), "other")
	if err := os.CopyFS(other, os.DirFS(dir)); err != nil {
		t.Fatal(err)
	}
	edit(t, other+"/pack.yml", set("id", "other"))
	msgs := compile(t, filepath.Dir(dir), dir, other).messages()
	for _, want := range []string{"course 'k8s-intro' is also in pack demo", "lab 'meet-the-cluster' is also in pack demo"} {
		if !slices.Contains(msgs, want) {
			t.Errorf("no %q in %q", want, msgs)
		}
	}
}

func TestSectionsMayUseLabsOfOtherPacks(t *testing.T) {
	dir := pack(t)
	edit(t, dir+courseFile, addItem("lab", "private-networks"))
	edit(t, dir+courseFile, set("needs", []any{"k3s", "vpn"}))
	b := compile(t, filepath.Dir(dir), dir, repo+"/core")
	if len(b.Errors) > 0 || len(b.Packs) != 2 {
		t.Fatalf("errors %v", b.messages())
	}

	// ... unless that pack has errors
	core := filepath.Join(t.TempDir(), "core")
	if err := os.CopyFS(core, os.DirFS(repo+"/core")); err != nil {
		t.Fatal(err)
	}
	edit(t, core+"/labs/private-networks.yml", set("xp", -1))
	b = compile(t, filepath.Dir(dir), dir, core)
	want := "courses/k8s-intro: lab 'private-networks' is in pack core, which has errors"
	if len(b.Packs) != 0 || !slices.Contains(b.messages(), want) {
		t.Fatalf("%d packs: %q", len(b.Packs), b.messages())
	}
}

func TestFolders(t *testing.T) {
	if _, err := Folders(packs, []string{t.TempDir()}); !errors.Is(err, ErrNotAPack) {
		t.Fatalf("a folder without pack.yml: err = %v", err)
	}
	src, err := Packs(filepath.Join(t.TempDir(), "nowhere"))
	if err != nil || len(src.List) != 0 {
		t.Fatalf("no content folder: %v, %v", src, err)
	}
	src, err = Folders(packs, []string{demo, repo + "/core"})
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	abs, _ := filepath.Abs(repo + "/core")
	if src.List[0].Path != "demo" || src.List[1].Path != abs {
		t.Fatalf("paths %q, %q", src.List[0].Path, src.List[1].Path)
	}
}
