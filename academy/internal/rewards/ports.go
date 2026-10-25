package rewards

import (
	"context"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

// Repo stores awards.
type Repo interface {
	// Awards are user's awards: badge id → when.
	Awards(ctx context.Context, user int64) (map[string]time.Time, error)
	// Award records a badge for user; false when it was already awarded.
	Award(ctx context.Context, user int64, badge string, at time.Time) (bool, error)
}

// Catalog lists entries as imported, retired ones included (catalog.Service).
type Catalog interface {
	Entries(ctx context.Context, kind catalog.Kind) ([]catalog.Entry, error)
}

// Sources read what the other modules recorded (cmd/academy adapts labsession, quiz, progress and identity).
type Sources struct {
	// Labs are user's passed lab attempts.
	Labs func(ctx context.Context, user int64) ([]LabPass, error)
	// Quizzes are user's passed quiz attempts.
	Quizzes func(ctx context.Context, user int64) ([]QuizPass, error)
	// Courses are the courses user has completed.
	Courses func(ctx context.Context, user int64) ([]string, error)
	// Users are every user (a rule added by an import is evaluated for each).
	Users func(ctx context.Context) ([]int64, error)
}

// Publisher appends to the history (history.Service).
type Publisher interface {
	Publish(ctx context.Context, e platform.Event) (platform.Event, error)
}
