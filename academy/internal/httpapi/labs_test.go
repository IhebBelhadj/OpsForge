package httpapi_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/IhebBelhadj/opsforge/academy/internal/httpapi"
)

const (
	startOp   = "/api/v1/labs/{lab}/attempts"
	checkOp   = "/api/v1/attempts/{attempt}/checks"
	hintOp    = "/api/v1/attempts/{attempt}/hints"
	resetOp   = "/api/v1/attempts/{attempt}/reset"
	abandonOp = "/api/v1/attempts/{attempt}/abandon"
	attemptOp = "/api/v1/attempts/{attempt}"
	opOp      = "/api/v1/operations/{operation}"
)

func startLab(t *testing.T, w *world, b *browser, lab string) httpapi.LabStarted {
	t.Helper()
	rec := post(t, b, "/api/v1/labs/"+lab+"/attempts", startOp, `{"course":"k8s-intro","item":"`+lab+`"}`)
	if rec.Code != 202 {
		t.Fatalf("start %s: %d %s", lab, rec.Code, rec.Body)
	}
	w.wait(t)
	return decode[httpapi.LabStarted](t, rec.Body.Bytes())
}

// finished reads an operation once the world's operations are done.
func finished(t *testing.T, w *world, b *browser, id string) httpapi.Operation {
	t.Helper()
	w.wait(t)
	rec := get(t, b, "/api/v1/operations/"+id, opOp)
	op := decode[httpapi.Operation](t, rec.Body.Bytes())
	if rec.Code != 200 || op.Status != "done" || op.Result == nil {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	return op
}

func attemptPath(id int64, rest string) string { return fmt.Sprintf("/api/v1/attempts/%d%s", id, rest) }

func TestALabFromStartToPass(t *testing.T) {
	w := newWorld(t)
	b := signIn(t, apiServer(t, w.deps))
	s := openStream(t, b, "", nil)
	started := startLab(t, w, b, "meet-the-cluster")
	a := started.Attempt
	if a.Status != "setup" || a.Namespace == nil || *a.Namespace != "lab-1" || a.Workspace != "lab-1" ||
		len(a.Steps) != 2 || *a.Course != "k8s-intro" || started.Operation.Kind != "lab.start" {
		t.Fatalf("%+v", started)
	}
	if op := finished(t, w, b, started.Operation.Id); op.Result.Status != "running" {
		t.Fatalf("setup: %+v", op.Result)
	}
	rec := get(t, b, "/api/v1/attempts/current", "/api/v1/attempts/current")
	cur := decode[httpapi.CurrentAttempt](t, rec.Body.Bytes())
	if cur.Attempt == nil || cur.Attempt.Status != "running" || *cur.Attempt.CurrentStep != "nodes" {
		t.Fatalf("%s", rec.Body)
	}

	rec = post(t, b, attemptPath(a.Id, "/hints"), hintOp, `{"step":"nodes"}`)
	if h := decode[httpapi.Hint](t, rec.Body.Bytes()); rec.Code != 200 || h.Hint != "Use kubectl get." || h.HintsUsed != 1 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	w.lab.failing = true
	rec = post(t, b, attemptPath(a.Id, "/checks"), checkOp, `{"step":"nodes"}`)
	if rec.Code != 202 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if out := finished(t, w, b, decode[httpapi.Operation](t, rec.Body.Bytes()).Id).Result; *out.Passed ||
		len(out.Results) != 1 || *out.Results[0].Got != 1 {
		t.Fatalf("failing check: %+v", out)
	}
	w.lab.failing = false
	rec = post(t, b, attemptPath(a.Id, "/checks"), checkOp, `{"step":"nodes"}`)
	finished(t, w, b, decode[httpapi.Operation](t, rec.Body.Bytes()).Id)
	rec = post(t, b, attemptPath(a.Id, "/checks"), checkOp, `{"step":"apply"}`)
	out := finished(t, w, b, decode[httpapi.Operation](t, rec.Body.Bytes()).Id).Result
	if out.Status != "passed" || *out.Xp != 90 || out.Seconds == nil {
		t.Fatalf("pass: %+v", out)
	}

	rec = get(t, b, attemptPath(a.Id, ""), attemptOp)
	if got := decode[httpapi.LabAttempt](t, rec.Body.Bytes()); got.Status != "passed" || *got.Xp != 90 ||
		got.CurrentStep != nil || !got.Steps[1].Passed || len(got.Steps[0].Hints) != 1 {
		t.Fatalf("%s", rec.Body)
	}
	if cur := decode[httpapi.CurrentAttempt](t, get(t, b, "/api/v1/attempts/current", "/api/v1/attempts/current").Body.Bytes()); cur.Attempt != nil {
		t.Fatalf("still current: %+v", cur.Attempt)
	}
	rec = get(t, b, "/api/v1/courses/k8s-intro/progress", "/api/v1/courses/{course}/progress")
	if p := decode[httpapi.CourseProgress](t, rec.Body.Bytes()); p.Items[2].Status != "completed" {
		t.Fatalf("the passed lab is not completed: %s", rec.Body)
	}

	// The stream carried the history and every operation's statuses, each matching the contract
	var kinds []string
	for len(kinds) == 0 || kinds[len(kinds)-1] != "history:lab.passed" {
		e := s.next(t)
		kind := e.event
		if e.event == "history" {
			kind += ":" + decode[httpapi.HistoryEvent](t, []byte(e.data)).Kind
		}
		kinds = append(kinds, kind)
	}
	if got := strings.Join(kinds, " "); !strings.HasPrefix(got, "history:lab.started operation operation") ||
		!strings.Contains(got, "history:hint.shown") || strings.Count(got, "history:step.checked") != 3 {
		t.Fatalf("stream: %s", got)
	}
	s.check(t)
}

func TestLabStartRefusals(t *testing.T) {
	w := newWorld(t)
	b := signIn(t, apiServer(t, w.deps))
	rec := post(t, b, "/api/v1/labs/nope/attempts", startOp, "")
	if rec.Code != 404 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	w.lab.stateErr = errors.New("python missing")
	rec = post(t, b, "/api/v1/labs/meet-the-cluster/attempts", startOp, "")
	if rec.Code != 503 || decode[httpapi.Problem](t, rec.Body.Bytes()).Type != "/problems/toolchain" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	w.lab.stateErr = nil
	startLab(t, w, b, "meet-the-cluster")
	rec = post(t, b, "/api/v1/labs/quiz-lab/attempts", startOp, "")
	if rec.Code != 409 || decode[httpapi.Problem](t, rec.Body.Bytes()).Type != "/problems/another-lab-running" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestAttemptRefusals(t *testing.T) {
	w := newWorld(t)
	b := signIn(t, apiServer(t, w.deps))
	a := startLab(t, w, b, "quiz-lab").Attempt

	for _, tc := range []struct {
		path, op, body string
		code           int
		kind           string
	}{
		{attemptPath(a.Id, "/checks"), checkOp, `{"step":"q"}`, 422, "/problems/answer-required"},
		{attemptPath(a.Id, "/checks"), checkOp, `{"step":"nope"}`, 404, "/problems/not-found"},
		{attemptPath(99, "/checks"), checkOp, `{"step":"q"}`, 404, "/problems/not-found"},
		{attemptPath(a.Id, "/hints"), hintOp, `{"step":"nope"}`, 404, "/problems/not-found"},
		{attemptPath(99, "/reset"), resetOp, "", 404, "/problems/not-found"},
		{attemptPath(99, "/abandon"), abandonOp, "", 404, "/problems/not-found"},
	} {
		rec := post(t, b, tc.path, tc.op, tc.body)
		if rec.Code != tc.code || decode[httpapi.Problem](t, rec.Body.Bytes()).Type != tc.kind {
			t.Errorf("%s %s: %d %s", tc.path, tc.body, rec.Code, rec.Body)
		}
	}
	post(t, b, attemptPath(a.Id, "/hints"), hintOp, `{"step":"q"}`)
	if rec := post(t, b, attemptPath(a.Id, "/hints"), hintOp, `{"step":"q"}`); rec.Code != 409 ||
		decode[httpapi.Problem](t, rec.Body.Bytes()).Type != "/problems/no-more-hints" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rec := post(t, b, attemptPath(a.Id, "/reset"), resetOp, "")
	if rec.Code != 202 {
		t.Fatalf("reset: %d %s", rec.Code, rec.Body)
	}
	finished(t, w, b, decode[httpapi.Operation](t, rec.Body.Bytes()).Id)
	rec = post(t, b, attemptPath(a.Id, "/abandon"), abandonOp, "")
	if rec.Code != 202 {
		t.Fatalf("abandon: %d %s", rec.Code, rec.Body)
	}
	if out := finished(t, w, b, decode[httpapi.Operation](t, rec.Body.Bytes()).Id).Result; out.Status != "abandoned" {
		t.Fatalf("%+v", out)
	}
	// Ended: every change is refused
	for path, op := range map[string]string{
		attemptPath(a.Id, "/checks"): checkOp, attemptPath(a.Id, "/hints"): hintOp,
		attemptPath(a.Id, "/reset"): resetOp, attemptPath(a.Id, "/abandon"): abandonOp,
	} {
		rec := post(t, b, path, op, `{"step":"q","answer":1}`)
		if rec.Code != 409 || decode[httpapi.Problem](t, rec.Body.Bytes()).Type != "/problems/attempt-not-running" {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
	if rec := get(t, b, attemptPath(99, ""), attemptOp); rec.Code != 404 {
		t.Fatalf("%d", rec.Code)
	}
	if rec := get(t, b, "/api/v1/operations/nope", opOp); rec.Code != 404 {
		t.Fatalf("%d", rec.Code)
	}
}

func TestStepsAreCheckedInOrderOverHTTP(t *testing.T) {
	w := newWorld(t)
	b := signIn(t, apiServer(t, w.deps))
	a := startLab(t, w, b, "meet-the-cluster").Attempt
	rec := post(t, b, attemptPath(a.Id, "/checks"), checkOp, `{"step":"apply"}`)
	if rec.Code != 409 || decode[httpapi.Problem](t, rec.Body.Bytes()).Type != "/problems/step-order" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}
