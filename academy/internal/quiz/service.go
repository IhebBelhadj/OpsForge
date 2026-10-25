package quiz

import (
	"context"
	"encoding/json"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

// Service starts, scores and lists quiz attempts.
type Service struct {
	repo    Repo
	content Content
	events  Publisher
	random  platform.Random
	clock   platform.Clock
}

// NewService draws questions with random and reads quizzes through content.
func NewService(repo Repo, content Content, events Publisher, random platform.Random, clock platform.Clock) *Service {
	return &Service{repo: repo, content: content, events: events, random: random, clock: clock}
}

// Start opens an attempt at a quiz item: its questions drawn now, its deadline set by the quiz's time limit.
func (s *Service) Start(ctx context.Context, user int64, course, item string) (Attempt, error) {
	ref, e, err := s.content.Content(ctx, course, item)
	if err != nil {
		return Attempt{}, err
	}
	if ref.Type != catalog.ItemQuiz {
		return Attempt{}, ErrNotAQuiz
	}
	def, err := Decode(e.Doc)
	if err != nil {
		return Attempt{}, err
	}
	now := s.clock.Now()
	a := Attempt{UserID: user, Course: course, Item: item, Hash: ref.Hash, StartedAt: now,
		Questions: Draw(def, s.random), Pass: def.Pass, Offered: def.XP}
	if def.TimeLimitS != nil {
		deadline := now.Add(time.Duration(*def.TimeLimitS) * time.Second)
		a.Deadline = &deadline
	}
	if a.ID, err = s.repo.Create(ctx, a); err != nil {
		return Attempt{}, err
	}
	return a, nil
}

// own is user's attempt id, or ErrAttemptNotFound.
func (s *Service) own(ctx context.Context, user, id int64) (Attempt, error) {
	a, err := s.repo.Attempt(ctx, id)
	if err != nil {
		return Attempt{}, err
	}
	if a.UserID != user {
		return Attempt{}, ErrAttemptNotFound
	}
	return a, nil
}

// Attempt is one of user's attempts.
func (s *Service) Attempt(ctx context.Context, user, id int64) (Attempt, error) {
	return s.own(ctx, user, id)
}

// Submit grades an open attempt. The quiz's xp is earned on its first pass only.
func (s *Service) Submit(ctx context.Context, user, id int64, answers map[string][]int) (Attempt, error) {
	a, err := s.own(ctx, user, id)
	if err != nil {
		return Attempt{}, err
	}
	if a.SubmittedAt != nil {
		return Attempt{}, ErrSubmitted
	}
	now := s.clock.Now()
	if a.Deadline != nil && now.After(a.Deadline.Add(DeadlineGrace)) {
		return Attempt{}, ErrExpired
	}
	if _, err := a.Grade(answers, now); err != nil {
		return Attempt{}, err
	}
	if a.Passed {
		passed, err := s.repo.Passed(ctx, user)
		if err != nil {
			return Attempt{}, err
		}
		a.XP = a.Offered
		for _, p := range passed {
			if p.Course == a.Course && p.Item == a.Item {
				a.XP = 0
				break
			}
		}
	}
	if err := s.repo.Submit(ctx, a); err != nil {
		return Attempt{}, err
	}
	data, _ := json.Marshal(map[string]any{"attempt": a.ID, "score": a.Score, "passed": a.Passed, "xp": a.XP,
		"content_hash": a.Hash})
	if _, err := s.events.Publish(ctx, platform.Event{UserID: user, Kind: KindSubmitted,
		Subject: a.Course + "/" + a.Item, Data: data, At: now}); err != nil {
		return Attempt{}, err
	}
	return a, nil
}

// Attempts are user's attempts at a quiz item, newest first.
func (s *Service) Attempts(ctx context.Context, user int64, course, item string) ([]Attempt, error) {
	ref, _, err := s.content.Content(ctx, course, item)
	if err != nil {
		return nil, err
	}
	if ref.Type != catalog.ItemQuiz {
		return nil, ErrNotAQuiz
	}
	return s.repo.Attempts(ctx, user, course, item)
}

// Passed are user's passed attempts, oldest first (their completions and XP).
func (s *Service) Passed(ctx context.Context, user int64) ([]Attempt, error) {
	return s.repo.Passed(ctx, user)
}
