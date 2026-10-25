// Package legacy imports Phase 10's teaching-lab database (.opsforge/labs.db) into the platform's, once
// (docs/courses/content-model.md, "Legacy import"): attempts with their step results, the history, hints (from
// hint.shown events) and badge awards, all for the local user. Phase 10's content hashes are kept, so a lab that
// did not change since still shows as passed and fresh. Rows already imported are remembered by their Phase 10
// id, so running it again imports only what Phase 10 recorded since. A one-off data migration: like a goose
// migration, its adapter writes several modules' tables in one transaction.
package legacy

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
)

// Attempt is a Phase 10 attempt (status running, passed or abandoned; seconds fractional).
type Attempt struct {
	ID         int64
	Lab        string
	Hash       string
	StartedAt  time.Time
	FinishedAt *time.Time
	Status     string
	HintsUsed  int
	Seconds    *float64
	XP         int
}

// StepResult is one check of a step of an attempt.
type StepResult struct {
	Attempt int64
	Step    string
	Passed  bool
	Detail  json.RawMessage
	At      time.Time
}

// Event is one event of the history; Data refers to an attempt by its Phase 10 id ("attempt").
type Event struct {
	ID      int64
	Kind    string
	Subject string
	Data    json.RawMessage
	At      time.Time
}

// Award is a badge awarded.
type Award struct {
	Badge string
	At    time.Time
}

// Hint is a hint shown during an attempt (from a hint.shown event).
type Hint struct {
	Attempt int64
	Step    string
	Index   int
	At      time.Time
}

// Source is everything labs.db holds (Phase 10 had one learner; every row becomes the local user's).
type Source struct {
	Attempts []Attempt
	Steps    []StepResult
	Events   []Event
	Awards   []Award
}

// Done is what an earlier run imported: Phase 10 attempt id → the platform's, and the Phase 10 event ids.
type Done struct {
	Attempts map[int64]int64
	Events   map[int64]bool
}

// Plan is what a run writes: the rows not imported yet. Steps, hints and events still refer to attempts by their
// Phase 10 ids (the store maps them as it inserts the attempts).
type Plan struct {
	Attempts []Attempt
	Steps    []StepResult
	Hints    []Hint
	Events   []Event
	Awards   []Award
}

// Report counts what a run imported.
type Report struct {
	Attempts, Steps, Hints, Events, Awards int
}

// NewPlan is what of src is not in done yet. An event about an attempt that is neither imported nor in src is an
// error (labs.db is inconsistent).
func NewPlan(src Source, done Done) (Plan, error) {
	var p Plan
	known := map[int64]bool{}
	for id := range done.Attempts {
		known[id] = true
	}
	planned := map[int64]bool{}
	for _, a := range src.Attempts {
		if !known[a.ID] {
			p.Attempts = append(p.Attempts, a)
			planned[a.ID], known[a.ID] = true, true
		}
	}
	for _, s := range src.Steps {
		if planned[s.Attempt] {
			p.Steps = append(p.Steps, s)
		}
	}
	for _, e := range src.Events {
		if done.Events[e.ID] {
			continue
		}
		ref, err := AttemptOf(e.Data)
		if err != nil {
			return Plan{}, fmt.Errorf("event %d: %w", e.ID, err)
		}
		if ref != 0 && !known[ref] {
			return Plan{}, fmt.Errorf("event %d (%s) refers to attempt %d, which labs.db does not have", e.ID, e.Kind, ref)
		}
		p.Events = append(p.Events, e)
		if e.Kind == "hint.shown" && planned[ref] {
			var h struct {
				Step  string
				Index int
			}
			if err := json.Unmarshal(e.Data, &h); err != nil {
				return Plan{}, fmt.Errorf("event %d: %w", e.ID, err)
			}
			p.Hints = append(p.Hints, Hint{Attempt: ref, Step: h.Step, Index: h.Index, At: e.At})
		}
	}
	p.Awards = src.Awards
	return p, nil
}

// AttemptOf is the attempt an event's data refers to (0: none).
func AttemptOf(data json.RawMessage) (int64, error) {
	var d struct {
		Attempt *int64 `json:"attempt"`
	}
	if len(data) == 0 {
		return 0, nil
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return 0, err
	}
	if d.Attempt == nil {
		return 0, nil
	}
	return *d.Attempt, nil
}

// Remap is an event's data referring to the platform's attempt id instead of Phase 10's.
func Remap(data json.RawMessage, ids map[int64]int64) (json.RawMessage, error) {
	ref, err := AttemptOf(data)
	if err != nil || ref == 0 {
		return data, err
	}
	id, ok := ids[ref]
	if !ok {
		return nil, fmt.Errorf("attempt %d was not imported", ref)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	m["attempt"] = json.RawMessage(fmt.Sprint(id))
	return json.Marshal(m)
}

// Seconds is an attempt's time in whole seconds, as the platform keeps it (nil: not finished).
func Seconds(s *float64) *int {
	if s == nil {
		return nil
	}
	v := int(math.Round(*s))
	return &v
}
