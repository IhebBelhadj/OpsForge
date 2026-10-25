package catalog_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
	"github.com/IhebBelhadj/opsforge/academy/internal/contentimport"
)

// memReader is the catalogue in memory: the golden bundle's entries, some of them retired.
type memReader struct{ entries []catalog.Entry }

func (m memReader) Live(_ context.Context, kind catalog.Kind, key string) (catalog.Entry, error) {
	for _, e := range m.entries {
		if e.Kind == kind && e.Key == key && !e.Retired {
			return e, nil
		}
	}
	return catalog.Entry{}, catalog.ErrNotFound
}

func (m memReader) LiveList(_ context.Context, kind catalog.Kind) ([]catalog.Entry, error) {
	var out []catalog.Entry
	for _, e := range m.entries {
		if e.Kind == kind && !e.Retired {
			out = append(out, e)
		}
	}
	return out, nil
}

func goldenEntries(t *testing.T) []catalog.Entry {
	t.Helper()
	data, err := os.ReadFile("../../testdata/bundle.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := contentimport.Entries(data)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func service(t *testing.T) *catalog.Service {
	t.Helper()
	return catalog.NewService(memReader{goldenEntries(t)})
}

func TestCourses(t *testing.T) {
	got, err := service(t).Courses(context.Background())
	if err != nil || len(got) != 1 {
		t.Fatalf("%+v, %v", got, err)
	}
	c := got[0]
	if c.ID != "k8s-intro" || c.Pack != "demo" || c.Title != "Kubernetes introduction" || c.Level != "beginner" ||
		c.Minutes != 33 || c.Items != 4 || c.Badge == nil || *c.Badge != "intro-done" || c.Layout != nil {
		t.Fatalf("%+v", c)
	}
}

func TestCourseHasItsSectionsInOrder(t *testing.T) {
	c, err := service(t).Course(context.Background(), "k8s-intro")
	if err != nil {
		t.Fatal(err)
	}
	if c.Hash != "ad7ef344f4bcb582" || len(c.Outcomes) != 2 || len(c.Sections) != 1 || c.Sections[0].Title != "Pods" {
		t.Fatalf("%+v", c)
	}
	var types []string
	for _, it := range c.Sections[0].Items {
		types = append(types, it.ID+":"+it.Type)
	}
	if strings.Join(types, " ") != "what-is-a-pod:lecture pods-check:quiz meet-the-cluster:lab first-pod:challenge" {
		t.Fatal(types)
	}
}

func TestRetiredOrUnknownIsNotFound(t *testing.T) {
	entries := goldenEntries(t)
	for i := range entries {
		if entries[i].Kind == catalog.KindCourse || entries[i].Key == "k8s-intro/pods-check" {
			entries[i].Retired = true
		}
	}
	svc := catalog.NewService(memReader{entries})
	ctx := context.Background()
	if got, err := svc.Courses(ctx); err != nil || len(got) != 0 {
		t.Fatalf("%+v, %v", got, err)
	}
	if _, err := svc.Course(ctx, "k8s-intro"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := service(t).Course(ctx, "nope"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := svc.Item(ctx, "k8s-intro", "pods-check"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := service(t).Item(ctx, "k8s-intro", "nope"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatal(err)
	}
}

func item(t *testing.T, id string) catalog.Item {
	t.Helper()
	it, err := service(t).Item(context.Background(), "k8s-intro", id)
	if err != nil {
		t.Fatal(err)
	}
	return it
}

func TestLecture(t *testing.T) {
	it := item(t, "what-is-a-pod")
	if it.Type != "lecture" || it.Section != 0 || it.Hash != "18e3d0e07448638c" || it.Lecture == nil ||
		!strings.HasPrefix(it.Lecture.Markdown, "A pod is") || len(it.Lecture.Resources) != 1 || it.Quiz != nil {
		t.Fatalf("%+v", it)
	}
}

// A quiz's questions come with an attempt (its draw and deadline); the item says only what the attempt will be.
func TestQuizShowsNoQuestions(t *testing.T) {
	q := item(t, "pods-check").Quiz
	if q == nil || q.Pass != 70 || q.XP != 20 || q.Questions != 2 || q.TimeLimitS != nil {
		t.Fatalf("%+v", q)
	}
}

func TestChallengeWithoutSolutionOrTests(t *testing.T) {
	c := item(t, "first-pod").Challenge
	if c == nil || c.Difficulty != "easy" || c.Starter["pod.yaml"] == "" || strings.Join(c.Tests, ",") != "pod is ready" ||
		c.Hints != 1 || c.TimeLimitS != 60 {
		t.Fatalf("%+v", c)
	}
}

func TestLabStepsShowTheirCheckNotHowItIsChecked(t *testing.T) {
	l := item(t, "meet-the-cluster").Lab
	if l == nil || !l.Namespace || strings.Join(l.Files, ",") != "hello.yaml" || len(l.Steps) != 2 {
		t.Fatalf("%+v", l)
	}
	probe, confirm := l.Steps[0], l.Steps[1]
	if probe.Check.Type != "probe" || probe.Hints != 1 || confirm.Check.Type != "confirm" ||
		confirm.Check.Label != "Applied" || confirm.Open != "hello.yaml" {
		t.Fatalf("%+v", l.Steps)
	}
}

func TestLabQuizCheckWithoutItsAnswer(t *testing.T) {
	entries := goldenEntries(t)
	for i, e := range entries {
		if e.Kind == catalog.KindLab {
			doc := strings.Replace(string(e.Doc), `"check":{"confirm":"Applied"}`,
				`"check":{"quiz":{"question":"How many sites?","choices":["two",3],"answer":1}}`, 1)
			entries[i].Doc = json.RawMessage(doc)
		}
	}
	it, err := catalog.NewService(memReader{entries}).Item(context.Background(), "k8s-intro", "meet-the-cluster")
	if err != nil {
		t.Fatal(err)
	}
	check := it.Lab.Steps[1].Check
	if check.Type != "quiz" || check.Question != "How many sites?" || strings.Join(check.Choices, ",") != "two,3" {
		t.Fatalf("%+v", check)
	}
	if data, _ := json.Marshal(it); strings.Contains(string(data), "answer") {
		t.Fatal(string(data))
	}
}

// Nothing that is earned leaks through any item: keys, explanations, reference solutions, how the checks and tests
// run, hint texts (shown one by one, at a cost).
func TestNoItemLeaksWhatIsEarned(t *testing.T) {
	secrets := []string{`"answer"`, "explain", "Sleeping is not a phase", "solution", "image: traefik/whoami",
		"probe\":{", "expect", "./opsforge", "Use kubectl get.", "The kind is Pod.", "setup", "teardown"}
	for _, id := range []string{"what-is-a-pod", "pods-check", "meet-the-cluster", "first-pod"} {
		data, _ := json.Marshal(item(t, id))
		for _, s := range secrets {
			if strings.Contains(string(data), s) {
				t.Errorf("%s shows %q: %s", id, s, data)
			}
		}
	}
}
