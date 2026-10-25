// Package rewards awards XP and badges (docs/courses/content-model.md, "Rewards"). XP is not stored: it is the best
// pass of each lab plus the best attempt of each quiz, read from their modules. A badge is earned when its rule
// holds over the learner's history, or when a course that names it as its badge is completed; every rule is
// evaluated against the whole history, so a badge added later is awarded for what was done before (Phase 10's
// award_badges). Awards are kept even when the badge is retired.
package rewards

import (
	"encoding/json"
	"fmt"
	"time"
)

// KindAwarded is the event of an award (subject: the badge id).
const KindAwarded = "badge.awarded"

// LabPass is one passed lab attempt.
type LabPass struct {
	Lab     string
	Hints   int
	Seconds int
	XP      int
}

// QuizPass is one passed quiz attempt (its XP is 0 after the first pass).
type QuizPass struct {
	Course, Item string
	XP           int
}

// History is what the rules read: the learner's passes, the courses completed, and the pack's Phase 10 curricula
// (curriculum id → its labs) until M2 turns them into courses.
type History struct {
	Labs      []LabPass
	Quizzes   []QuizPass
	Courses   map[string]bool
	Curricula map[string][]string
}

// XP is the best XP of each lab plus the best XP of each quiz.
func (h History) XP() int {
	best := map[string]int{}
	for _, p := range h.Labs {
		best["lab:"+p.Lab] = max(best["lab:"+p.Lab], p.XP)
	}
	for _, p := range h.Quizzes {
		k := "quiz:" + p.Course + "/" + p.Item
		best[k] = max(best[k], p.XP)
	}
	total := 0
	for _, xp := range best {
		total += xp
	}
	return total
}

// Badge is a badge as imported. Rule is its badges.yml rule; Courses are the courses that award it on completion.
type Badge struct {
	ID, Pack, Title, Description string
	Icon                         *string
	Position                     int
	Rule                         json.RawMessage
	Courses                      []string
	Retired                      bool
}

// Rule decides from a history whether a badge is earned.
type Rule interface {
	Earned(h History) bool
}

// Rules are the rule types by name (lab/labs.py RULES, which validates them at compile): each decodes its own
// parameters. A new type is a new entry here and in labs.RULES.
var Rules = map[string]func(json.RawMessage) (Rule, error){
	"labs_passed":         decode[labsPassed],
	"lab_passed":          decode[labPassed],
	"curriculum_complete": decode[curriculumComplete],
	"xp":                  decode[xpRule],
}

func decode[R Rule](raw json.RawMessage) (Rule, error) {
	var r R
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	return r, nil
}

// ParseRule reads a badge's rule; an unknown type is an error (the compiler rejects it, so it means a newer pack).
func ParseRule(raw json.RawMessage) (Rule, error) {
	var head struct{ Type string }
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, fmt.Errorf("rule: %w", err)
	}
	parse, ok := Rules[head.Type]
	if !ok {
		return nil, fmt.Errorf("unknown rule type %q", head.Type)
	}
	r, err := parse(raw)
	if err != nil {
		return nil, fmt.Errorf("rule %s: %w", head.Type, err)
	}
	return r, nil
}

// Earned reports whether b is earned: one of its courses is completed, or its rule holds.
func Earned(b Badge, h History) (bool, error) {
	for _, c := range b.Courses {
		if h.Courses[c] {
			return true, nil
		}
	}
	r, err := ParseRule(b.Rule)
	if err != nil {
		return false, err
	}
	return r.Earned(h), nil
}

// limits are the optional bounds of a pass that counts (max_hints, max_seconds).
type limits struct {
	MaxHints   *int `json:"max_hints"`
	MaxSeconds *int `json:"max_seconds"`
}

func (l limits) counts(p LabPass) bool {
	return (l.MaxHints == nil || p.Hints <= *l.MaxHints) && (l.MaxSeconds == nil || p.Seconds <= *l.MaxSeconds)
}

// labsPassed: at least Min distinct labs passed within the limits, of one curriculum when it is set.
type labsPassed struct {
	limits
	Min        int
	Curriculum *string
}

func (r labsPassed) Earned(h History) bool {
	var in map[string]bool
	if r.Curriculum != nil {
		in = map[string]bool{}
		for _, l := range h.Curricula[*r.Curriculum] {
			in[l] = true
		}
	}
	labs := map[string]bool{}
	for _, p := range h.Labs {
		if (in == nil || in[p.Lab]) && r.counts(p) {
			labs[p.Lab] = true
		}
	}
	return len(labs) >= r.Min
}

// labPassed: one lab passed within the limits.
type labPassed struct {
	limits
	Lab string
}

func (r labPassed) Earned(h History) bool {
	for _, p := range h.Labs {
		if p.Lab == r.Lab && r.counts(p) {
			return true
		}
	}
	return false
}

// curriculumComplete: every lab of a (non-empty) curriculum passed.
type curriculumComplete struct {
	Curriculum string
}

func (r curriculumComplete) Earned(h History) bool {
	labs := h.Curricula[r.Curriculum]
	if len(labs) == 0 {
		return false
	}
	passed := map[string]bool{}
	for _, p := range h.Labs {
		passed[p.Lab] = true
	}
	for _, l := range labs {
		if !passed[l] {
			return false
		}
	}
	return true
}

// xpRule: at least Min XP.
type xpRule struct {
	Min int
}

func (r xpRule) Earned(h History) bool { return h.XP() >= r.Min }

// Award is a badge with when the learner earned it (nil: not yet).
type Award struct {
	Badge
	AwardedAt *time.Time
}

// Summary is a learner's XP and badges: every live badge, plus retired ones already awarded, in pack order.
type Summary struct {
	XP     int
	Badges []Award
}
