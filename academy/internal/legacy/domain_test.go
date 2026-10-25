package legacy_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/legacy"
)

var t0 = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

// source is two attempts (one passed with a hint, one abandoned) and their history.
func source() legacy.Source {
	secs := 70.4
	return legacy.Source{
		Attempts: []legacy.Attempt{
			{ID: 1, Lab: "regions-and-latency", Hash: "h1", StartedAt: t0, Status: "passed", HintsUsed: 1, Seconds: &secs, XP: 90},
			{ID: 2, Lab: "private-networks", Hash: "h2", StartedAt: t0, Status: "abandoned"},
		},
		Steps: []legacy.StepResult{{Attempt: 1, Step: "ping", Passed: true, Detail: json.RawMessage(`{}`), At: t0},
			{Attempt: 2, Step: "nat", Passed: false, Detail: json.RawMessage(`{}`), At: t0}},
		Events: []legacy.Event{
			{ID: 10, Kind: "lab.started", Subject: "regions-and-latency", Data: json.RawMessage(`{"attempt": 1}`), At: t0},
			{ID: 11, Kind: "hint.shown", Subject: "regions-and-latency", Data: json.RawMessage(`{"attempt": 1, "step": "ping", "index": 0}`), At: t0},
			{ID: 12, Kind: "lab.passed", Subject: "regions-and-latency", Data: json.RawMessage(`{"attempt": 1, "xp": 90}`), At: t0},
			{ID: 13, Kind: "badge.awarded", Subject: "first-route", Data: json.RawMessage(`{}`), At: t0},
			{ID: 14, Kind: "lab.abandoned", Subject: "private-networks", Data: json.RawMessage(`{"attempt": 2}`), At: t0},
		},
		Awards: []legacy.Award{{Badge: "first-route", At: t0}},
	}
}

func TestAFirstRunPlansEverything(t *testing.T) {
	p, err := legacy.NewPlan(source(), legacy.Done{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Attempts) != 2 || len(p.Steps) != 2 || len(p.Events) != 5 || len(p.Awards) != 1 || len(p.Hints) != 1 {
		t.Fatalf("%+v", p)
	}
	if h := p.Hints[0]; h.Attempt != 1 || h.Step != "ping" || h.Index != 0 {
		t.Fatalf("hint %+v", h)
	}
}

func TestASecondRunPlansOnlyWhatIsNew(t *testing.T) {
	done := legacy.Done{Attempts: map[int64]int64{1: 7}, Events: map[int64]bool{10: true, 11: true, 12: true, 13: true}}
	p, err := legacy.NewPlan(source(), done)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Attempts) != 1 || p.Attempts[0].ID != 2 || len(p.Steps) != 1 || p.Steps[0].Attempt != 2 ||
		len(p.Events) != 1 || p.Events[0].ID != 14 || len(p.Hints) != 0 {
		t.Fatalf("%+v", p)
	}
	all := legacy.Done{Attempts: map[int64]int64{1: 7, 2: 8}, Events: map[int64]bool{10: true, 11: true, 12: true,
		13: true, 14: true}}
	if p, _ := legacy.NewPlan(source(), all); len(p.Attempts)+len(p.Steps)+len(p.Events)+len(p.Hints) != 0 {
		t.Fatalf("everything imported, still planned: %+v", p)
	}
}

func TestAnEventAboutAnUnknownAttemptIsAnError(t *testing.T) {
	src := source()
	src.Events = append(src.Events, legacy.Event{ID: 15, Kind: "lab.started", Data: json.RawMessage(`{"attempt": 9}`)})
	if _, err := legacy.NewPlan(src, legacy.Done{}); err == nil || !strings.Contains(err.Error(), "attempt 9") {
		t.Fatalf("err = %v", err)
	}
}

func TestRemap(t *testing.T) {
	got, err := legacy.Remap(json.RawMessage(`{"attempt": 1, "xp": 90}`), map[int64]int64{1: 7})
	if err != nil || string(got) != `{"attempt":7,"xp":90}` {
		t.Fatalf("%s %v", got, err)
	}
	if got, err := legacy.Remap(json.RawMessage(`{}`), nil); err != nil || string(got) != `{}` {
		t.Fatalf("no attempt: %s %v", got, err)
	}
	if _, err := legacy.Remap(json.RawMessage(`{"attempt": 2}`), map[int64]int64{1: 7}); err == nil {
		t.Fatal("an attempt not imported: no error")
	}
}

func TestSeconds(t *testing.T) {
	half := 70.5
	if s := legacy.Seconds(&half); *s != 71 {
		t.Fatalf("%d", *s)
	}
	if legacy.Seconds(nil) != nil {
		t.Fatal("nil")
	}
}
