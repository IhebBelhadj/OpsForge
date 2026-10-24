package contentimport_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
	"github.com/IhebBelhadj/opsforge/academy/internal/contentimport"
)

// golden is academy/testdata/bundle.golden.json, written by the Python tests from the demo pack.
func golden(t *testing.T) json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "bundle.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func entries(t *testing.T, bundle json.RawMessage) []catalog.Entry {
	t.Helper()
	got, err := contentimport.Entries(bundle)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func find(t *testing.T, es []catalog.Entry, kind catalog.Kind, key string) catalog.Entry {
	t.Helper()
	for _, e := range es {
		if e.Kind == kind && e.Key == key {
			return e
		}
	}
	t.Fatalf("no %s %s", kind, key)
	return catalog.Entry{}
}

func TestEntriesFlattenTheBundle(t *testing.T) {
	es := entries(t, golden(t))
	var keys []string
	for _, e := range es {
		keys = append(keys, string(e.Kind)+" "+e.Key)
		if e.PackID != "demo" || e.Hash == "" || !json.Valid(e.Doc) || e.Title == "" {
			t.Errorf("%s %s: %+v", e.Kind, e.Key, e)
		}
	}
	want := []string{
		"pack demo", "course k8s-intro",
		"item k8s-intro/what-is-a-pod", "item k8s-intro/pods-check", "item k8s-intro/meet-the-cluster",
		"item k8s-intro/first-pod",
		"lecture k8s-intro/what-is-a-pod", "quiz k8s-intro/pods-check", "challenge k8s-intro/first-pod",
		"lab meet-the-cluster", "badge intro-done",
	}
	if !slices.Equal(keys, want) {
		t.Fatalf("got %q", keys)
	}

	item := find(t, es, catalog.KindItem, "k8s-intro/meet-the-cluster")
	if item.CourseID != "k8s-intro" || item.Position != 2 || item.Hash != find(t, es, catalog.KindLab,
		"meet-the-cluster").Hash {
		t.Fatalf("item %+v", item)
	}
	var doc struct {
		Section int    `json:"section"`
		Type    string `json:"type"`
		Ref     string `json:"ref"`
	}
	if err := json.Unmarshal(item.Doc, &doc); err != nil || doc != (struct {
		Section int    `json:"section"`
		Type    string `json:"type"`
		Ref     string `json:"ref"`
	}{0, "lab", "meet-the-cluster"}) {
		t.Fatalf("item doc %s", item.Doc)
	}

	pack := find(t, es, catalog.KindPack, "demo")
	for _, list := range []string{`"courses"`, `"labs"`, `"lectures"`, `"badges"`} {
		if strings.Contains(string(pack.Doc), list) {
			t.Errorf("the pack's doc repeats its %s", list)
		}
	}
	if !strings.Contains(string(pack.Doc), `"hint_penalty":0.1`) || !strings.Contains(string(pack.Doc), `"assets"`) {
		t.Errorf("pack doc %s", pack.Doc)
	}
	if lec := find(t, es, catalog.KindLecture, "k8s-intro/what-is-a-pod"); lec.CourseID != "k8s-intro" {
		t.Errorf("lecture %+v", lec)
	}
	if badge := find(t, es, catalog.KindBadge, "intro-done"); len(badge.Hash) != 16 {
		t.Errorf("a badge's hash is its document's: %q", badge.Hash)
	}
}

func TestEntriesRefuseABundleWithErrors(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "bundle.invalid.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.Unmarshal(golden(t), &doc)
	doc["errors"] = []any{map[string]any{"pack": "demo", "message": "labs/x.yml: broken"}}
	withErrors, _ := json.Marshal(doc)
	_, err = contentimport.Entries(withErrors)
	var be *contentimport.BundleErrors
	if !errors.As(err, &be) || len(be.Errors) != 1 || be.Errors[0].Message != "labs/x.yml: broken" {
		t.Fatalf("err = %v", err)
	}
	if _, err := contentimport.Entries(data); err == nil {
		t.Fatal("bundle.invalid.json decoded")
	}
}

func entry(kind catalog.Kind, key, doc string) catalog.Entry {
	return catalog.Entry{Kind: kind, Key: key, PackID: "p", Title: key, Hash: "h-" + doc, Doc: json.RawMessage(doc)}
}

func TestDiff(t *testing.T) {
	lab := entry(catalog.KindLab, "a", `{"v":1}`)
	kept := entry(catalog.KindLab, "b", `{"v":1}`)
	gone := entry(catalog.KindLab, "c", `{"v":1}`)
	long := entry(catalog.KindLab, "d", `{"v":1}`)
	long.Retired = true
	back := entry(catalog.KindBadge, "e", `{"v":1}`)
	back.Retired = true
	moved := entry(catalog.KindItem, "x/i", `{"v":1}`)

	edited := lab
	edited.Doc, edited.Hash = json.RawMessage(`{"v":2}`), "h2"
	reordered := moved
	reordered.Position = 3
	fresh := entry(catalog.KindLab, "n", `{"v":1}`)

	ch := contentimport.Diff(
		[]catalog.Entry{lab, kept, gone, long, back, moved},
		[]catalog.Entry{edited, kept, back, reordered, fresh},
	)
	keys := func(es []catalog.Entry) []string {
		var out []string
		for _, e := range es {
			out = append(out, e.Key)
		}
		return out
	}
	if got := keys(ch.Upsert); !slices.Equal(got, []string{"a", "e", "x/i", "n"}) {
		t.Errorf("upsert %v", got)
	}
	if got := keys(ch.Retire); !slices.Equal(got, []string{"c"}) {
		t.Errorf("retire %v", got)
	}
	r := ch.Report
	if r.Added[catalog.KindLab] != 1 || r.Updated[catalog.KindLab] != 1 || r.Updated[catalog.KindBadge] != 1 ||
		r.Updated[catalog.KindItem] != 1 || r.Retired[catalog.KindLab] != 1 || r.Unchanged[catalog.KindLab] != 1 ||
		!r.Changed {
		t.Errorf("report %+v", r)
	}

	same := contentimport.Diff([]catalog.Entry{lab, kept, long}, []catalog.Entry{lab, kept})
	if len(same.Upsert)+len(same.Retire) != 0 || same.Report.Changed || same.Report.Unchanged[catalog.KindLab] != 2 {
		t.Errorf("no change: %+v", same)
	}
}
