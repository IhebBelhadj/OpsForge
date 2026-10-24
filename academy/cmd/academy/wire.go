//go:build wireinject

package main

import (
	"context"

	"github.com/google/wire"

	"github.com/IhebBelhadj/opsforge/academy/internal/config"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

// initApp is the object graph; `make gen` (wire) writes its implementation to wire_gen.go.
func initApp(ctx context.Context, cfg config.Config, build platform.Build) (*App, func(), error) {
	wire.Build(
		wire.Bind(new(platform.Clock), new(platform.SystemClock)),
		wire.Value(platform.SystemClock{}),
		provideLogger,
		provideStore,
		provideToolchain,
		provideReadiness,
		provideTerminals,
		provideShells,
		provideIdentity,
		provideHandler,
		newApp,
	)
	return nil, nil, nil
}
