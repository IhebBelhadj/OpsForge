package compiler

import (
	"errors"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Source is one pack folder: Path names it in the bundle (relative to the content folder), FS reads it.
type Source struct {
	Path string
	FS   fs.FS
}

// loaded is a pack as read: Phase 10's document (pack, curricula, labs, badges; the labpack schema), its courses
// (the course schema), each lab's files, and the problems met while reading.
type loaded struct {
	src           Source
	doc           map[string]any
	labIDs        []string // in file-name order, as the bundle lists them
	curriculumIDs []string
	courses       []*course
	files         map[string]map[string]any
	errs          []string
}

// course is a course folder: doc is {course, lectures, quizzes, challenges}, the ids in file-name order.
type course struct {
	id                            string
	doc                           map[string]any
	lectures, quizzes, challenges []string
}

// ordered is a mapping whose order matters (ids in file-name order, as the bundle lists them).
type ordered struct {
	keys   []string
	values map[string]any
}

func (o *ordered) set(k string, v any) {
	if o.values == nil {
		o.values = map[string]any{}
	}
	if _, ok := o.values[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.values[k] = v
}

// lectureFile is front matter between --- lines, then the Markdown.
var lectureFile = regexp.MustCompile(`(?s)\A---\n(.*?)\n---\n?(.*)\z`)

// reader reads one pack's files, collecting problems (`<path in the pack>: message`) instead of failing.
type reader struct {
	fsys fs.FS
	errs []string
}

func (r *reader) error(p, msg string) {
	r.errs = append(r.errs, p+": "+msg)
}

// text is a file as Python's read_text gives it: UTF-8, with \r\n and \r read as \n.
func (r *reader) text(p string) (string, bool) {
	data, err := fs.ReadFile(r.fsys, p)
	if err != nil {
		r.error(p, "cannot read: "+fsError(err))
		return "", false
	}
	if !utf8.Valid(data) {
		r.error(p, "cannot read: not UTF-8 text")
		return "", false
	}
	return strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n"), true
}

func (r *reader) yaml(p string, def any) any {
	text, ok := r.text(p)
	if !ok {
		return def
	}
	doc, err := decodeYAML([]byte(text))
	if err != nil {
		r.error(p, "cannot read: "+strings.ReplaceAll(err.Error(), "\n", " "))
		return def
	}
	if doc == nil {
		return def
	}
	return doc
}

func (r *reader) lecture(p string) any {
	text, ok := r.text(p)
	if !ok {
		return nil
	}
	m := lectureFile.FindStringSubmatch(text)
	if m == nil {
		r.error(p, "needs YAML front matter (--- title, minutes ---) before the Markdown")
		return nil
	}
	meta, err := decodeYAML([]byte(m[1]))
	if err != nil {
		r.error(p, "front matter: "+strings.ReplaceAll(err.Error(), "\n", " "))
		return nil
	}
	if !truthy(meta) {
		meta = map[string]any{}
	}
	doc, ok := meta.(map[string]any)
	if !ok {
		r.error(p, "front matter must be a mapping")
		return nil
	}
	doc["markdown"] = strings.Trim(m[2], "\n") + "\n"
	return doc
}

// files are every file under dir (none if it does not exist) as {relative path: text}.
func (r *reader) files(dir string) map[string]any {
	out := map[string]any{}
	_ = fs.WalkDir(r.fsys, dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !isFile(r.fsys, p, d) {
			return nil //nolint:nilerr // a missing folder has no files
		}
		if text, ok := r.text(p); ok {
			out[strings.TrimPrefix(p, dir+"/")] = text
		}
		return nil
	})
	return out
}

// glob lists dir's entries whose name ends with suffix, in name order (fs.ReadDir sorts).
func (r *reader) glob(dir, suffix string) []string {
	entries, _ := fs.ReadDir(r.fsys, dir)
	var out []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), suffix) {
			out = append(out, e.Name())
		}
	}
	return out
}

// subdirs lists dir's folders that hold file, in name order.
func (r *reader) subdirs(dir, file string) []string {
	var out []string
	for _, name := range r.glob(dir, "") {
		if _, err := fs.Stat(r.fsys, path.Join(dir, name, file)); err == nil {
			out = append(out, name)
		}
	}
	return out
}

func (r *reader) exists(p string) bool {
	_, err := fs.Stat(r.fsys, p)
	return err == nil
}

func isFile(fsys fs.FS, p string, d fs.DirEntry) bool {
	if d.Type().IsRegular() {
		return true
	}
	if d.IsDir() {
		return false
	}
	info, err := fs.Stat(fsys, p) // a link: what it points to
	return err == nil && info.Mode().IsRegular()
}

func fsError(err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "no such file"
	case errors.Is(err, fs.ErrPermission):
		return "permission denied"
	}
	return err.Error()
}

// load reads a pack folder.
func load(src Source) *loaded {
	r := &reader{fsys: src.FS}
	curricula, labs := map[string]any{}, ordered{}
	for _, name := range r.glob("curricula", ".yml") {
		curricula[strings.TrimSuffix(name, ".yml")] = r.yaml("curricula/"+name, map[string]any{})
	}
	for _, name := range r.glob("labs", ".yml") {
		labs.set(strings.TrimSuffix(name, ".yml"), r.yaml("labs/"+name, map[string]any{}))
	}
	var badges any = []any{}
	if r.exists("badges.yml") {
		badges = r.yaml("badges.yml", []any{})
	}
	p := &loaded{
		src: src,
		doc: map[string]any{
			"pack": r.yaml("pack.yml", map[string]any{}), "curricula": curricula, "labs": labs.values, "badges": badges,
		},
		files: map[string]map[string]any{},
	}
	if labs.values == nil {
		p.doc["labs"] = map[string]any{}
	}
	p.labIDs = labs.keys
	p.curriculumIDs = r.glob("curricula", ".yml")
	for i, name := range p.curriculumIDs {
		p.curriculumIDs[i] = strings.TrimSuffix(name, ".yml")
	}
	for _, cid := range r.subdirs("courses", "course.yml") {
		p.courses = append(p.courses, loadCourse(r, cid))
	}
	for _, lid := range labs.keys {
		p.files[lid] = r.files("labs/" + lid + "/files")
	}
	p.errs = r.errs
	return p
}

// loadCourse reads a course folder into the document course.schema.json describes (nil for unreadable files).
func loadCourse(r *reader, cid string) *course {
	dir := "courses/" + cid
	c := &course{id: cid}
	lectures, quizzes, challenges := map[string]any{}, map[string]any{}, map[string]any{}
	for _, name := range r.glob(dir+"/lectures", ".md") {
		id := strings.TrimSuffix(name, ".md")
		c.lectures = append(c.lectures, id)
		lectures[id] = r.lecture(dir + "/lectures/" + name)
	}
	for _, name := range r.glob(dir+"/quizzes", ".yml") {
		id := strings.TrimSuffix(name, ".yml")
		c.quizzes = append(c.quizzes, id)
		quizzes[id] = r.yaml(dir+"/quizzes/"+name, map[string]any{})
	}
	for _, chid := range r.subdirs(dir+"/challenges", "challenge.yml") {
		c.challenges = append(c.challenges, chid)
		base := dir + "/challenges/" + chid
		doc := map[string]any{}
		switch c := r.yaml(base+"/challenge.yml", map[string]any{}).(type) {
		case map[string]any:
			doc = c
		default:
			if truthy(c) {
				r.error(base+"/challenge.yml", "a challenge is a mapping")
			}
		}
		doc["starter"] = r.files(base + "/starter")
		doc["solution"] = r.files(base + "/solution")
		challenges[chid] = doc
	}
	c.doc = map[string]any{
		"course": r.yaml(dir+"/course.yml", map[string]any{}), "lectures": lectures, "quizzes": quizzes,
		"challenges": challenges,
	}
	return c
}

// truthy is Python's truth of a value: None, false, 0, "" and empty containers are false.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case int64:
		return x != 0
	case float64:
		return x != 0
	case string:
		return x != ""
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}
