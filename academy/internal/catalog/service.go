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
	d, e, c, err := s.item(ctx, course, id)
	if err != nil {
		return Item{}, err
	}
	it := Item{Course: course, ID: d.ID, Type: d.Type, Title: d.Title, Minutes: d.Minutes, Section: d.Section,
		Hash: e.Hash}
	switch d.Type {
	case ItemLecture:
		it.Lecture, err = LectureView(c)
	case ItemQuiz:
		it.Quiz, err = QuizView(c)
	case ItemChallenge:
		it.Challenge, err = ChallengeView(c)
	case ItemLab:
		it.Lab, err = LabView(c)
	}
	return it, err
}

// Content is a live item of a live course and the entry holding its content as imported: the quiz with its keys,
// the lab with its setup and checks. For the modules that grade the item; the transport serves Item.
func (s *Service) Content(ctx context.Context, course, id string) (ItemRef, Entry, error) {
	d, e, c, err := s.item(ctx, course, id)
	if err != nil {
		return ItemRef{}, Entry{}, err
	}
	return ItemRef{ID: d.ID, Type: d.Type, Ref: d.Ref, Title: d.Title, Minutes: d.Minutes, Hash: e.Hash}, c, nil
}

// Entry is one live entry as imported (a lab, a pack), or ErrNotFound; like Content, not for the transport.
func (s *Service) Entry(ctx context.Context, kind Kind, key string) (Entry, error) {
	return s.r.Live(ctx, kind, key)
}

// Entries are every entry of a kind as imported, retired ones included (Entry.Retired), by pack then position: for
// the modules that keep what a learner earned from content since retired (awards). Not for the transport.
func (s *Service) Entries(ctx context.Context, kind Kind) ([]Entry, error) {
	return s.r.List(ctx, kind)
}

// item reads a live item entry of a live course and its content entry.
func (s *Service) item(ctx context.Context, course, id string) (itemDoc, Entry, Entry, error) {
	if _, err := s.r.Live(ctx, KindCourse, course); err != nil {
		return itemDoc{}, Entry{}, Entry{}, err
	}
	e, err := s.r.Live(ctx, KindItem, course+"/"+id)
	if err != nil {
		return itemDoc{}, Entry{}, Entry{}, err
	}
	d, err := decodeItem(e)
	if err != nil {
		return d, e, Entry{}, err
	}
	var kind Kind
	key := course + "/" + d.Ref
	switch d.Type {
	case ItemLecture:
		kind = KindLecture
	case ItemQuiz:
		kind = KindQuiz
	case ItemChallenge:
		kind = KindChallenge
	case ItemLab:
		kind, key = KindLab, d.Ref
	default:
		return d, e, Entry{}, fmt.Errorf("item %s: unknown type %q", e.Key, d.Type)
	}
	// The item's content was imported with it: a missing one is a broken catalogue, not a 404
	c, err := s.r.Live(ctx, kind, key)
	if err != nil {
		return d, e, c, fmt.Errorf("item %s: its %s %s: %w", e.Key, kind, key, err)
	}
	return d, e, c, nil
}
