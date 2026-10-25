package contentimport

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

// ErrCompile wraps a failed compile: a pack folder cannot be read, or the toolchain could not check the scenario
// steps (missing, crashed or speaking another contract).
var ErrCompile = errors.New("compiling the packs failed")

// Service imports the packs; one import runs at a time.
type Service struct {
	compiler Compiler
	store    Store
	clock    platform.Clock
	mu       sync.Mutex
}

// NewService imports what compiler compiles into store.
func NewService(compiler Compiler, store Store, clock platform.Clock) *Service {
	return &Service{compiler: compiler, store: store, clock: clock}
}

// Import compiles the packs and brings the catalogue to them. A bundle with errors (*BundleErrors) or a failed
// compile writes nothing.
func (s *Service) Import(ctx context.Context) (Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	bundle, err := s.compiler.Compile(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("%w: %w", ErrCompile, err)
	}
	next, err := Entries(bundle)
	if err != nil {
		return Report{}, err
	}
	current, err := s.store.Entries(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("catalogue: %w", err)
	}
	ch := Diff(current, next)
	if ch.Report.Changed {
		if err := s.store.Apply(ctx, ch.Upsert, ch.Retire, s.clock.Now()); err != nil {
			return Report{}, fmt.Errorf("catalogue: %w", err)
		}
	}
	return ch.Report, nil
}
