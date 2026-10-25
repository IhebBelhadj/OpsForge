package operations

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

// Work is an operation's body; its result is marshalled as the operation's result.
type Work func(ctx context.Context) (any, error)

// Service starts operations and keeps track of the running ones.
type Service struct {
	repo    Repo
	sender  Sender
	clock   platform.Clock
	log     *slog.Logger
	running sync.WaitGroup
}

// NewService stores operations in repo and pushes their statuses through sender.
func NewService(repo Repo, sender Sender, clock platform.Clock, log *slog.Logger) *Service {
	return &Service{repo: repo, sender: sender, clock: clock, log: log}
}

// Start records a running operation and runs work in the background, detached from ctx's cancellation (the
// request that started it ends at once) but not from its values.
func (s *Service) Start(ctx context.Context, user int64, kind, subject string, work Work) (Operation, error) {
	op := Operation{ID: newID(), UserID: user, Kind: kind, Subject: subject, Status: Running,
		CreatedAt: s.clock.Now()}
	if err := s.repo.Create(ctx, op); err != nil {
		return Operation{}, fmt.Errorf("operation %s: %w", kind, err)
	}
	s.push(op)
	s.running.Add(1)
	go func() {
		defer s.running.Done()
		s.finish(context.WithoutCancel(ctx), op, work)
	}()
	return op, nil
}

func (s *Service) finish(ctx context.Context, op Operation, work Work) {
	result, err := run(ctx, work)
	now := s.clock.Now()
	op.FinishedAt = &now
	op.Status = Done
	if err == nil {
		op.Result, err = json.Marshal(result)
	}
	if err != nil {
		msg := err.Error()
		op.Status, op.Error, op.Result = Failed, &msg, nil
		s.log.Error("operation failed", slog.String("id", op.ID), slog.String("kind", op.Kind),
			slog.String("subject", op.Subject), slog.Any("error", err))
	}
	if err := s.repo.Finish(ctx, op); err != nil {
		s.log.Error("recording a finished operation", slog.String("id", op.ID), slog.Any("error", err))
	}
	s.push(op)
}

// run calls work, turning a panic into an error: one broken operation must not stop the platform.
func run(ctx context.Context, work Work) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("internal error: %v", r)
		}
	}()
	return work(ctx)
}

func (s *Service) push(op Operation) {
	doc, err := json.Marshal(op)
	if err != nil {
		return
	}
	s.sender.Send(op.UserID, platform.Message{Event: StreamEvent, Data: doc})
}

// Get is one of user's operations.
func (s *Service) Get(ctx context.Context, user int64, id string) (Operation, error) {
	op, err := s.repo.Get(ctx, id)
	if err != nil {
		return Operation{}, err
	}
	if op.UserID != user {
		return Operation{}, ErrNotFound
	}
	return op, nil
}

// Wait returns when every running operation has finished, or when ctx ends (shutdown's grace period).
func (s *Service) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.running.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Recover marks the operations a previous process left running as failed (Interrupted).
func (s *Service) Recover(ctx context.Context) error {
	n, err := s.repo.FailRunning(ctx, Interrupted, s.clock.Now())
	if n > 0 {
		s.log.Warn("operations interrupted by a restart", slog.Int64("count", n))
	}
	return err
}

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b) // never fails (crypto/rand panics instead)
	return hex.EncodeToString(b)
}
