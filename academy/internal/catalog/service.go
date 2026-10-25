package catalog

import (
	"context"
	"fmt"
)

// Service answers the catalogue's reads: the course list, a course page, an item.
type Service struct {
	r Reader
}

// NewService reads from r.
func NewService(r Reader) *Service { return &Service{r: r} }

// Courses lists the live courses.
func (s *Service) Courses(ctx context.Context) ([]CourseSummary, error) {
	entries, err := s.r.LiveList(ctx, KindCourse)
	if err != nil {
		return nil, err
	}
	out := make([]CourseSummary, 0, len(entries))
	for _, e := range entries {
		c, err := CourseView(e)
		if err != nil {
			return nil, err
		}
		out = append(out, c.CourseSummary)
	}
	return out, nil
}

// Course is one live course, or ErrNotFound.
func (s *Service) Course(ctx context.Context, id string) (Course, error) {
	e, err := s.r.Live(ctx, KindCourse, id)
	if err != nil {
		return Course{}, err
	}
	return CourseView(e)
}

// Item is one live item of a live course with its content, or ErrNotFound.
func (s *Service) Item(ctx context.Context, course, id string) (Item, error) {
	if _, err := s.r.Live(ctx, KindCourse, course); err != nil {
		return Item{}, err
	}
	e, err := s.r.Live(ctx, KindItem, course+"/"+id)
	if err != nil {
		return Item{}, err
	}
	d, err := decodeItem(e)
	if err != nil {
		return Item{}, err
	}
	it := Item{Course: course, ID: d.ID, Type: d.Type, Title: d.Title, Minutes: d.Minutes, Section: d.Section,
		Hash: e.Hash}
	// The item's content was imported with it: a missing one is a broken catalogue, not a 404.
	content := func(kind Kind, key string) (Entry, error) {
		c, err := s.r.Live(ctx, kind, key)
		if err != nil {
			return c, fmt.Errorf("item %s: its %s %s: %w", e.Key, kind, key, err)
		}
		return c, nil
	}
	switch d.Type {
	case ItemLecture:
		c, err := content(KindLecture, course+"/"+d.Ref)
		if err == nil {
			it.Lecture, err = LectureView(c)
		}
		return it, err
	case ItemQuiz:
		c, err := content(KindQuiz, course+"/"+d.Ref)
		if err == nil {
			it.Quiz, err = QuizView(c)
		}
		return it, err
	case ItemChallenge:
		c, err := content(KindChallenge, course+"/"+d.Ref)
		if err == nil {
			it.Challenge, err = ChallengeView(c)
		}
		return it, err
	case ItemLab:
		c, err := content(KindLab, d.Ref)
		if err == nil {
			it.Lab, err = LabView(c)
		}
		return it, err
	}
	return Item{}, fmt.Errorf("item %s: unknown type %q", e.Key, d.Type)
}
