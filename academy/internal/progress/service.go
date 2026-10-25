package progress

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

// Service answers where a learner stands and records what only it records.
type Service struct {
	repo    Repo
	catalog Catalog
	events  Publisher
	clock   platform.Clock
	sources Sources
}

// NewService reads courses from cat and completions from sources.
func NewService(repo Repo, cat Catalog, events Publisher, clock platform.Clock, sources Sources) *Service {
	return &Service{repo: repo, catalog: cat, events: events, clock: clock, sources: sources}
}

// done is every completion of user from the other modules.
func (s *Service) done(ctx context.Context, user int64) (map[string]Completion, error) {
	var all []Completion
	for _, src := range s.sources {
		list, err := src.Completions(ctx, user)
		if err != nil {
			return nil, fmt.Errorf("completions: %w", err)
		}
		all = append(all, list...)
	}
	return Completions(all), nil
}

func (s *Service) compute(ctx context.Context, user int64, c catalog.Course, done map[string]Completion,
	completions map[string]time.Time) (CourseProgress, error) {
	records, err := s.repo.Records(ctx, user, c.ID)
	if err != nil {
		return CourseProgress{}, err
	}
	var at *time.Time
	if t, ok := completions[c.ID]; ok {
		at = &t
	}
	return Compute(c, records, done, at), nil
}

// Course is user's progress in a live course (catalog.ErrNotFound otherwise).
func (s *Service) Course(ctx context.Context, user int64, course string) (CourseProgress, error) {
	c, err := s.catalog.Course(ctx, course)
	if err != nil {
		return CourseProgress{}, err
	}
	done, err := s.done(ctx, user)
	if err != nil {
		return CourseProgress{}, err
	}
	completions, err := s.repo.CourseCompletions(ctx, user)
	if err != nil {
		return CourseProgress{}, err
	}
	return s.compute(ctx, user, c, done, completions)
}

// Overview is user's progress in every live course, in catalogue order.
func (s *Service) Overview(ctx context.Context, user int64) ([]CourseProgress, error) {
	courses, err := s.catalog.Courses(ctx)
	if err != nil {
		return nil, err
	}
	done, err := s.done(ctx, user)
	if err != nil {
		return nil, err
	}
	completions, err := s.repo.CourseCompletions(ctx, user)
	if err != nil {
		return nil, err
	}
	out := make([]CourseProgress, 0, len(courses))
	for _, sum := range courses {
		c, err := s.catalog.Course(ctx, sum.ID)
		if err != nil {
			return nil, err
		}
		p, err := s.compute(ctx, user, c, done, completions)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// find is an item of a live course (catalog.ErrNotFound otherwise).
func (s *Service) find(ctx context.Context, course, item string) (catalog.ItemRef, error) {
	c, err := s.catalog.Course(ctx, course)
	if err != nil {
		return catalog.ItemRef{}, err
	}
	for _, sec := range c.Sections {
		for _, it := range sec.Items {
			if it.ID == item {
				return it, nil
			}
		}
	}
	return catalog.ItemRef{}, catalog.ErrNotFound
}

// CompleteLecture marks a lecture completed on its current content. Completing it again changes nothing, unless
// the lecture was updated since: then it is completed anew (no longer stale).
func (s *Service) CompleteLecture(ctx context.Context, user int64, course, item string) (ItemProgress, error) {
	it, err := s.find(ctx, course, item)
	if err != nil {
		return ItemProgress{}, err
	}
	if it.Type != catalog.ItemLecture {
		return ItemProgress{}, ErrNotALecture
	}
	records, err := s.repo.Records(ctx, user, course)
	if err != nil {
		return ItemProgress{}, err
	}
	if r := records[item]; r.CompletedAt != nil && r.Hash == it.Hash {
		return ItemProgress{ID: item, Type: it.Type, Status: Completed, CompletedAt: r.CompletedAt}, nil
	}
	now := s.clock.Now()
	if err := s.repo.Complete(ctx, user, course, item, it.Hash, now); err != nil {
		return ItemProgress{}, err
	}
	data, _ := json.Marshal(map[string]string{"content_hash": it.Hash})
	if _, err := s.events.Publish(ctx, platform.Event{UserID: user, Kind: KindLectureCompleted,
		Subject: course + "/" + item, Data: data, At: now}); err != nil {
		return ItemProgress{}, err
	}
	return ItemProgress{ID: item, Type: it.Type, Status: Completed, CompletedAt: &now}, nil
}

// Open records that user opened the item (the resume point).
func (s *Service) Open(ctx context.Context, user int64, course, item string) error {
	if _, err := s.find(ctx, course, item); err != nil {
		return err
	}
	return s.repo.Open(ctx, user, course, item, s.clock.Now())
}

// Note is user's note on an item (empty, without UpdatedAt, when there is none).
func (s *Service) Note(ctx context.Context, user int64, course, item string) (Note, error) {
	if _, err := s.find(ctx, course, item); err != nil {
		return Note{}, err
	}
	return s.repo.Note(ctx, user, course, item)
}

// SaveNote replaces user's note on an item.
func (s *Service) SaveNote(ctx context.Context, user int64, course, item, body string) (Note, error) {
	if len(body) > MaxNote {
		return Note{}, ErrNoteTooLong
	}
	if _, err := s.find(ctx, course, item); err != nil {
		return Note{}, err
	}
	now := s.clock.Now()
	n := Note{Body: body, UpdatedAt: &now}
	return n, s.repo.SaveNote(ctx, user, course, item, n)
}

// CompletedCourses are the courses user has completed (first completions, live or not).
func (s *Service) CompletedCourses(ctx context.Context, user int64) ([]string, error) {
	completions, err := s.repo.CourseCompletions(ctx, user)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(completions))
	for c := range completions {
		out = append(out, c)
	}
	slices.Sort(out)
	return out, nil
}

// OnEvent is the history handler: after an event that completes an item, every course the user has now completed
// for the first time is recorded and announced (course.completed, which badge rules read).
func (s *Service) OnEvent(ctx context.Context, e platform.Event) error {
	if !CompletingEvents[e.Kind] {
		return nil
	}
	return s.Refresh(ctx, e.UserID)
}

// Refresh records and announces every course user has completed for the first time (after OnEvent's events, or
// after rows written without events: migrate-legacy).
func (s *Service) Refresh(ctx context.Context, user int64) error {
	overview, err := s.Overview(ctx, user)
	if err != nil {
		return err
	}
	for _, p := range overview {
		if !p.Complete || p.CompletedAt != nil {
			continue
		}
		now := s.clock.Now()
		first, err := s.repo.CompleteCourse(ctx, user, p.Course, now)
		if err != nil {
			return err
		}
		if !first {
			continue
		}
		data, _ := json.Marshal(map[string]int{"items": p.Total})
		if _, err := s.events.Publish(ctx, platform.Event{UserID: user, Kind: KindCourseCompleted,
			Subject: p.Course, Data: data, At: now}); err != nil {
			return err
		}
	}
	return nil
}
