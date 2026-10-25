package httpapi_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/IhebBelhadj/opsforge/academy/internal/httpapi"
)

const (
	labStateOp = "/api/v1/lab/state"
	itemOp     = "/api/v1/courses/{course}/items/{item}"
)

func TestLabState(t *testing.T) {
	w := newWorld(t)
	b := signIn(t, apiServer(t, w.deps))
	rec := get(t, b, "/api/v1/lab/state", labStateOp)
	st := decode[httpapi.LabState](t, rec.Body.Bytes())
	if rec.Code != 200 || !st.Up || !st.Fault || st.Links["a-b"].Condition != httpapi.LabLinkConditionCut ||
		*st.Links["a-b"].Live.Loss != "100%" || st.Nodes[0].Name != "a-cp1" || st.Sites["a"].PublicIp == "" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "placeholders") {
		t.Fatalf("placeholders are the platform's, not the page's: %s", rec.Body)
	}

	w.state.Invalidate()
	w.lab.stateErr = errors.New("python missing")
	defer func() { w.lab.stateErr = nil }()
	rec = get(t, b, "/api/v1/lab/state", labStateOp)
	if p := decode[httpapi.Problem](t, rec.Body.Bytes()); rec.Code != 503 || p.Type != "/problems/toolchain" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestItemTextsAreResolvedForTheRunningLab(t *testing.T) {
	w := newWorld(t)
	b := signIn(t, apiServer(t, w.deps))
	rec := get(t, b, "/api/v1/courses/k8s-intro/items/what-is-a-pod", itemOp)
	if it := decode[httpapi.Item](t, rec.Body.Bytes()); !strings.Contains(it.Lecture.Markdown, "Pods on `c-w1` run") {
		t.Fatalf("%s", it.Lecture.Markdown)
	}

	// ${ns} is the learner's own attempt's namespace, once one runs
	lab := func() httpapi.LabView {
		rec := get(t, b, "/api/v1/courses/k8s-intro/items/meet-the-cluster", itemOp)
		return *decode[httpapi.Item](t, rec.Body.Bytes()).Lab
	}
	if body := lab().Steps[1].Body; body != "Apply `hello.yaml` in `${ns}`." {
		t.Fatalf("before the attempt: %q", body)
	}
	startLab(t, w, b, "meet-the-cluster")
	if body := lab().Steps[1].Body; body != "Apply `hello.yaml` in `lab-1`." {
		t.Fatalf("during the attempt: %q", body)
	}

	// Without the lab (the toolchain fails), texts stay as written
	w.state.Invalidate()
	w.lab.stateErr = errors.New("python missing")
	defer func() { w.lab.stateErr = nil }()
	rec = get(t, b, "/api/v1/courses/k8s-intro/items/what-is-a-pod", itemOp)
	if it := decode[httpapi.Item](t, rec.Body.Bytes()); !strings.Contains(it.Lecture.Markdown, "`${node:c}`") {
		t.Fatalf("%s", it.Lecture.Markdown)
	}
}
