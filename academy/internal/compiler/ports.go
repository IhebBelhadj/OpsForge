package compiler

import (
	"context"
	"errors"

	"github.com/IhebBelhadj/opsforge/academy/internal/lab"
)

// StepChecker checks scenario steps without running them (OpsForge's `api validate-steps`): which steps exist and
// what they take is the toolchain's knowledge, not the platform's.
type StepChecker interface {
	CheckSteps(ctx context.Context, sets []lab.StepSet) ([]lab.StepError, error)
}

// ErrSteps wraps a failed step check: the toolchain is missing, crashed or speaks another contract.
var ErrSteps = errors.New("checking the scenario steps failed")
