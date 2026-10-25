package httpapi_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/IhebBelhadj/opsforge/academy/internal/httpapi"
)

const (
	quizAttemptsOp = "/api/v1/courses/{course}/items/{item}/quiz-attempts"
	submitOp       = "/api/v1/quiz-attempts/{attempt}/submit"
)

func startQuiz(t *testing.T, b *browser) httpapi.QuizAttempt {
	t.Helper()
	rec := post(t, b, "/api/v1/courses/k8s-intro/items/pods-check/quiz-attempts", quizAttemptsOp, "")
	if rec.Code != 201 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	return decode[httpapi.QuizAttempt](t, rec.Body.Bytes())
}

func TestAQuizAttemptHidesItsKeysUntilSubmitted(t *testing.T) {
	w := newWorld(t)
	b := signIn(t, apiServer(t, w.deps))
	a := startQuiz(t, b)
	if len(a.Questions) != 2 || a.Result != nil || a.Pass != 70 || a.Deadline != nil {
		t.Fatalf("%+v", a)
	}
	rec := get(t, b, fmt.Sprintf("/api/v1/quiz-attempts/%d", a.Id), "/api/v1/quiz-attempts/{attempt}")
	for _, secret := range []string{"answer", "explain", "Sleeping is not"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Fatalf("an open attempt shows %q: %s", secret, rec.Body)
		}
	}

	s := openStream(t, b, "", nil)
	rec = post(t, b, fmt.Sprintf("/api/v1/quiz-attempts/%d/submit", a.Id), submitOp,
		`{"answers":{"shared":[1],"phases":[0,1,3]}}`)
	got := decode[httpapi.QuizAttempt](t, rec.Body.Bytes())
	if rec.Code != 200 || got.Result == nil || !got.Result.Passed || got.Result.Score != 100 || got.Result.Xp != 20 ||
		got.Result.Questions[1].Explain != "Sleeping is not a phase." {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if e := s.next(t); !strings.Contains(e.data, `"quiz.submitted"`) {
		t.Fatalf("%+v", e)
	}
	s.check(t)

	rec = get(t, b, "/api/v1/courses/k8s-intro/progress", "/api/v1/courses/{course}/progress")
	if p := decode[httpapi.CourseProgress](t, rec.Body.Bytes()); p.Items[1].Status != "completed" {
		t.Fatalf("the passed quiz is not completed: %s", rec.Body)
	}
	rec = get(t, b, "/api/v1/courses/k8s-intro/items/pods-check/quiz-attempts", quizAttemptsOp)
	if l := decode[httpapi.QuizAttemptList](t, rec.Body.Bytes()); len(l.Attempts) != 1 || !*l.Attempts[0].Passed {
		t.Fatalf("%s", rec.Body)
	}
}

func TestQuizRefusals(t *testing.T) {
	w := newWorld(t)
	srv := apiServer(t, w.deps)
	b := signIn(t, srv)
	a := startQuiz(t, b)
	path := fmt.Sprintf("/api/v1/quiz-attempts/%d/submit", a.Id)

	rec := post(t, b, path, submitOp, `{"answers":{"nope":[0]}}`)
	if rec.Code != 422 || decode[httpapi.Problem](t, rec.Body.Bytes()).Type != "/problems/invalid-answers" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	post(t, b, path, submitOp, `{"answers":{}}`)
	rec = post(t, b, path, submitOp, `{"answers":{}}`)
	if rec.Code != 409 || decode[httpapi.Problem](t, rec.Body.Bytes()).Type != "/problems/already-submitted" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	for _, p := range []string{"/api/v1/quiz-attempts/999/submit"} {
		if rec := post(t, b, p, submitOp, `{"answers":{}}`); rec.Code != 404 {
			t.Fatalf("%s: %d", p, rec.Code)
		}
	}
	if rec := get(t, b, "/api/v1/quiz-attempts/999", "/api/v1/quiz-attempts/{attempt}"); rec.Code != 404 {
		t.Fatalf("%d", rec.Code)
	}
	for item, want := range map[string]int{"what-is-a-pod": 409, "nope": 404} {
		path := "/api/v1/courses/k8s-intro/items/" + item + "/quiz-attempts"
		if rec := post(t, b, path, quizAttemptsOp, ""); rec.Code != want {
			t.Errorf("start %s: %d", item, rec.Code)
		}
		if rec := get(t, b, path, quizAttemptsOp); rec.Code != want {
			t.Errorf("list %s: %d", item, rec.Code)
		}
	}
	// Another learner's session cannot read or submit it (one local user today: a second user is made by hand)
	if _, err := w.store.DB.Exec(`INSERT INTO users (name, created_at) VALUES ('other', 'x')`); err != nil {
		t.Fatal(err)
	}
	if _, err := w.store.DB.Exec(`UPDATE quiz_attempts SET user_id = 2`); err != nil {
		t.Fatal(err)
	}
	if rec := get(t, b, fmt.Sprintf("/api/v1/quiz-attempts/%d", a.Id), "/api/v1/quiz-attempts/{attempt}"); rec.Code != 404 {
		t.Fatalf("another user's attempt: %d", rec.Code)
	}
}

func TestAQuizPastItsDeadlineIsRefused(t *testing.T) {
	w := newWorld(t)
	b := signIn(t, apiServer(t, w.deps))
	a := startQuiz(t, b)
	if _, err := w.store.DB.Exec(`UPDATE quiz_attempts SET deadline = '2000-01-01T00:00:00.000000000Z'`); err != nil {
		t.Fatal(err)
	}
	rec := post(t, b, fmt.Sprintf("/api/v1/quiz-attempts/%d/submit", a.Id), submitOp, `{"answers":{}}`)
	if rec.Code != 409 || decode[httpapi.Problem](t, rec.Body.Bytes()).Type != "/problems/quiz-expired" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}
