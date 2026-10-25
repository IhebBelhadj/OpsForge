package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/IhebBelhadj/opsforge/academy/internal/httpapi"
)

func post(t *testing.T, b *browser, path, op, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := record(b.send(t, http.MethodPost, path, body, nil))
	checkContract(t, "POST", op, rec)
	return rec
}

func TestCompletingALectureShowsInTheCourseProgress(t *testing.T) {
	w := newWorld(t)
	b := signIn(t, apiServer(t, w.deps))
	const op = "/api/v1/courses/{course}/items/{item}/complete"
	rec := post(t, b, "/api/v1/courses/k8s-intro/items/what-is-a-pod/complete", op, "")
	ip := decode[httpapi.ItemProgress](t, rec.Body.Bytes())
	if rec.Code != 200 || ip.Status != "completed" || ip.CompletedAt == nil {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}

	rec = get(t, b, "/api/v1/courses/k8s-intro/progress", "/api/v1/courses/{course}/progress")
	p := decode[httpapi.CourseProgress](t, rec.Body.Bytes())
	if p.Completed != 1 || p.Total != 4 || p.Complete || p.Resume == nil || *p.Resume != "pods-check" ||
		p.Items[0].Status != "completed" {
		t.Fatalf("%s", rec.Body)
	}
	rec = get(t, b, "/api/v1/progress", "/api/v1/progress")
	if o := decode[httpapi.ProgressOverview](t, rec.Body.Bytes()); len(o.Courses) != 1 || o.Courses[0].Completed != 1 {
		t.Fatalf("%s", rec.Body)
	}

	for path, want := range map[string]int{
		"/api/v1/courses/k8s-intro/items/pods-check/complete": 409,
		"/api/v1/courses/k8s-intro/items/nope/complete":       404,
	} {
		if rec := post(t, b, path, op, ""); rec.Code != want {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
	if rec := get(t, b, "/api/v1/courses/nope/progress", "/api/v1/courses/{course}/progress"); rec.Code != 404 {
		t.Fatalf("%d", rec.Code)
	}
}

func TestVisitingAnItemMakesItTheResumePoint(t *testing.T) {
	w := newWorld(t)
	b := signIn(t, apiServer(t, w.deps))
	const op = "/api/v1/courses/{course}/items/{item}/visit"
	if rec := post(t, b, "/api/v1/courses/k8s-intro/items/meet-the-cluster/visit", op, ""); rec.Code != 204 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rec := get(t, b, "/api/v1/courses/k8s-intro/progress", "/api/v1/courses/{course}/progress")
	if p := decode[httpapi.CourseProgress](t, rec.Body.Bytes()); *p.Resume != "meet-the-cluster" ||
		p.Items[2].Status != "in_progress" || p.LastOpenedAt == nil {
		t.Fatalf("%s", rec.Body)
	}
	if rec := post(t, b, "/api/v1/courses/k8s-intro/items/nope/visit", op, ""); rec.Code != 404 {
		t.Fatalf("%d", rec.Code)
	}
}

func TestNotesRoundTrip(t *testing.T) {
	w := newWorld(t)
	b := signIn(t, apiServer(t, w.deps))
	const op = "/api/v1/courses/{course}/items/{item}/notes"
	path := "/api/v1/courses/k8s-intro/items/what-is-a-pod/notes"
	rec := get(t, b, path, op)
	if n := decode[httpapi.Note](t, rec.Body.Bytes()); rec.Code != 200 || n.Body != "" || n.UpdatedAt != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rec = record(b.send(t, http.MethodPut, path, `{"body":"pods share **localhost**"}`, nil))
	checkContract(t, "PUT", op, rec)
	if n := decode[httpapi.Note](t, rec.Body.Bytes()); rec.Code != 200 || n.UpdatedAt == nil {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if n := decode[httpapi.Note](t, get(t, b, path, op).Body.Bytes()); n.Body != "pods share **localhost**" {
		t.Fatalf("%+v", n)
	}
	long := `{"body":"` + strings.Repeat("x", 64<<10+1) + `"}`
	rec = record(b.send(t, http.MethodPut, path, long, nil))
	checkContract(t, "PUT", op, rec)
	if rec.Code != 422 {
		t.Fatalf("%d", rec.Code)
	}
	nope := "/api/v1/courses/k8s-intro/items/nope/notes"
	if rec := get(t, b, nope, op); rec.Code != 404 {
		t.Fatalf("%d", rec.Code)
	}
	rec = record(b.send(t, http.MethodPut, nope, `{"body":""}`, nil))
	checkContract(t, "PUT", op, rec)
	if rec.Code != 404 {
		t.Fatalf("%d", rec.Code)
	}
}

func TestCompletingTheLastItemCompletesTheCourse(t *testing.T) {
	w := newWorld(t)
	b := signIn(t, apiServer(t, w.deps))
	s := openStream(t, b, "", nil)
	post(t, b, "/api/v1/courses/k8s-intro/items/what-is-a-pod/complete",
		"/api/v1/courses/{course}/items/{item}/complete", "")
	if e := s.next(t); !strings.Contains(e.data, `"lecture.completed"`) {
		t.Fatalf("%+v", e)
	}
	s.check(t)
}
