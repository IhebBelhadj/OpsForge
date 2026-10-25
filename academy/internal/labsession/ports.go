package labsession

import (
	"context"
	"encoding/json"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
	"github.com/IhebBelhadj/opsforge/academy/internal/lab"
	"github.com/IhebBelhadj/opsforge/academy/internal/operations"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

// Repo stores attempts, their step results and the hints shown.
type Repo interface {
	// Create stores a new attempt and returns its id; ErrAnotherLabRunning if the user has an active one.
	Create(ctx context.Context, a Attempt) (int64, error)
	// SetPlaces records the attempt's namespace and workspace.
	SetPlaces(ctx context.Context, id int64, namespace, workspace string) error
	// Attempt reads an attempt, or ErrAttemptNotFound.
	Attempt(ctx context.Context, id int64) (Attempt, error)
	// Active is the user's attempt in setup or running, if any.
	Active(ctx context.Context, user int64) (Attempt, bool, error)
	// Finish records an attempt's status, end, time, XP and error.
	Finish(ctx context.Context, a Attempt) error
	// PassedSteps are the attempt's passed steps since it was last reset.
	PassedSteps(ctx context.Context, id int64) (map[string]bool, error)
	RecordStep(ctx context.Context, id int64, step string, passed bool, detail json.RawMessage, at time.Time) error
	// ClearSteps forgets the attempt's step results (a reset).
	ClearSteps(ctx context.Context, id int64) error
	// Hints are how many hints of each step the attempt has shown.
	Hints(ctx context.Context, id int64) (map[string]int, error)
	// ShowHint records hint idx of a step as shown and counts it in the attempt's hints_used, once.
	ShowHint(ctx context.Context, id int64, step string, idx int, at time.Time) error
	// Passed are the user's passed attempts, oldest first.
	Passed(ctx context.Context, user int64) ([]Attempt, error)
	// Recover moves attempts left in setup by a stopped process to running (they can be abandoned).
	Recover(ctx context.Context) (int64, error)
}

// Catalog reads entries as imported (catalog.Service).
type Catalog interface {
	Entry(ctx context.Context, kind catalog.Kind, key string) (catalog.Entry, error)
}

// LabController runs things on the lab through OpsForge (opsforgeapi.Client).
type LabController interface {
	State(ctx context.Context) (lab.State, error)
	Run(ctx context.Context, req lab.RunRequest) (lab.RunResult, error)
	Probe(ctx context.Context, req lab.ProbeRequest) (lab.ProbeResult, error)
	Heal(ctx context.Context) (bool, error)
}

// Workspaces creates an attempt's files (workspace.Service).
type Workspaces interface {
	Create(ctx context.Context, user int64, ws string, files map[string]string) error
}

// Operations runs the long part of a request in the background (operations.Service).
type Operations interface {
	Start(ctx context.Context, user int64, kind, subject string, work operations.Work) (operations.Operation, error)
}

// Publisher appends to the history (history.Service).
type Publisher interface {
	Publish(ctx context.Context, e platform.Event) (platform.Event, error)
}
