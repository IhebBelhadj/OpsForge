package labstate_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/lab"
	"github.com/IhebBelhadj/opsforge/academy/internal/labstate"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

type countingLab struct {
	calls int
	err   error
}

func (l *countingLab) State(context.Context) (lab.State, error) {
	l.calls++
	return lab.State{Up: true, Placeholders: map[string]string{"${node:a}": "a-cp1"}}, l.err
}

func TestStateIsCachedBriefly(t *testing.T) {
	c := &clock{time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)}
	l := &countingLab{}
	svc := labstate.NewService(l, c)
	ctx := context.Background()
	for range 3 {
		if st, err := svc.State(ctx); err != nil || !st.Up {
			t.Fatalf("%+v %v", st, err)
		}
	}
	if l.calls != 1 {
		t.Fatalf("calls = %d", l.calls)
	}
	c.now = c.now.Add(labstate.CacheTTL)
	_, _ = svc.State(ctx)
	if l.calls != 2 {
		t.Fatalf("after the TTL: calls = %d", l.calls)
	}
	_ = svc.OnEvent(ctx, platform.Event{Kind: "step.checked"})
	_, _ = svc.State(ctx)
	if l.calls != 2 {
		t.Fatalf("a step check invalidated the cache")
	}
	_ = svc.OnEvent(ctx, platform.Event{Kind: "lab.passed"})
	_, _ = svc.State(ctx)
	if l.calls != 3 {
		t.Fatalf("after lab.passed: calls = %d", l.calls)
	}
}

func TestAFailedCallIsNotCachedAndTextsStayRaw(t *testing.T) {
	l := &countingLab{err: errors.New("incus is down")}
	svc := labstate.NewService(l, &clock{time.Now()})
	ctx := context.Background()
	if _, err := svc.State(ctx); !errors.Is(err, labstate.ErrToolchain) {
		t.Fatalf("err = %v", err)
	}
	resolve := svc.Resolver(ctx, map[string]string{"${ns}": "lab-3"})
	if got := resolve("${node:a} -n ${ns}"); got != "${node:a} -n lab-3" {
		t.Fatalf("got %q", got)
	}
	if l.calls != 2 {
		t.Fatalf("calls = %d", l.calls)
	}
	l.err = nil
	if got := svc.Resolver(ctx, nil)("${node:a}"); got != "a-cp1" {
		t.Fatalf("got %q", got)
	}
}
