package labstate

import (
	"context"

	"github.com/IhebBelhadj/opsforge/academy/internal/lab"
)

// LabController reports the lab's state (opsforgecli.Runner).
type LabController interface {
	State(ctx context.Context) (lab.State, error)
}
