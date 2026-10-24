// Package contentimport mirrors the compiled packs (`opsforge academy compile`, api/bundle.schema.json) into the
// catalogue: the bundle is flattened into entries, compared with the catalogue, and only what changed is written.
// The same packs twice change nothing, not even a timestamp; removed content is retired, never deleted.
package contentimport

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
)

// BundleError is one validation error the compiler found in a pack.
type BundleError struct {
	Pack    string `json:"pack"`
	Message string `json:"message"`
}

// BundleErrors is a bundle that has validation errors: it is never imported.
type BundleErrors struct {
	Errors []BundleError
}

func (e *BundleErrors) Error() string {
	msgs := make([]string, len(e.Errors))
	for i, be := range e.Errors {
		msgs[i] = be.Pack + ": " + be.Message
	}
	return fmt.Sprintf("the packs have %d error(s): %s", len(e.Errors), strings.Join(msgs, "; "))
}

// head is what the catalogue's columns need from any bundle document.
type head struct {
	ID          string `json:"id"`
	Course      string `json:"course"`
	Title       string `json:"title"`
	Position    int    `json:"position"`
	ContentHash string `json:"content_hash"`
}

// childLists are the pack's lists of entries, left out of the pack's own document.
var childLists = []string{"courses", "lectures", "quizzes", "challenges", "labs", "badges"}

// Entries flattens a bundle (already checked against bundle.schema.json) into catalogue entries, in bundle order:
// per pack the pack, each course followed by its items, then lectures, quizzes, challenges, labs and badges.
func Entries(bundle json.RawMessage) ([]catalog.Entry, error) {
	var doc struct {
		Packs  []map[string]json.RawMessage `json:"packs"`
		Errors []BundleError                `json:"errors"`
	}
	if err := json.Unmarshal(bundle, &doc); err != nil {
		return nil, fmt.Errorf("bundle: %w", err)
	}
	if len(doc.Errors) > 0 {
		return nil, &BundleErrors{Errors: doc.Errors}
	}
	var out []catalog.Entry
	for _, pack := range doc.Packs {
		es, err := packEntries(pack)
		if err != nil {
			return nil, err
		}
		out = append(out, es...)
	}
	return out, nil
}

func packEntries(pack map[string]json.RawMessage) ([]catalog.Entry, error) {
	lists := map[string][]json.RawMessage{}
	own := map[string]json.RawMessage{}
	for k, v := range pack {
		own[k] = v
	}
	for _, name := range childLists {
		var list []json.RawMessage
		if err := json.Unmarshal(pack[name], &list); err != nil {
			return nil, fmt.Errorf("pack %s: %s: %w", pack["id"], name, err)
		}
		lists[name] = list
		delete(own, name)
	}
	packDoc, _ := json.Marshal(own) // map keys sorted: the same pack gives the same bytes
	p, err := entry(catalog.KindPack, packDoc, "", func(h head) string { return h.ID })
	if err != nil {
		return nil, err
	}
	out := []catalog.Entry{p}
	add := func(kind catalog.Kind, docs []json.RawMessage, key func(head) string) error {
		for _, d := range docs {
			e, err := entry(kind, d, p.Key, key)
			if err != nil {
				return err
			}
			out = append(out, e)
		}
		return nil
	}
	inCourse := func(h head) string { return h.Course + "/" + h.ID }
	byID := func(h head) string { return h.ID }
	for _, c := range lists["courses"] {
		course, err := entry(catalog.KindCourse, c, p.Key, byID)
		if err != nil {
			return nil, err
		}
		items, err := courseItems(course)
		if err != nil {
			return nil, err
		}
		out = append(append(out, course), items...)
	}
	for _, step := range []struct {
		kind catalog.Kind
		list string
		key  func(head) string
	}{
		{catalog.KindLecture, "lectures", inCourse}, {catalog.KindQuiz, "quizzes", inCourse},
		{catalog.KindChallenge, "challenges", inCourse}, {catalog.KindLab, "labs", byID},
		{catalog.KindBadge, "badges", byID},
	} {
		if err := add(step.kind, lists[step.list], step.key); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// entry is one bundle document as an entry of kind in pack. A document without a content hash (a badge) is
// hashed here, like the compiler's: the first 16 hex digits of a SHA-256.
func entry(kind catalog.Kind, doc json.RawMessage, pack string, key func(head) string) (catalog.Entry, error) {
	var h head
	if err := json.Unmarshal(doc, &h); err != nil {
		return catalog.Entry{}, fmt.Errorf("%s: %w", kind, err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, doc); err != nil {
		return catalog.Entry{}, fmt.Errorf("%s %s: %w", kind, h.ID, err)
	}
	hash := h.ContentHash
	if hash == "" {
		sum := sha256.Sum256(compact.Bytes())
		hash = hex.EncodeToString(sum[:8])
	}
	e := catalog.Entry{Kind: kind, Key: key(h), PackID: pack, CourseID: h.Course, Position: h.Position, Title: h.Title,
		Hash: hash, Doc: compact.Bytes()}
	if kind == catalog.KindPack {
		e.PackID = h.ID
	}
	return e, nil
}

// courseItems are the course's items, numbered through its sections; an item's document is its course entry
// plus the index of its section.
func courseItems(course catalog.Entry) ([]catalog.Entry, error) {
	var doc struct {
		Sections []struct {
			Items []map[string]any `json:"items"`
		} `json:"sections"`
	}
	if err := json.Unmarshal(course.Doc, &doc); err != nil {
		return nil, fmt.Errorf("course %s: %w", course.Key, err)
	}
	var out []catalog.Entry
	for s, section := range doc.Sections {
		for _, item := range section.Items {
			item["section"] = s
			data, _ := json.Marshal(item)
			id, _ := item["id"].(string)
			title, _ := item["title"].(string)
			hash, _ := item["content_hash"].(string)
			out = append(out, catalog.Entry{Kind: catalog.KindItem, Key: course.Key + "/" + id, PackID: course.PackID,
				CourseID: course.Key, Position: len(out), Title: title, Hash: hash, Doc: data})
		}
	}
	return out, nil
}

// Report counts the entries per kind by what the import did with them.
type Report struct {
	Added, Updated, Retired, Unchanged map[catalog.Kind]int
	Changed                            bool
}

// Changes are the writes that bring the catalogue to the bundle: entries to insert or replace (a retired entry
// that is back is replaced), keys to retire.
type Changes struct {
	Upsert []catalog.Entry
	Retire []catalog.Entry
	Report Report
}

// Diff compares the catalogue (current, retired entries included) with the bundle's entries (next).
func Diff(current, next []catalog.Entry) Changes {
	type key struct {
		kind catalog.Kind
		key  string
	}
	have := make(map[key]catalog.Entry, len(current))
	for _, e := range current {
		have[key{e.Kind, e.Key}] = e
	}
	ch := Changes{Report: Report{Added: map[catalog.Kind]int{}, Updated: map[catalog.Kind]int{},
		Retired: map[catalog.Kind]int{}, Unchanged: map[catalog.Kind]int{}}}
	seen := make(map[key]bool, len(next))
	for _, e := range next {
		k := key{e.Kind, e.Key}
		seen[k] = true
		old, ok := have[k]
		switch {
		case !ok:
			ch.Report.Added[e.Kind]++
		case old.Retired || !old.Same(e):
			ch.Report.Updated[e.Kind]++
		default:
			ch.Report.Unchanged[e.Kind]++
			continue
		}
		ch.Upsert = append(ch.Upsert, e)
	}
	for _, e := range current {
		if !seen[key{e.Kind, e.Key}] && !e.Retired {
			ch.Retire = append(ch.Retire, e)
			ch.Report.Retired[e.Kind]++
		}
	}
	ch.Report.Changed = len(ch.Upsert)+len(ch.Retire) > 0
	return ch
}
