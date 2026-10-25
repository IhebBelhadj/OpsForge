package httpapi_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/IhebBelhadj/opsforge/academy/internal/httpapi"
)

const rewardsOp = "/api/v1/badges"

func TestPassingALabAwardsItsBadge(t *testing.T) {
	w := newWorld(t)
	b := signIn(t, apiServer(t, w.deps))
	rec := get(t, b, "/api/v1/badges", rewardsOp)
	got := decode[httpapi.Rewards](t, rec.Body.Bytes())
	if rec.Code != 200 || got.Xp != 0 || len(got.Badges) != 1 || got.Badges[0].Id != "intro-done" ||
		got.Badges[0].AwardedAt != nil || len(got.Badges[0].Courses) != 1 || got.Badges[0].Courses[0] != "k8s-intro" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}

	s := openStream(t, b, "", nil)
	a := startLab(t, w, b, "meet-the-cluster").Attempt
	for _, step := range []string{"nodes", "apply"} {
		rec = post(t, b, attemptPath(a.Id, "/checks"), checkOp, `{"step":"`+step+`"}`)
		finished(t, w, b, decode[httpapi.Operation](t, rec.Body.Bytes()).Id)
	}
	rec = get(t, b, "/api/v1/badges", rewardsOp)
	got = decode[httpapi.Rewards](t, rec.Body.Bytes())
	if got.Xp != 100 || got.Badges[0].AwardedAt == nil {
		t.Fatalf("after the pass: %s", rec.Body)
	}
	var kinds []string
	for len(kinds) == 0 || kinds[len(kinds)-1] != "badge.awarded" {
		e := s.next(t)
		if e.event != "history" {
			continue
		}
		var h httpapi.HistoryEvent
		if err := json.Unmarshal([]byte(e.data), &h); err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, h.Kind)
	}
	// The award follows the pass that earned it (its rule: one lab passed; the course is not complete yet)
	if want := "lab.started step.checked step.checked lab.passed badge.awarded"; strings.Join(kinds, " ") != want {
		t.Fatalf("history = %v", kinds)
	}
	s.check(t)
}
