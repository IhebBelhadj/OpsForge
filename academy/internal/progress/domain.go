// Package progress is where a learner stands in each course: every item's status (not started, in progress,
// completed, and whether the content changed since it was completed), the course's completion and the item to
// resume from (docs/courses/content-model.md, "Completion"). Lectures are completed here; quizzes, labs and
// challenges by their modules, read through the Completions port, so a lab passed in one course completes it in
// every course and an item added later shows as done when it is.
package progress

import (
	"errors"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
)

// The events progress publishes (subject "<course>/<item>" and the course id).
const (
	KindLectureCompleted = "lecture.completed"
	KindCourseCompleted  = "course.completed"
)

// CompletingEvents are the events after which a course may have become complete (published by this module and the
// ones behind Completions).
var CompletingEvents = map[string]bool{
	KindLectureCompleted: true, "quiz.submitted": true, "lab.passed": true, "challenge.accepted": true,
}

// MaxNote is the longest note, in bytes.
const MaxNote = 64 << 10

var (
	// ErrNotALecture is completing an item that its own module completes (a quiz is passed, not marked done).
	ErrNotALecture = errors.New("only lectures are completed by hand")
	// ErrNoteTooLong is a note over MaxNote.
	ErrNoteTooLong = errors.New("the note is too long")
)

// Status is where the learner is with an item.
type Status string

// The statuses of an item.
const (
	NotStarted Status = "not_started"
	InProgress Status = "in_progress" // opened, not completed
	Completed  Status = "completed"
)

// Record is what this module keeps about an item: when it was last opened, and a lecture's completion with the
// content hash it was completed on.
type Record struct {
	Hash        string
	CompletedAt *time.Time
	OpenedAt    *time.Time
}

// Completion is an item completed by another module: Type is the item type, Ref "<course>/<item>" for a quiz or a
// challenge and the lab id for a lab; Hash the content hash it was completed on (the latest completion).
type Completion struct {
	Type, Ref, Hash string
	At              time.Time
}

func completionKey(typ, ref string) string { return typ + "\x00" + ref }

// Completions indexes completions by item type and ref, keeping the latest per item.
func Completions(list []Completion) map[string]Completion {
	out := make(map[string]Completion, len(list))
	for _, c := range list {
		k := completionKey(c.Type, c.Ref)
		if prev, ok := out[k]; !ok || c.At.After(prev.At) {
			out[k] = c
		}
	}
	return out
}

// ItemProgress is one item's status. Stale: completed on content that has changed since.
type ItemProgress struct {
	ID, Type    string
	Status      Status
	Stale       bool
	CompletedAt *time.Time
}

// CourseProgress is a course's items in course order with their statuses. Complete: every item is completed now
// (stale ones count); CompletedAt: when it first was (kept when items are added later). Resume is the item to
// continue with, "" when the course is complete.
type CourseProgress struct {
	Course              string
	Items               []ItemProgress
	Completed, Total    int
	Complete            bool
	CompletedAt         *time.Time
	Resume              string
	LastOpened          *time.Time
	lastOpenedItemIndex int
}

// Compute is a course's progress from this module's records (by item id), the other modules' completions
// (Completions) and the course's first completion.
func Compute(c catalog.Course, records map[string]Record, done map[string]Completion, completedAt *time.Time) CourseProgress {
	p := CourseProgress{Course: c.ID, CompletedAt: completedAt, lastOpenedItemIndex: -1}
	for _, sec := range c.Sections {
		for _, it := range sec.Items {
			r := records[it.ID]
			ip := ItemProgress{ID: it.ID, Type: it.Type, Status: NotStarted}
			if r.OpenedAt != nil {
				ip.Status = InProgress
				if p.LastOpened == nil || r.OpenedAt.After(*p.LastOpened) {
					p.LastOpened, p.lastOpenedItemIndex = r.OpenedAt, len(p.Items)
				}
			}
			hash, at, ok := completion(c.ID, it, r, done)
			if ok {
				ip.Status, ip.Stale, ip.CompletedAt = Completed, hash != it.Hash, at
				p.Completed++
			}
			p.Items = append(p.Items, ip)
		}
	}
	p.Total = len(p.Items)
	p.Complete = p.Total > 0 && p.Completed == p.Total
	p.Resume = resume(p)
	return p
}

// completion is how an item was completed: by its record for a lecture, by another module's row otherwise.
func completion(course string, it catalog.ItemRef, r Record, done map[string]Completion) (string, *time.Time, bool) {
	var key string
	switch it.Type {
	case catalog.ItemLecture:
		return r.Hash, r.CompletedAt, r.CompletedAt != nil
	case catalog.ItemLab:
		key = completionKey(it.Type, it.Ref)
	default:
		key = completionKey(it.Type, course+"/"+it.ID)
	}
	c, ok := done[key]
	if !ok {
		return "", nil, false
	}
	at := c.At
	return c.Hash, &at, true
}

// resume is the item opened last if it is not completed, else the first item after it that is not, else the first
// one that is not; "" when every item is completed.
func resume(p CourseProgress) string {
	n := len(p.Items)
	start := max(p.lastOpenedItemIndex, 0)
	for i := range n {
		if it := p.Items[(start+i)%n]; it.Status != Completed {
			return it.ID
		}
	}
	return ""
}

// Note is the learner's note on an item.
type Note struct {
	Body      string
	UpdatedAt *time.Time // nil: no note yet
}
