package rewards_test

import (
	"encoding/json"
	"testing"

	"github.com/IhebBelhadj/opsforge/academy/internal/rewards"
)

// history is a learner who passed two cloud labs (one with a hint, slowly) and a quiz twice.
func history() rewards.History {
	return rewards.History{
		Labs: []rewards.LabPass{
			{Lab: "regions-and-latency", Hints: 0, Seconds: 300, XP: 100},
			{Lab: "private-networks", Hints: 1, Seconds: 1200, XP: 90},
			{Lab: "private-networks", Hints: 0, Seconds: 600, XP: 100},
		},
		Quizzes: []rewards.QuizPass{{Course: "k8s-intro", Item: "pods-check", XP: 50},
			{Course: "k8s-intro", Item: "pods-check", XP: 0}},
		Courses: map[string]bool{"k8s-intro": true},
		Curricula: map[string][]string{
			"cloud":      {"regions-and-latency", "private-networks"},
			"kubernetes": {"nodes-and-regions", "losing-a-site"},
			"empty":      {},
		},
	}
}

func TestXPIsTheBestOfEachItem(t *testing.T) {
	if got := history().XP(); got != 250 {
		t.Fatalf("XP = %d, want 100 + 100 + 50", got)
	}
	if got := (rewards.History{}).XP(); got != 0 {
		t.Fatalf("empty XP = %d", got)
	}
}

func TestRules(t *testing.T) {
	for rule, want := range map[string]bool{
		`{"type":"labs_passed","min":2}`:                                   true,
		`{"type":"labs_passed","min":3}`:                                   false,
		`{"type":"labs_passed","min":2,"max_hints":0}`:                     true, // the second pass of private-networks
		`{"type":"labs_passed","min":2,"max_seconds":400}`:                 false,
		`{"type":"labs_passed","min":2,"curriculum":"cloud"}`:              true,
		`{"type":"labs_passed","min":1,"curriculum":"kubernetes"}`:         false,
		`{"type":"labs_passed","min":0}`:                                   true,
		`{"type":"lab_passed","lab":"private-networks"}`:                   true,
		`{"type":"lab_passed","lab":"private-networks","max_seconds":500}`: false,
		`{"type":"lab_passed","lab":"private-networks","max_hints":0}`:     true,
		`{"type":"lab_passed","lab":"losing-a-site"}`:                      false,
		`{"type":"curriculum_complete","curriculum":"cloud"}`:              true,
		`{"type":"curriculum_complete","curriculum":"kubernetes"}`:         false,
		`{"type":"curriculum_complete","curriculum":"empty"}`:              false,
		`{"type":"curriculum_complete","curriculum":"retired"}`:            false,
		`{"type":"xp","min":250}`:                                          true,
		`{"type":"xp","min":251}`:                                          false,
	} {
		got, err := rewards.Earned(rewards.Badge{ID: "b", Rule: json.RawMessage(rule)}, history())
		if err != nil || got != want {
			t.Errorf("%s: earned = %v, %v; want %v", rule, got, err, want)
		}
	}
}

func TestACompletedCourseAwardsItsBadge(t *testing.T) {
	b := rewards.Badge{ID: "intro-done", Rule: json.RawMessage(`{"type":"xp","min":10000}`), Courses: []string{"k8s-intro"}}
	if got, err := rewards.Earned(b, history()); err != nil || !got {
		t.Fatalf("earned = %v, %v", got, err)
	}
	b.Courses = []string{"other"}
	if got, _ := rewards.Earned(b, history()); got {
		t.Fatal("earned by a course that is not completed")
	}
}

func TestUnknownOrBrokenRules(t *testing.T) {
	for _, rule := range []string{`{"type":"streak_days","min":3}`, `{"type":"xp","min":"many"}`, `[]`} {
		if _, err := rewards.Earned(rewards.Badge{ID: "b", Rule: json.RawMessage(rule)}, history()); err == nil {
			t.Errorf("%s: no error", rule)
		}
	}
}
