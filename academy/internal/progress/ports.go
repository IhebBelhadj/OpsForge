package progress

import (
	"context"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

// Repo stores this module's records.
type Repo interface {
	// Records are user's records in a course, by item id.
	Records(ctx context.Context, user int64, course string) (map[string]Record, error)
	// Complete records a lecture's completion on content hash.
	Complete(ctx context.Context, user int64, course, item, hash string, at time.Time) error
	// Open records that the item was opened at `at`.
	Open(ctx context.Context, user int64, course, item string, at time.Time) error
	// CompleteCourse records a course's first completion; false if it was already recorded.
	CompleteCourse(ctx context.Context, user int64, course string, at time.Time) (bool, error)
	// CourseCompletions are user's first completions, by course id.
	CourseCompletions(ctx context.Context, user int64) (map[string]time.Time, error)
	Note(ctx context.Context, user int64, course, item string) (Note, error)
	SaveNote(ctx context.Context, user int64, course, item string, n Note) error
}

// Catalog reads the live courses (catalog.Service).
type Catalog interface {
	Courses(ctx context.Context) ([]catalog.CourseSummary, error)
	Course(ctx context.Context, id string) (catalog.Course, error)
}

// Source lists the items one module completed for a user (quiz attempts passed, labs passed).
type Source interface {
	Completions(ctx context.Context, user int64) ([]Completion, error)
}

// SourceFunc is a Source (cmd/academy adapts each module's passes with one).
type SourceFunc func(ctx context.Context, user int64) ([]Completion, error)

// Completions calls f.
func (f SourceFunc) Completions(ctx context.Context, user int64) ([]Completion, error) {
	return f(ctx, user)
}

// Sources are every module that completes items.
type Sources []Source

// Publisher appends to the history (history.Service).
type Publisher interface {
	Publish(ctx context.Context, e platform.Event) (platform.Event, error)
}
