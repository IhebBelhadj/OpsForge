package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
	catalogsqlite "github.com/IhebBelhadj/opsforge/academy/internal/catalog/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/contentimport"
	"github.com/IhebBelhadj/opsforge/academy/internal/httpapi"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform/sqlite"
)

// withCatalog serves the golden bundle's catalogue from a real database.
func withCatalog(t *testing.T) func(*httpapi.Deps) {
	t.Helper()
	data, err := os.ReadFile("../../testdata/bundle.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := contentimport.Entries(data)
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "academy.db"), platform.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo := catalogsqlite.NewRepo(store.DB)
	if err := repo.Apply(context.Background(), entries, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	return func(d *httpapi.Deps) { d.Catalog = catalog.NewService(repo) }
}

func get(t *testing.T, b *browser, path, op string) *httptest.ResponseRecorder {
	t.Helper()
	rec := record(b.send(t, http.MethodGet, path, "", nil))
	checkContract(t, "GET", op, rec)
	return rec
}

func TestCourseList(t *testing.T) {
	b := signIn(t, apiServer(t, withCatalog(t)))
	rec := get(t, b, "/api/v1/courses", "/api/v1/courses")
	got := decode[httpapi.CourseList](t, rec.Body.Bytes())
	if rec.Code != 200 || len(got.Courses) != 1 || got.Courses[0].Id != "k8s-intro" || got.Courses[0].Items != 4 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestCoursePage(t *testing.T) {
	b := signIn(t, apiServer(t, withCatalog(t)))
	const op = "/api/v1/courses/{course}"
	rec := get(t, b, "/api/v1/courses/k8s-intro", op)
	got := decode[httpapi.Course](t, rec.Body.Bytes())
	if rec.Code != 200 || len(got.Sections) != 1 || len(got.Sections[0].Items) != 4 || got.Sections[0].Items[2].Type != "lab" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rec = get(t, b, "/api/v1/courses/nope", op)
	if p := decode[httpapi.Problem](t, rec.Body.Bytes()); rec.Code != 404 || p.Type != "/problems/not-found" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestItems(t *testing.T) {
	b := signIn(t, apiServer(t, withCatalog(t)))
	const op = "/api/v1/courses/{course}/items/{item}"
	secrets := []string{`"answer"`, "explain", "solution", "image: traefik/whoami", "expect", "./opsforge",
		"Use kubectl get.", "The kind is Pod."}
	for id, typ := range map[string]string{"what-is-a-pod": "lecture", "pods-check": "quiz",
		"meet-the-cluster": "lab", "first-pod": "challenge"} {
		rec := get(t, b, "/api/v1/courses/k8s-intro/items/"+id, op)
		got := decode[httpapi.Item](t, rec.Body.Bytes())
		if rec.Code != 200 || string(got.Type) != typ || got.Course != "k8s-intro" {
			t.Fatalf("%s: %d %s", id, rec.Code, rec.Body)
		}
		for _, s := range secrets {
			if strings.Contains(rec.Body.String(), s) {
				t.Errorf("%s shows %q: %s", id, s, rec.Body)
			}
		}
	}
	for _, path := range []string{"/api/v1/courses/k8s-intro/items/nope", "/api/v1/courses/nope/items/first-pod"} {
		if rec := get(t, b, path, op); rec.Code != 404 {
			t.Fatalf("%s: %d", path, rec.Code)
		}
	}
}

func TestCatalogueNeedsASession(t *testing.T) {
	anon := &browser{srv: apiServer(t, withCatalog(t)), client: http.DefaultClient}
	for path, op := range map[string]string{"/api/v1/courses": "/api/v1/courses",
		"/api/v1/courses/k8s-intro":                 "/api/v1/courses/{course}",
		"/api/v1/courses/k8s-intro/items/first-pod": "/api/v1/courses/{course}/items/{item}"} {
		if rec := get(t, anon, path, op); rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: %d", path, rec.Code)
		}
	}
}
