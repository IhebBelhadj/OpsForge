package labsession

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
	"github.com/IhebBelhadj/opsforge/academy/internal/lab"
	"github.com/IhebBelhadj/opsforge/academy/internal/operations"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

// Service runs lab attempts. busy is the one lab operation at a time: taken before an operation starts (a second
// one is refused, never queued) and released when its background work ends.
type Service struct {
	repo    Repo
	catalog Catalog
	lab     LabController
	spaces  Workspaces
	ops     Operations
	events  Publisher
	clock   platform.Clock
	busy    sync.Mutex
}

// NewService runs labs through controller.
func NewService(repo Repo, cat Catalog, controller LabController, spaces Workspaces, ops Operations,
	events Publisher, clock platform.Clock) *Service {
	return &Service{repo: repo, catalog: cat, lab: controller, spaces: spaces, ops: ops, events: events, clock: clock}
}

// definition is a live lab with its pack's hint penalty.
func (s *Service) definition(ctx context.Context, labID string) (Definition, error) {
	e, err := s.catalog.Entry(ctx, catalog.KindLab, labID)
	if errors.Is(err, catalog.ErrNotFound) {
		return Definition{}, ErrLabNotFound
	}
	if err != nil {
		return Definition{}, err
	}
	pack, err := s.catalog.Entry(ctx, catalog.KindPack, e.PackID)
	if err != nil {
		return Definition{}, fmt.Errorf("lab %s: its pack: %w", labID, err)
	}
	return Decode(e, pack)
}

// take reserves the lab for one operation; the returned release is safe to call more than once.
func (s *Service) take() (func(), error) {
	if !s.busy.TryLock() {
		return nil, ErrBusy
	}
	return sync.OnceFunc(s.busy.Unlock), nil
}

func (s *Service) publish(ctx context.Context, a Attempt, kind string, data map[string]any) error {
	if data == nil {
		data = map[string]any{}
	}
	data["attempt"] = a.ID
	doc, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = s.events.Publish(ctx, platform.Event{UserID: a.UserID, Kind: kind, Subject: a.Lab, Data: doc,
		At: s.clock.Now()})
	return err
}

// Start begins an attempt at a lab (from course/item, or "" for none) and runs its setup in the background. It
// is refused while another attempt is active, the lab is busy or down, the lab needs another layout or its
// required labs are not passed.
func (s *Service) Start(ctx context.Context, user int64, labID, course, item string) (Attempt, operations.Operation, error) {
	def, err := s.definition(ctx, labID)
	if err != nil {
		return Attempt{}, operations.Operation{}, err
	}
	_, active, err := s.repo.Active(ctx, user)
	if err != nil {
		return Attempt{}, operations.Operation{}, err
	}
	if active {
		return Attempt{}, operations.Operation{}, ErrAnotherLabRunning
	}
	if err := s.unlocked(ctx, user, def); err != nil {
		return Attempt{}, operations.Operation{}, err
	}
	release, err := s.take()
	if err != nil {
		return Attempt{}, operations.Operation{}, err
	}
	handedOver := false // to the setup, which releases the lab when it ends
	defer func() {
		if !handedOver {
			release()
		}
	}()
	state, err := s.lab.State(ctx)
	if err != nil {
		return Attempt{}, operations.Operation{}, fmt.Errorf("%w: %w", ErrToolchain, err)
	}
	if !state.Up {
		return Attempt{}, operations.Operation{}, ErrLabDown
	}
	if def.Layout != nil && (state.Layout == nil || *state.Layout != *def.Layout) {
		return Attempt{}, operations.Operation{}, ErrUnavailable
	}

	a := Attempt{UserID: user, Lab: labID, Course: course, Item: item, Hash: def.Hash, Status: Setup,
		StartedAt: s.clock.Now()}
	if a.ID, err = s.repo.Create(ctx, a); err != nil {
		return Attempt{}, operations.Operation{}, err
	}
	a.Workspace = fmt.Sprintf("lab-%d", a.ID)
	if def.Namespace {
		a.Namespace = a.Workspace
	}
	if err := s.repo.SetPlaces(ctx, a.ID, a.Namespace, a.Workspace); err != nil {
		return Attempt{}, operations.Operation{}, err
	}
	if err := s.spaces.Create(ctx, user, a.Workspace, def.Files); err != nil {
		s.fail(ctx, &a, "workspace: "+err.Error())
		return Attempt{}, operations.Operation{}, fmt.Errorf("workspace: %w", err)
	}
	if err := s.publish(ctx, a, KindStarted, map[string]any{"content_hash": a.Hash}); err != nil {
		return Attempt{}, operations.Operation{}, err
	}
	op, err := s.ops.Start(ctx, user, OpStart, labID, func(ctx context.Context) (any, error) {
		defer release()
		return s.setUp(ctx, a, def)
	})
	if err != nil {
		s.fail(ctx, &a, err.Error())
		return Attempt{}, operations.Operation{}, err
	}
	handedOver = true
	return a, op, nil
}

// unlocked checks that the user passed every lab def requires.
func (s *Service) unlocked(ctx context.Context, user int64, def Definition) error {
	if len(def.Requires) == 0 {
		return nil
	}
	passed, err := s.repo.Passed(ctx, user)
	if err != nil {
		return err
	}
	done := map[string]bool{}
	for _, p := range passed {
		done[p.Lab] = true
	}
	var missing []string
	for _, r := range def.Requires {
		if !done[r] {
			missing = append(missing, r)
		}
	}
	if len(missing) > 0 {
		return &LockedError{Missing: missing}
	}
	return nil
}

// setUp runs the setup; when it fails, the teardown cleans up what it did and the attempt fails.
func (s *Service) setUp(ctx context.Context, a Attempt, def Definition) (Outcome, error) {
	if msg := s.run(ctx, lab.Setup, def.Setup, a.Namespace); msg != "" {
		if teardown := s.tearDown(ctx, a, def); teardown != "" {
			msg += "\nteardown: " + teardown
		}
		s.fail(ctx, &a, msg)
		return Outcome{Attempt: a.ID, Status: a.Status, Error: &msg, Results: []lab.Expectation{}}, nil
	}
	a.Status = Running
	if err := s.repo.Finish(ctx, a); err != nil {
		return Outcome{}, err
	}
	return Outcome{Attempt: a.ID, Status: a.Status, Results: []lab.Expectation{}}, nil
}

// run runs steps of a phase in the attempt's namespace; "" when they all ran, else why not. Nothing to run and no
// namespace to create or delete is no toolchain call.
func (s *Service) run(ctx context.Context, phase lab.Phase, steps []json.RawMessage, namespace string) string {
	if len(steps) == 0 && namespace == "" {
		return ""
	}
	if steps == nil {
		steps = []json.RawMessage{}
	}
	res, err := s.lab.Run(ctx, lab.RunRequest{Phase: phase, Steps: steps, Namespace: namespace})
	if err != nil {
		return err.Error()
	}
	if res.Error != "" {
		return res.Error
	}
	return ""
}

// tearDown runs the teardown (which deletes the namespace) and heals the links; "" when both went well.
func (s *Service) tearDown(ctx context.Context, a Attempt, def Definition) string {
	msg := s.run(ctx, lab.Teardown, def.Teardown, a.Namespace)
	if _, err := s.lab.Heal(ctx); err != nil {
		msg = joinLines(msg, "heal: "+err.Error())
	}
	return msg
}

func joinLines(a, b string) string {
	if a == "" {
		return b
	}
	return a + "\n" + b
}

// fail records a failed setup (best effort: the attempt must not stay active).
func (s *Service) fail(ctx context.Context, a *Attempt, msg string) {
	now := s.clock.Now()
	a.Status, a.FinishedAt, a.Error = Failed, &now, msg
	if s.repo.Finish(ctx, *a) == nil {
		_ = s.publish(ctx, *a, KindFailed, map[string]any{"error": msg})
	}
}

// running is user's attempt id, running, with its lab.
func (s *Service) running(ctx context.Context, user, id int64) (Attempt, Definition, error) {
	a, err := s.repo.Attempt(ctx, id)
	if err != nil {
		return Attempt{}, Definition{}, err
	}
	if a.UserID != user {
		return Attempt{}, Definition{}, ErrAttemptNotFound
	}
	if a.Status != Running {
		return Attempt{}, Definition{}, ErrNotRunning
	}
	def, err := s.definition(ctx, a.Lab)
	return a, def, err
}

// Check checks a step of a running attempt: the first step not passed yet. A probe runs on the lab; a quiz takes
// answer (a choice index). Passing the last step passes the attempt: the teardown runs and the XP is earned.
func (s *Service) Check(ctx context.Context, user, id int64, stepID string, answer *int) (operations.Operation, error) {
	a, def, err := s.running(ctx, user, id)
	if err != nil {
		return operations.Operation{}, err
	}
	if _, ok := def.Step(stepID); !ok {
		return operations.Operation{}, ErrNoSuchStep
	}
	passed, err := s.repo.PassedSteps(ctx, a.ID)
	if err != nil {
		return operations.Operation{}, err
	}
	step, last, ok := def.Current(passed)
	if !ok || step.ID != stepID {
		return operations.Operation{}, ErrStepOrder
	}
	if step.Check.Type == CheckQuiz && answer == nil {
		return operations.Operation{}, ErrAnswerRequired
	}
	release, err := s.take()
	if err != nil {
		return operations.Operation{}, err
	}
	op, err := s.ops.Start(ctx, user, OpCheck, a.Lab, func(ctx context.Context) (any, error) {
		defer release()
		return s.check(ctx, a, def, step, last, answer)
	})
	if err != nil {
		release()
	}
	return op, err
}

func (s *Service) check(ctx context.Context, a Attempt, def Definition, step Step, last bool, answer *int) (Outcome, error) {
	out := Outcome{Attempt: a.ID, Status: Running, Step: &step.ID, Results: []lab.Expectation{}}
	ok := false
	detail := map[string]any{}
	switch step.Check.Type {
	case CheckConfirm:
		ok = true
	case CheckQuiz:
		ok = *answer == step.Check.Answer
		detail["answer"] = *answer
		if ok && step.Check.Explain != "" {
			out.Explain = &step.Check.Explain
		}
	case CheckProbe:
		req := *step.Check.Probe
		req.Namespace = a.Namespace
		res, err := s.lab.Probe(ctx, req)
		if err != nil {
			msg := err.Error()
			out.Error = &msg
		} else {
			ok = res.Passed
			if res.Results != nil {
				out.Results = res.Results
			}
			if res.Error != "" {
				out.Error = &res.Error
			}
		}
		detail["results"], detail["error"] = out.Results, out.Error
	}
	out.Passed = &ok
	detail["passed"] = ok
	doc, err := json.Marshal(detail)
	if err != nil {
		return Outcome{}, err
	}
	if err := s.repo.RecordStep(ctx, a.ID, step.ID, ok, doc, s.clock.Now()); err != nil {
		return Outcome{}, err
	}
	if err := s.publish(ctx, a, KindStepChecked, map[string]any{"step": step.ID, "passed": ok}); err != nil {
		return Outcome{}, err
	}
	if !ok || !last {
		return out, nil
	}
	return s.pass(ctx, a.ID, def, out)
}

// pass finishes an attempt whose steps all passed: teardown and heal, then its time and XP (hints read now).
func (s *Service) pass(ctx context.Context, id int64, def Definition, out Outcome) (Outcome, error) {
	a, err := s.repo.Attempt(ctx, id)
	if err != nil {
		return Outcome{}, err
	}
	if msg := s.tearDown(ctx, a, def); msg != "" {
		out.Error = &msg
	}
	now := s.clock.Now()
	a.Status, a.FinishedAt = Passed, &now
	a.Seconds = int(now.Sub(a.StartedAt).Seconds())
	a.XP = PassXP(def.XP, a.HintsUsed, def.HintPenalty)
	if err := s.repo.Finish(ctx, a); err != nil {
		return Outcome{}, err
	}
	if err := s.publish(ctx, a, KindPassed, map[string]any{"xp": a.XP, "hints": a.HintsUsed,
		"seconds": a.Seconds, "content_hash": a.Hash}); err != nil {
		return Outcome{}, err
	}
	out.Status, out.XP, out.Seconds = Passed, &a.XP, &a.Seconds
	return out, nil
}

// Hint opens the next hint of a step of a running attempt. Each hint counts against the pass once.
func (s *Service) Hint(ctx context.Context, user, id int64, stepID string) (HintShown, error) {
	a, def, err := s.running(ctx, user, id)
	if err != nil {
		return HintShown{}, err
	}
	step, ok := def.Step(stepID)
	if !ok {
		return HintShown{}, ErrNoSuchStep
	}
	shown, err := s.repo.Hints(ctx, a.ID)
	if err != nil {
		return HintShown{}, err
	}
	idx := shown[stepID]
	if idx >= len(step.Hints) {
		return HintShown{}, ErrNoMoreHints
	}
	if err := s.repo.ShowHint(ctx, a.ID, stepID, idx, s.clock.Now()); err != nil {
		return HintShown{}, err
	}
	if err := s.publish(ctx, a, KindHintShown, map[string]any{"step": stepID, "index": idx}); err != nil {
		return HintShown{}, err
	}
	if a, err = s.repo.Attempt(ctx, a.ID); err != nil {
		return HintShown{}, err
	}
	return HintShown{Step: stepID, Index: idx, Text: step.Hints[idx], HintsUsed: a.HintsUsed}, nil
}

// Reset tears a running attempt's lab down and sets it up again; its steps start over (hints stay counted).
func (s *Service) Reset(ctx context.Context, user, id int64) (operations.Operation, error) {
	a, def, err := s.running(ctx, user, id)
	if err != nil {
		return operations.Operation{}, err
	}
	if def.Reset != "setup" {
		return operations.Operation{}, ErrResetUnsupported
	}
	release, err := s.take()
	if err != nil {
		return operations.Operation{}, err
	}
	op, err := s.ops.Start(ctx, user, OpReset, a.Lab, func(ctx context.Context) (any, error) {
		defer release()
		teardown := s.tearDown(ctx, a, def)
		if err := s.repo.ClearSteps(ctx, a.ID); err != nil {
			return nil, err
		}
		if err := s.publish(ctx, a, KindReset, nil); err != nil {
			return nil, err
		}
		out, err := s.setUp(ctx, a, def)
		if err == nil && teardown != "" && out.Error == nil {
			msg := "teardown: " + teardown
			out.Error = &msg
		}
		return out, err
	})
	if err != nil {
		release()
	}
	return op, err
}

// Abandon ends a running attempt: teardown and heal, nothing earned.
func (s *Service) Abandon(ctx context.Context, user, id int64) (operations.Operation, error) {
	a, def, err := s.running(ctx, user, id)
	if err != nil {
		return operations.Operation{}, err
	}
	release, err := s.take()
	if err != nil {
		return operations.Operation{}, err
	}
	op, err := s.ops.Start(ctx, user, OpAbandon, a.Lab, func(ctx context.Context) (any, error) {
		defer release()
		out := Outcome{Attempt: a.ID, Status: Abandoned, Results: []lab.Expectation{}}
		if msg := s.tearDown(ctx, a, def); msg != "" {
			out.Error = &msg
		}
		now := s.clock.Now()
		a.Status, a.FinishedAt = Abandoned, &now
		if err := s.repo.Finish(ctx, a); err != nil {
			return nil, err
		}
		return out, s.publish(ctx, a, KindAbandoned, nil)
	})
	if err != nil {
		release()
	}
	return op, err
}

// View is one of user's attempts with its steps (hint texts as shown so far).
func (s *Service) View(ctx context.Context, user, id int64) (View, error) {
	a, err := s.repo.Attempt(ctx, id)
	if err != nil {
		return View{}, err
	}
	if a.UserID != user {
		return View{}, ErrAttemptNotFound
	}
	return s.view(ctx, a)
}

// Current is user's active attempt, if any.
func (s *Service) Current(ctx context.Context, user int64) (View, bool, error) {
	a, ok, err := s.repo.Active(ctx, user)
	if err != nil || !ok {
		return View{}, false, err
	}
	v, err := s.view(ctx, a)
	return v, err == nil, err
}

func (s *Service) view(ctx context.Context, a Attempt) (View, error) {
	v := View{Attempt: a, Steps: []StepState{}}
	def, err := s.definition(ctx, a.Lab)
	if errors.Is(err, ErrLabNotFound) { // retired since: the attempt is still the learner's
		return v, nil
	}
	if err != nil {
		return View{}, err
	}
	passed, err := s.repo.PassedSteps(ctx, a.ID)
	if err != nil {
		return View{}, err
	}
	shown, err := s.repo.Hints(ctx, a.ID)
	if err != nil {
		return View{}, err
	}
	for _, step := range def.Steps {
		st := StepState{ID: step.ID, Passed: passed[step.ID], Hints: []string{}}
		for i := 0; i < shown[step.ID] && i < len(step.Hints); i++ {
			st.Hints = append(st.Hints, step.Hints[i])
		}
		v.Steps = append(v.Steps, st)
	}
	if a.Status == Running {
		if step, _, ok := def.Current(passed); ok {
			v.Current = step.ID
		}
	}
	return v, nil
}

// Passed are user's passed attempts, oldest first (their completions and XP).
func (s *Service) Passed(ctx context.Context, user int64) ([]Attempt, error) {
	return s.repo.Passed(ctx, user)
}

// Recover makes the attempts a stopped process left in setup running, so they can be abandoned (their operation
// is failed by operations.Service.Recover).
func (s *Service) Recover(ctx context.Context) (int64, error) {
	return s.repo.Recover(ctx)
}
