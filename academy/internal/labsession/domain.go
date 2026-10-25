// Package labsession runs lab attempts (docs/courses/architecture.md, "Platform service: modules"): start runs
// the lab's setup, each step is checked in order (a probe observes the lab through the toolchain, a quiz or a
// confirm is answered), hints cost XP, reset sets the lab up again, and finishing or abandoning always runs the
// teardown and heals the links. One attempt runs per user, and one lab operation at a time on the one lab (they
// share its links and cluster). Phase 10's rules (its Python server, since removed), on the toolchain contract.
package labsession

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
	"github.com/IhebBelhadj/opsforge/academy/internal/lab"
)

// The events of an attempt (subject: the lab id; data: the attempt id and details).
const (
	KindStarted     = "lab.started"
	KindStepChecked = "step.checked"
	KindHintShown   = "hint.shown"
	KindReset       = "lab.reset"
	KindPassed      = "lab.passed"
	KindAbandoned   = "lab.abandoned"
	KindFailed      = "lab.failed" // its setup failed
)

// The operations an attempt runs (operations.Operation.Kind).
const (
	OpStart   = "lab.start"
	OpCheck   = "lab.check"
	OpReset   = "lab.reset"
	OpAbandon = "lab.abandon"
)

// DefaultHintPenalty is a pack's hint penalty when it sets none (labs.DEFAULT_HINT_PENALTY).
const DefaultHintPenalty = 0.1

var (
	// ErrLabNotFound is a lab that is not in the catalogue (or retired).
	ErrLabNotFound = errors.New("no such lab")
	// ErrAttemptNotFound is an attempt that does not exist or is another user's.
	ErrAttemptNotFound = errors.New("no such attempt")
	// ErrAnotherLabRunning is starting a lab while one is running: finish or abandon it first.
	ErrAnotherLabRunning = errors.New("another lab is running")
	// ErrBusy is a lab operation while another one runs on the lab.
	ErrBusy = errors.New("the lab is busy with another operation")
	// ErrLocked is a lab whose required labs are not passed yet (*LockedError says which).
	ErrLocked = errors.New("the lab is locked")
	// ErrLabDown is starting a lab while the lab environment is down.
	ErrLabDown = errors.New("the lab is down")
	// ErrUnavailable is a lab that needs another layout than the running one.
	ErrUnavailable = errors.New("the lab cannot run on this layout")
	// ErrToolchain is a toolchain call that failed before anything started.
	ErrToolchain = errors.New("the lab toolchain failed")
	// ErrNotRunning is acting on an attempt that is not running (still setting up, or finished).
	ErrNotRunning = errors.New("the attempt is not running")
	// ErrStepOrder is checking a step other than the first one not passed.
	ErrStepOrder = errors.New("check the steps in order")
	// ErrNoSuchStep is a step the lab does not have.
	ErrNoSuchStep = errors.New("no such step")
	// ErrAnswerRequired is checking a quiz step without an answer.
	ErrAnswerRequired = errors.New("a quiz step needs an answer")
	// ErrNoMoreHints is asking for a hint when every hint of the step is shown.
	ErrNoMoreHints = errors.New("no more hints for this step")
	// ErrResetUnsupported is resetting a lab that resets from a snapshot (not in the toolchain yet).
	ErrResetUnsupported = errors.New("this lab resets from a snapshot, which is not supported yet")
)

// LockedError is ErrLocked with the labs to pass first.
type LockedError struct{ Missing []string }

func (e *LockedError) Error() string {
	return fmt.Sprintf("%v: pass %s first", ErrLocked, strings.Join(e.Missing, ", "))
}

// Is makes errors.Is(err, ErrLocked) true.
func (e *LockedError) Is(target error) bool { return target == ErrLocked }

// Check types.
const (
	CheckProbe   = "probe"
	CheckQuiz    = "quiz"
	CheckConfirm = "confirm"
)

// Definition is a lab as imported, with what views leave out: setup, teardown, probes, answers, hint texts.
type Definition struct {
	ID          string
	Hash        string
	XP          int
	HintPenalty float64
	Layout      *string
	Requires    []string
	Namespace   bool   // setup creates lab-<attempt>
	Reset       string // setup (default) or snapshot
	Files       map[string]string
	Setup       []json.RawMessage
	Teardown    []json.RawMessage
	Steps       []Step
}

// Step is one step with its check and hints.
type Step struct {
	ID    string
	Check Check
	Hints []string
}

// Check is how a step passes: Probe for a probe, Answer/Explain for a quiz, nothing for a confirm.
type Check struct {
	Type    string
	Probe   *lab.ProbeRequest
	Answer  int
	Explain string
}

// Decode reads a lab entry and its pack's entry (the hint penalty).
func Decode(labEntry, pack catalog.Entry) (Definition, error) {
	var doc struct {
		XP       int
		Layout   *string
		Requires []string
		Files    map[string]string
		Body     struct {
			Namespace, Reset string
			Setup, Teardown  []json.RawMessage
			Steps            []struct {
				ID    string
				Hints []string
				Check map[string]json.RawMessage
			}
		}
	}
	if err := json.Unmarshal(labEntry.Doc, &doc); err != nil {
		return Definition{}, fmt.Errorf("lab %s: %w", labEntry.Key, err)
	}
	var p struct {
		HintPenalty *float64 `json:"hint_penalty"`
	}
	if err := json.Unmarshal(pack.Doc, &p); err != nil {
		return Definition{}, fmt.Errorf("pack %s: %w", pack.Key, err)
	}
	d := Definition{ID: labEntry.Key, Hash: labEntry.Hash, XP: doc.XP, HintPenalty: DefaultHintPenalty,
		Layout: doc.Layout, Requires: doc.Requires, Namespace: doc.Body.Namespace == "auto", Reset: doc.Body.Reset,
		Files: doc.Files, Setup: doc.Body.Setup, Teardown: doc.Body.Teardown}
	if p.HintPenalty != nil {
		d.HintPenalty = *p.HintPenalty
	}
	if d.Reset == "" {
		d.Reset = "setup"
	}
	for _, s := range doc.Body.Steps {
		c, err := decodeCheck(s.Check)
		if err != nil {
			return Definition{}, fmt.Errorf("lab %s step %s: %w", labEntry.Key, s.ID, err)
		}
		d.Steps = append(d.Steps, Step{ID: s.ID, Check: c, Hints: s.Hints})
	}
	return d, nil
}

func decodeCheck(check map[string]json.RawMessage) (Check, error) {
	for kind, arg := range check {
		c := Check{Type: kind}
		switch kind {
		case CheckProbe:
			var p lab.ProbeRequest
			if err := json.Unmarshal(arg, &p); err != nil {
				return c, err
			}
			c.Probe = &p
		case CheckQuiz:
			var q struct {
				Answer  int
				Explain string
			}
			if err := json.Unmarshal(arg, &q); err != nil {
				return c, err
			}
			c.Answer, c.Explain = q.Answer, q.Explain
		case CheckConfirm:
		default:
			return c, fmt.Errorf("unknown check %q", kind)
		}
		return c, nil
	}
	return Check{}, errors.New("a step without a check")
}

// Step is the step with this id.
func (d Definition) Step(id string) (Step, bool) {
	for _, s := range d.Steps {
		if s.ID == id {
			return s, true
		}
	}
	return Step{}, false
}

// Current is the first step not passed yet, and whether it is the last one; false when every step is passed.
func (d Definition) Current(passed map[string]bool) (Step, bool, bool) {
	for i, s := range d.Steps {
		if !passed[s.ID] {
			return s, i == len(d.Steps)-1, true
		}
	}
	return Step{}, false, false
}

// PassXP is what a pass is worth: the lab's xp less penalty per hint used, never below half, rounded half to even
// (Phase 10's labs.pass_xp, so migrated passes keep their XP).
func PassXP(xp, hints int, penalty float64) int {
	return int(math.RoundToEven(float64(xp) * math.Max(0.5, 1-penalty*float64(hints))))
}

// Status is where an attempt is.
type Status string

// The statuses of an attempt.
const (
	Setup     Status = "setup" // its setup is running
	Running   Status = "running"
	Passed    Status = "passed"
	Abandoned Status = "abandoned"
	Failed    Status = "failed" // its setup failed
)

// Active reports whether the attempt still holds the user's one running lab.
func (s Status) Active() bool { return s == Setup || s == Running }

// Attempt is one try at a lab. Namespace is "" for a lab without its own; Course and Item say where it was started
// from ("" from outside a course). Seconds and XP are set when it passes; Error says why its setup failed.
type Attempt struct {
	ID         int64
	UserID     int64
	Lab        string
	Course     string
	Item       string
	Hash       string
	Namespace  string
	Workspace  string
	Status     Status
	StartedAt  time.Time
	FinishedAt *time.Time
	HintsUsed  int
	Seconds    int
	XP         int
	Error      string
}

// StepState is a step as the attempt stands: passed, and the hints shown so far.
type StepState struct {
	ID     string
	Passed bool
	Hints  []string
}

// View is an attempt with its steps' states; Current is the step to check next ("" when none).
type View struct {
	Attempt
	Steps   []StepState
	Current string
}

// Outcome is what a lab operation did (the result of its operations.Operation; its JSON is the API's LabOutcome).
// Status is the attempt's status after it; Step, Passed and Results belong to a check; XP and Seconds to a pass;
// Error is a step that failed (a setup, a probe that could not run, a teardown).
type Outcome struct {
	Attempt int64             `json:"attempt"`
	Status  Status            `json:"status"`
	Step    *string           `json:"step"`
	Passed  *bool             `json:"passed"`
	Explain *string           `json:"explain"`
	Results []lab.Expectation `json:"results"`
	XP      *int              `json:"xp"`
	Seconds *int              `json:"seconds"`
	Error   *string           `json:"error"`
}

// HintShown is a hint the learner opened; HintsUsed is the attempt's count after it, Namespace the attempt's
// (its "${ns}").
type HintShown struct {
	Namespace string
	Step      string
	Index     int
	Text      string
	HintsUsed int
}
