package labstate

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/lab"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

// Service serves the lab's state from a short cache. One toolchain call at a time: callers that arrive meanwhile
// wait for its answer.
type Service struct {
	lab   LabController
	clock platform.Clock
	mu    sync.Mutex
	state lab.State
	at    time.Time // when state was read; zero: nothing cached
}

// NewService reads the state through controller.
func NewService(controller LabController, clock platform.Clock) *Service {
	return &Service{lab: controller, clock: clock}
}

// State is the lab's state, at most CacheTTL old. A failed call is not cached.
func (s *Service) State(ctx context.Context) (lab.State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.at.IsZero() && s.clock.Now().Sub(s.at) < CacheTTL {
		return s.state, nil
	}
	st, err := s.lab.State(ctx)
	if err != nil {
		return lab.State{}, fmt.Errorf("%w: %w", ErrToolchain, err)
	}
	s.state, s.at = st, s.clock.Now()
	return st, nil
}

// Invalidate makes the next State ask the toolchain.
func (s *Service) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.at = time.Time{}
}

// Resolver resolves lab texts for the running topology, plus extra values (an attempt's "${ns}"). When the state
// cannot be read, the texts keep the placeholders the extra values do not cover.
func (s *Service) Resolver(ctx context.Context, extra map[string]string) func(string) string {
	values := map[string]string{}
	if st, err := s.State(ctx); err == nil {
		for k, v := range st.Placeholders {
			values[k] = v
		}
	}
	for k, v := range extra {
		values[k] = v
	}
	return func(text string) string { return Resolve(text, values) }
}

// OnEvent is the history handler: a lab event (lab.started, lab.passed, ...) may have changed links and
// instances, so the next read asks the toolchain.
func (s *Service) OnEvent(_ context.Context, e platform.Event) error {
	if strings.HasPrefix(e.Kind, "lab.") {
		s.Invalidate()
	}
	return nil
}
