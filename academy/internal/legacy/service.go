package legacy

import (
	"context"
	"fmt"
)

// Service runs the migration.
type Service struct {
	reader Reader
	store  Store
}

// NewService reads Phase 10 databases with reader and writes into store.
func NewService(reader Reader, store Store) *Service {
	return &Service{reader: reader, store: store}
}

// Migrate imports what the Phase 10 database at path holds and earlier runs did not import, for user.
func (s *Service) Migrate(ctx context.Context, path string, user int64) (Report, error) {
	src, err := s.reader.Read(ctx, path)
	if err != nil {
		return Report{}, fmt.Errorf("reading %s: %w", path, err)
	}
	done, err := s.store.Done(ctx)
	if err != nil {
		return Report{}, err
	}
	p, err := NewPlan(src, done)
	if err != nil {
		return Report{}, err
	}
	return s.store.Apply(ctx, user, p)
}
