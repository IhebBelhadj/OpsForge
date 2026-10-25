package operations_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/operations"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

var t0 = time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)

type clock struct{}

func (clock) Now() time.Time { return t0 }

type memRepo struct {
	mu  sync.Mutex
	ops map[string]operations.Operation
}

func (r *memRepo) Create(_ context.Context, op operations.Operation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ops[op.ID] = op
	return nil
}

func (r *memRepo) Finish(_ context.Context, op operations.Operation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ops[op.ID] = op
	return nil
}

func (r *memRepo) Get(_ context.Context, id string) (operations.Operation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	op, ok := r.ops[id]
	if !ok {
		return op, operations.ErrNotFound
	}
	return op, nil
}

func (r *memRepo) FailRunning(_ context.Context, reason string, at time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int64
	for id, op := range r.ops {
		if op.Status == operations.Running {
			op.Status, op.Error, op.FinishedAt = operations.Failed, &reason, &at
			r.ops[id] = op
			n++
		}
	}
	return n, nil
}

type recorder struct {
	mu   sync.Mutex
	msgs []platform.Message
}

func (r *recorder) Send(_ int64, m platform.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, m)
}

func newService() (*operations.Service, *memRepo, *recorder) {
	repo, rec := &memRepo{ops: map[string]operations.Operation{}}, &recorder{}
	return operations.NewService(repo, rec, clock{}, slog.New(slog.NewTextHandler(io.Discard, nil))), repo, rec
}

func TestAnOperationRunsInTheBackgroundAndIsPushedTwice(t *testing.T) {
	svc, _, rec := newService()
	ctx, cancel := context.WithCancel(context.Background())
	release := make(chan struct{})
	op, err := svc.Start(ctx, 1, "lab.check", "meet", func(ctx context.Context) (any, error) {
		<-release
		if ctx.Err() != nil {
			return nil, errors.New("the work was cancelled with its request")
		}
		return map[string]bool{"passed": true}, nil
	})
	if err != nil || op.Status != operations.Running || len(op.ID) != 24 {
		t.Fatalf("%+v %v", op, err)
	}
	cancel() // the request ends
	if got, _ := svc.Get(context.Background(), 1, op.ID); got.Status != operations.Running {
		t.Fatalf("finished before its work: %+v", got)
	}
	close(release)
	if err := svc.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(context.Background(), 1, op.ID)
	if err != nil || got.Status != operations.Done || string(got.Result) != `{"passed":true}` || got.Error != nil ||
		!got.FinishedAt.Equal(t0) {
		t.Fatalf("%+v %v", got, err)
	}
	if len(rec.msgs) != 2 || rec.msgs[0].Event != operations.StreamEvent || rec.msgs[1].ID != 0 {
		t.Fatalf("pushed %+v", rec.msgs)
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.msgs[1].Data, &doc); err != nil || doc["status"] != "done" || doc["kind"] != "lab.check" {
		t.Fatalf("pushed %s", rec.msgs[1].Data)
	}
}

func TestAFailingOrPanickingOperationIsFailed(t *testing.T) {
	svc, _, _ := newService()
	ctx := context.Background()
	failing, _ := svc.Start(ctx, 1, "k", "s", func(context.Context) (any, error) { return nil, errors.New("boom") })
	panicking, _ := svc.Start(ctx, 1, "k", "s", func(context.Context) (any, error) { panic("oops") })
	_ = svc.Wait(ctx)
	for op, want := range map[string]string{failing.ID: "boom", panicking.ID: "internal error: oops"} {
		got, _ := svc.Get(ctx, 1, op)
		if got.Status != operations.Failed || got.Error == nil || *got.Error != want || got.Result != nil {
			t.Errorf("%+v", got)
		}
	}
}

func TestOperationsAreTheirUsers(t *testing.T) {
	svc, _, _ := newService()
	ctx := context.Background()
	op, _ := svc.Start(ctx, 1, "k", "s", func(context.Context) (any, error) { return nil, nil })
	_ = svc.Wait(ctx)
	if _, err := svc.Get(ctx, 2, op.ID); !errors.Is(err, operations.ErrNotFound) {
		t.Fatalf("another user's: %v", err)
	}
	if _, err := svc.Get(ctx, 1, "nope"); !errors.Is(err, operations.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
}

func TestWaitGivesUpWithItsContext(t *testing.T) {
	svc, _, _ := newService()
	release := make(chan struct{})
	defer close(release)
	_, _ = svc.Start(context.Background(), 1, "k", "s", func(context.Context) (any, error) {
		<-release
		return nil, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := svc.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("%v", err)
	}
}

func TestRecoverFailsWhatARestartInterrupted(t *testing.T) {
	svc, repo, _ := newService()
	repo.ops["old"] = operations.Operation{ID: "old", UserID: 1, Status: operations.Running}
	if err := svc.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if op := repo.ops["old"]; op.Status != operations.Failed || *op.Error != operations.Interrupted {
		t.Fatalf("%+v", op)
	}
}
