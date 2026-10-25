package labsession_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/lab"
	"github.com/IhebBelhadj/opsforge/academy/internal/labsession"
)

func ptr[T any](v T) *T { return &v }

func TestStartRunsTheSetupInItsNamespace(t *testing.T) {
	f := newFixture()
	a, out := f.start(t, "tour")
	if a.Status != labsession.Setup || a.Namespace != "lab-1" || a.Workspace != "lab-1" || a.Hash != "H-tour" ||
		a.Course != "k8s" || a.Item != "item" {
		t.Fatalf("started %+v", a)
	}
	if out.Status != labsession.Running || out.Error != nil || out.Attempt != 1 {
		t.Fatalf("outcome %+v", out)
	}
	if f.lab.called() != "setup 1 lab-1" {
		t.Fatalf("calls: %s", f.lab.called())
	}
	if f.spaces.created["lab-1"]["web.yaml"] != "kind: Pod\n" {
		t.Fatalf("workspace %+v", f.spaces.created)
	}
	if stored, _ := f.repo.Attempt(context.Background(), 1); stored.Status != labsession.Running {
		t.Fatalf("stored %+v", stored)
	}
	if f.bus.kinds() != "lab.started" {
		t.Fatalf("events %s", f.bus.kinds())
	}
}

func TestALabWithoutSetupOrNamespaceCallsNothing(t *testing.T) {
	f := newFixture()
	_, out := f.start(t, "plain")
	if out.Status != labsession.Running || f.lab.called() != "" {
		t.Fatalf("%+v, calls %s", out, f.lab.called())
	}
}

func TestStartRefusals(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		lab   string
		setup func(*fixture)
		want  error
	}{
		"unknown lab":   {"nope", nil, labsession.ErrLabNotFound},
		"locked":        {"next", nil, labsession.ErrLocked},
		"other layout":  {"far", nil, labsession.ErrUnavailable},
		"lab down":      {"tour", func(f *fixture) { f.lab.state.Up = false }, labsession.ErrLabDown},
		"no toolchain":  {"tour", func(f *fixture) { f.lab.stateErr = errors.New("python?") }, labsession.ErrToolchain},
		"another lab":   {"tour", func(f *fixture) { f.start(t, "plain") }, labsession.ErrAnotherLabRunning},
		"no workspace":  {"tour", func(f *fixture) { f.spaces.err = errors.New("disk full") }, errAny},
		"unknown state": {"far", func(f *fixture) { f.lab.state.Layout = nil }, labsession.ErrUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			if tc.setup != nil {
				tc.setup(f)
			}
			_, _, err := f.svc.Start(ctx, 1, tc.lab, "", "")
			if errors.Is(tc.want, errAny) {
				if err == nil {
					t.Fatal("started")
				}
			} else if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			// Refused or failed, it leaves no active attempt behind and the lab free
			if _, active, _ := f.repo.Active(ctx, 1); active && name != "another lab" {
				t.Fatal("an attempt is still active")
			}
			if name != "another lab" {
				f.lab.state.Up, f.lab.stateErr, f.spaces.err = true, nil, nil
				if _, _, err := f.svc.Start(ctx, 1, "plain", "", ""); err != nil {
					t.Fatalf("the lab stayed reserved: %v", err)
				}
			}
		})
	}
	var locked *labsession.LockedError
	_, _, err := newFixture().svc.Start(ctx, 1, "next", "", "")
	if !errors.As(err, &locked) || len(locked.Missing) != 1 || locked.Missing[0] != "tour" {
		t.Fatalf("%v", err)
	}
}

// errAny marks a case that only needs to fail (no sentinel to match).
var errAny = errors.New("any error")

func TestAFailedSetupIsTornDownAndFailsTheAttempt(t *testing.T) {
	f := newFixture()
	f.lab.failRun[lab.Setup] = "step 1 failed: exit 1"
	_, out := f.start(t, "tour")
	if out.Status != labsession.Failed || out.Error == nil || *out.Error != "step 1 failed: exit 1" {
		t.Fatalf("%+v", out)
	}
	if f.lab.called() != "setup 1 lab-1, teardown 1 lab-1, heal" {
		t.Fatalf("calls: %s", f.lab.called())
	}
	if f.bus.kinds() != "lab.started lab.failed" {
		t.Fatalf("events %s", f.bus.kinds())
	}
	a, _ := f.repo.Attempt(context.Background(), 1)
	if a.Status != labsession.Failed || a.FinishedAt == nil || a.Error == "" {
		t.Fatalf("%+v", a)
	}
	f.lab.failRun = map[lab.Phase]string{}
	if _, out := f.start(t, "tour"); out.Status != labsession.Running {
		t.Fatalf("starting again: %+v", out)
	}
}

func TestStepsAreCheckedInOrderUntilThePass(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	a, _ := f.start(t, "tour")

	if _, err := f.svc.Check(ctx, 1, a.ID, "why", ptr(1)); !errors.Is(err, labsession.ErrStepOrder) {
		t.Fatalf("out of order: %v", err)
	}
	if _, err := f.svc.Check(ctx, 1, a.ID, "nope", nil); !errors.Is(err, labsession.ErrNoSuchStep) {
		t.Fatalf("unknown step: %v", err)
	}
	f.lab.probes = []lab.ProbeResult{{Passed: false, Error: "nodes: exit 1",
		Results: []lab.Expectation{{Metric: "p.exit", Want: "== 0", Got: ptr(1.0), Checked: true}}}}
	out := f.check(t, a.ID, "look", nil)
	if *out.Passed || out.Error == nil || len(out.Results) != 1 || out.Status != labsession.Running {
		t.Fatalf("failing probe: %+v", out)
	}
	if out := f.check(t, a.ID, "look", nil); !*out.Passed {
		t.Fatalf("passing probe: %+v", out)
	}
	if _, err := f.svc.Check(ctx, 1, a.ID, "why", nil); !errors.Is(err, labsession.ErrAnswerRequired) {
		t.Fatalf("quiz without answer: %v", err)
	}
	if out := f.check(t, a.ID, "why", ptr(0)); *out.Passed || out.Explain != nil {
		t.Fatalf("wrong answer: %+v", out)
	}
	if out := f.check(t, a.ID, "why", ptr(1)); !*out.Passed || *out.Explain != "because" {
		t.Fatalf("right answer: %+v", out)
	}
	f.clock.add(90 * time.Second)
	out = f.check(t, a.ID, "done", nil)
	if out.Status != labsession.Passed || *out.XP != 100 || *out.Seconds != 90 || out.Error != nil {
		t.Fatalf("pass: %+v", out)
	}
	if f.lab.called() != "setup 1 lab-1, probe lab-1, probe lab-1, teardown 1 lab-1, heal" {
		t.Fatalf("calls: %s", f.lab.called())
	}
	if f.bus.kinds() != "lab.started step.checked step.checked step.checked step.checked step.checked lab.passed" {
		t.Fatalf("events %s", f.bus.kinds())
	}
	if _, err := f.svc.Check(ctx, 1, a.ID, "done", nil); !errors.Is(err, labsession.ErrNotRunning) {
		t.Fatalf("after the pass: %v", err)
	}
	passed, _ := f.svc.Passed(ctx, 1)
	if len(passed) != 1 || passed[0].XP != 100 {
		t.Fatalf("%+v", passed)
	}
	// The pass unlocks the lab that requires it
	if _, out := f.start(t, "next"); out.Status != labsession.Running {
		t.Fatalf("%+v", out)
	}
}

func TestAProbeTheToolchainCannotRunFailsTheCheck(t *testing.T) {
	f := newFixture()
	a, _ := f.start(t, "tour")
	f.lab.probeErr = errors.New("toolchain probe: contract 2")
	if out := f.check(t, a.ID, "look", nil); *out.Passed || *out.Error != "toolchain probe: contract 2" {
		t.Fatalf("%+v", out)
	}
}

func TestHintsCostXPOnce(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	a, _ := f.start(t, "tour")
	h, err := f.svc.Hint(ctx, 1, a.ID, "look")
	if err != nil || h.Index != 0 || h.Text != "h1" || h.HintsUsed != 1 {
		t.Fatalf("%+v %v", h, err)
	}
	if h, _ := f.svc.Hint(ctx, 1, a.ID, "look"); h.Index != 1 || h.Text != "h2" || h.HintsUsed != 2 {
		t.Fatalf("%+v", h)
	}
	if _, err := f.svc.Hint(ctx, 1, a.ID, "look"); !errors.Is(err, labsession.ErrNoMoreHints) {
		t.Fatalf("third: %v", err)
	}
	if _, err := f.svc.Hint(ctx, 1, a.ID, "why"); !errors.Is(err, labsession.ErrNoMoreHints) {
		t.Fatalf("a step without hints: %v", err)
	}
	if _, err := f.svc.Hint(ctx, 1, a.ID, "nope"); !errors.Is(err, labsession.ErrNoSuchStep) {
		t.Fatalf("unknown step: %v", err)
	}
	v, _ := f.svc.View(ctx, 1, a.ID)
	if len(v.Steps[0].Hints) != 2 || v.Current != "look" || v.HintsUsed != 2 {
		t.Fatalf("view %+v", v)
	}
	f.check(t, a.ID, "look", nil)
	f.check(t, a.ID, "why", ptr(1))
	if out := f.check(t, a.ID, "done", nil); *out.XP != 50 { // 100 × (1 − 2 × 0.25)
		t.Fatalf("%+v", out)
	}
}

func TestResetStartsTheStepsOver(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	a, _ := f.start(t, "tour")
	f.check(t, a.ID, "look", nil)
	op, err := f.svc.Reset(ctx, 1, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out := f.outcome(t, op); out.Status != labsession.Running || out.Error != nil {
		t.Fatalf("%+v", out)
	}
	if f.lab.called() != "setup 1 lab-1, probe lab-1, teardown 1 lab-1, heal, setup 1 lab-1" {
		t.Fatalf("calls: %s", f.lab.called())
	}
	if v, _ := f.svc.View(ctx, 1, a.ID); v.Current != "look" || v.Steps[0].Passed {
		t.Fatalf("%+v", v)
	}

	g := newFixture()
	s, _ := g.start(t, "snap")
	if _, err := g.svc.Reset(ctx, 1, s.ID); !errors.Is(err, labsession.ErrResetUnsupported) {
		t.Fatalf("snapshot lab: %v", err)
	}
}

func TestAbandonTearsDownAndEndsTheAttempt(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	a, _ := f.start(t, "tour")
	f.lab.healErr = errors.New("wan-core unreachable")
	op, err := f.svc.Abandon(ctx, 1, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	out := f.outcome(t, op)
	if out.Status != labsession.Abandoned || out.Error == nil || *out.Error != "heal: wan-core unreachable" {
		t.Fatalf("%+v", out)
	}
	if f.lab.called() != "setup 1 lab-1, teardown 1 lab-1, heal" {
		t.Fatalf("calls: %s", f.lab.called())
	}
	if _, err := f.svc.Abandon(ctx, 1, a.ID); !errors.Is(err, labsession.ErrNotRunning) {
		t.Fatalf("twice: %v", err)
	}
	if _, ok, _ := f.svc.Current(ctx, 1); ok {
		t.Fatal("still current")
	}
	if f.bus.kinds() != "lab.started lab.abandoned" {
		t.Fatalf("events %s", f.bus.kinds())
	}
}

func TestAttemptsAreTheirUsers(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	a, _ := f.start(t, "tour")
	for name, err := range map[string]error{
		"check":   func() error { _, err := f.svc.Check(ctx, 2, a.ID, "look", nil); return err }(),
		"hint":    func() error { _, err := f.svc.Hint(ctx, 2, a.ID, "look"); return err }(),
		"reset":   func() error { _, err := f.svc.Reset(ctx, 2, a.ID); return err }(),
		"abandon": func() error { _, err := f.svc.Abandon(ctx, 2, a.ID); return err }(),
		"view":    func() error { _, err := f.svc.View(ctx, 2, a.ID); return err }(),
		"unknown": func() error { _, err := f.svc.View(ctx, 1, 99); return err }(),
	} {
		if !errors.Is(err, labsession.ErrAttemptNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if v, ok, err := f.svc.Current(ctx, 1); err != nil || !ok || v.ID != a.ID || v.Current != "look" || len(v.Steps) != 3 {
		t.Fatalf("current %+v %v %v", v, ok, err)
	}
	if _, ok, _ := f.svc.Current(ctx, 2); ok {
		t.Fatal("user 2 has user 1's attempt")
	}
}

// slowLab blocks probes until released, to hold the lab busy.
type slowLab struct {
	*fakeLab
	release chan struct{}
}

func (l slowLab) Probe(ctx context.Context, req lab.ProbeRequest) (lab.ProbeResult, error) {
	<-l.release
	return l.fakeLab.Probe(ctx, req)
}

func TestOneLabOperationAtATime(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	slow := slowLab{fakeLab: f.lab, release: make(chan struct{})}
	f.svc = labsession.NewService(f.repo, catalogue{}, slow, f.spaces, f.ops, f.bus, f.clock)
	a, _ := f.start(t, "tour")
	op, err := f.svc.Check(ctx, 1, a.ID, "look", nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{
		"check":   func() error { _, err := f.svc.Check(ctx, 1, a.ID, "look", nil); return err }(),
		"reset":   func() error { _, err := f.svc.Reset(ctx, 1, a.ID); return err }(),
		"abandon": func() error { _, err := f.svc.Abandon(ctx, 1, a.ID); return err }(),
		"start":   func() error { _, _, err := f.svc.Start(ctx, 2, "plain", "", ""); return err }(),
	} {
		if !errors.Is(err, labsession.ErrBusy) {
			t.Errorf("%s while a probe runs: %v", name, err)
		}
	}
	if _, err := f.svc.Hint(ctx, 1, a.ID, "look"); err != nil { // a hint touches no lab
		t.Fatalf("hint while a probe runs: %v", err)
	}
	close(slow.release)
	if out := f.outcome(t, op); !*out.Passed {
		t.Fatalf("%+v", out)
	}
	if _, err := f.svc.Abandon(ctx, 1, a.ID); err != nil {
		t.Fatalf("after the probe: %v", err)
	}
}

func TestRecoverLetsAnInterruptedSetupBeAbandoned(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	id, _ := f.repo.Create(ctx, labsession.Attempt{UserID: 1, Lab: "tour", Status: labsession.Setup, StartedAt: t0})
	if _, err := f.svc.Abandon(ctx, 1, id); !errors.Is(err, labsession.ErrNotRunning) {
		t.Fatalf("in setup: %v", err)
	}
	if n, err := f.svc.Recover(ctx); err != nil || n != 1 {
		t.Fatalf("%d %v", n, err)
	}
	op, err := f.svc.Abandon(ctx, 1, id)
	if err != nil {
		t.Fatal(err)
	}
	if out := f.outcome(t, op); out.Status != labsession.Abandoned {
		t.Fatalf("%+v", out)
	}
}
