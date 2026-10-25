package compiler

import (
	"context"
	"encoding/json"

	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

// Service compiles the packs of the content folder (contentimport's Compiler).
type Service struct {
	dir   string
	steps StepChecker
	clock platform.Clock
}

// NewService compiles the packs in dir, checking their steps with steps.
func NewService(dir string, steps StepChecker, clock platform.Clock) *Service {
	return &Service{dir: dir, steps: steps, clock: clock}
}

// Compile compiles every pack of the content folder into the bundle.
func (s *Service) Compile(ctx context.Context) (json.RawMessage, error) {
	sources, err := Packs(s.dir)
	if err != nil {
		return nil, err
	}
	defer sources.Close()
	return Compile(ctx, sources.List, s.steps, s.clock.Now())
}

// CompilePaths compiles the pack folders at paths (academy compile <pack>...).
func (s *Service) CompilePaths(ctx context.Context, paths []string) (json.RawMessage, error) {
	sources, err := Folders(s.dir, paths)
	if err != nil {
		return nil, err
	}
	defer sources.Close()
	return Compile(ctx, sources.List, s.steps, s.clock.Now())
}
