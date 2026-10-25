package rewards

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

// RewardingEvents are the events after which a badge may have become earned.
var RewardingEvents = map[string]bool{
	"lab.passed": true, "quiz.submitted": true, "course.completed": true, "challenge.accepted": true,
}

// Service evaluates badges and answers what a learner earned. mu makes one evaluation at a time, so an award is
// announced once.
type Service struct {
	repo    Repo
	catalog Catalog
	sources Sources
	events  Publisher
	clock   platform.Clock
	log     *slog.Logger
	mu      sync.Mutex
}

// NewService reads badges from cat and the learner's history from sources.
func NewService(repo Repo, cat Catalog, sources Sources, events Publisher, clock platform.Clock,
	log *slog.Logger) *Service {
	return &Service{repo: repo, catalog: cat, sources: sources, events: events, clock: clock, log: log}
}

// badges are every badge as imported (retired ones too), in pack order, with the live courses that award each, and
// the live packs' curricula.
func (s *Service) badges(ctx context.Context) ([]Badge, map[string][]string, error) {
	courses := map[string][]string{} // badge → courses
	entries, err := s.catalog.Entries(ctx, catalog.KindCourse)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		var c struct{ Badge *string }
		if err := json.Unmarshal(e.Doc, &c); err != nil {
			return nil, nil, fmt.Errorf("course %s: %w", e.Key, err)
		}
		if !e.Retired && c.Badge != nil {
			courses[*c.Badge] = append(courses[*c.Badge], e.Key)
		}
	}
	curricula := map[string][]string{}
	if entries, err = s.catalog.Entries(ctx, catalog.KindPack); err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		var p struct {
			Curricula []struct {
				ID     string
				Levels []struct{ Labs []string }
			}
		}
		if err := json.Unmarshal(e.Doc, &p); err != nil {
			return nil, nil, fmt.Errorf("pack %s: %w", e.Key, err)
		}
		for _, c := range p.Curricula {
			if e.Retired {
				continue
			}
			labs := []string{}
			for _, l := range c.Levels {
				labs = append(labs, l.Labs...)
			}
			curricula[c.ID] = labs
		}
	}
	if entries, err = s.catalog.Entries(ctx, catalog.KindBadge); err != nil {
		return nil, nil, err
	}
	out := make([]Badge, 0, len(entries))
	for _, e := range entries {
		var d struct {
			Description string
			Icon        *string
			Rule        json.RawMessage
		}
		if err := json.Unmarshal(e.Doc, &d); err != nil {
			return nil, nil, fmt.Errorf("badge %s: %w", e.Key, err)
		}
		out = append(out, Badge{ID: e.Key, Pack: e.PackID, Title: e.Title, Description: d.Description, Icon: d.Icon,
			Position: e.Position, Rule: d.Rule, Courses: courses[e.Key], Retired: e.Retired})
	}
	return out, curricula, nil
}

// history is user's history for the rules.
func (s *Service) history(ctx context.Context, user int64, curricula map[string][]string) (History, error) {
	h := History{Curricula: curricula, Courses: map[string]bool{}}
	var err error
	if h.Labs, err = s.sources.Labs(ctx, user); err != nil {
		return History{}, fmt.Errorf("lab passes: %w", err)
	}
	if h.Quizzes, err = s.sources.Quizzes(ctx, user); err != nil {
		return History{}, fmt.Errorf("quiz passes: %w", err)
	}
	courses, err := s.sources.Courses(ctx, user)
	if err != nil {
		return History{}, fmt.Errorf("completed courses: %w", err)
	}
	for _, c := range courses {
		h.Courses[c] = true
	}
	return h, nil
}

// Summary is user's XP and badges (live ones, and retired ones already awarded).
func (s *Service) Summary(ctx context.Context, user int64) (Summary, error) {
	badges, curricula, err := s.badges(ctx)
	if err != nil {
		return Summary{}, err
	}
	h, err := s.history(ctx, user, curricula)
	if err != nil {
		return Summary{}, err
	}
	awards, err := s.repo.Awards(ctx, user)
	if err != nil {
		return Summary{}, err
	}
	out := Summary{XP: h.XP(), Badges: []Award{}}
	for _, b := range badges {
		a := Award{Badge: b}
		if at, ok := awards[b.ID]; ok {
			a.AwardedAt = &at
		} else if b.Retired {
			continue
		}
		out.Badges = append(out.Badges, a)
	}
	return out, nil
}

// Evaluate awards user every live badge now earned and not awarded yet, announcing each (badge.awarded).
func (s *Service) Evaluate(ctx context.Context, user int64) ([]Award, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	badges, curricula, err := s.badges(ctx)
	if err != nil {
		return nil, err
	}
	return s.evaluate(ctx, user, badges, curricula)
}

func (s *Service) evaluate(ctx context.Context, user int64, badges []Badge, curricula map[string][]string) ([]Award, error) {
	h, err := s.history(ctx, user, curricula)
	if err != nil {
		return nil, err
	}
	awards, err := s.repo.Awards(ctx, user)
	if err != nil {
		return nil, err
	}
	var out []Award
	for _, b := range badges {
		if _, done := awards[b.ID]; done || b.Retired {
			continue
		}
		earned, err := Earned(b, h)
		if err != nil {
			s.log.Warn("badge rule not evaluated", slog.String("badge", b.ID), slog.Any("error", err))
			continue
		}
		if !earned {
			continue
		}
		now := s.clock.Now()
		first, err := s.repo.Award(ctx, user, b.ID, now)
		if err != nil {
			return out, err
		}
		if !first {
			continue
		}
		data, _ := json.Marshal(map[string]any{"title": b.Title, "pack": b.Pack})
		if _, err := s.events.Publish(ctx, platform.Event{UserID: user, Kind: KindAwarded, Subject: b.ID,
			Data: data, At: now}); err != nil {
			return out, err
		}
		out = append(out, Award{Badge: b, AwardedAt: &now})
	}
	return out, nil
}

// EvaluateAll evaluates every user (after an import: a new or changed rule is retroactive).
func (s *Service) EvaluateAll(ctx context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	users, err := s.sources.Users(ctx)
	if err != nil {
		return 0, err
	}
	badges, curricula, err := s.badges(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, u := range users {
		awarded, err := s.evaluate(ctx, u, badges, curricula)
		n += len(awarded)
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// OnEvent is the history handler: after an event that may earn a badge, the user's badges are evaluated.
func (s *Service) OnEvent(ctx context.Context, e platform.Event) error {
	if !RewardingEvents[e.Kind] {
		return nil
	}
	_, err := s.Evaluate(ctx, e.UserID)
	return err
}
