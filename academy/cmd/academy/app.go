package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/config"
	"github.com/IhebBelhadj/opsforge/academy/internal/httpapi"
	"github.com/IhebBelhadj/opsforge/academy/internal/opsforgecli"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/terminal"
	"github.com/IhebBelhadj/opsforge/academy/internal/terminal/ptyshell"
	"github.com/IhebBelhadj/opsforge/academy/web"
)

// App is the wired platform: one HTTP server over the services.
type App struct {
	cfg       config.Config
	log       *slog.Logger
	handler   http.Handler
	terminals *terminal.Service
	ready     chan string // receives the listen address once serving (tests use port 0)
}

// Run serves until ctx ends, then stops accepting and gives running requests cfg.ShutdownTimeout.
func (a *App) Run(ctx context.Context) error {
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", a.cfg.Addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: a.handler, ReadHeaderTimeout: 10 * time.Second, BaseContext: func(net.Listener) context.Context {
		return context.WithoutCancel(ctx)
	}}
	// Terminal WebSockets are hijacked connections Shutdown does not wait for: hang up their shells, which
	// closes each socket as "going away"
	srv.RegisterOnShutdown(a.terminals.Shutdown)
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	a.log.Info("serving", slog.String("url", "http://"+ln.Addr().String()+"/"))
	if a.ready != nil {
		a.ready <- ln.Addr().String()
	}

	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}
	a.log.Info("shutting down", slog.Duration("grace", a.cfg.ShutdownTimeout))
	shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Providers (cmd/academy/wire.go wires them; tests build what they need by hand).

func provideLogger(cfg config.Config) *slog.Logger {
	return platform.NewLogger(os.Stderr, cfg.LogLevel)
}

func provideStore(ctx context.Context, cfg config.Config, clock platform.Clock) (*sqlite.Store, func(), error) {
	store, err := sqlite.Open(ctx, cfg.DB, clock)
	if err != nil {
		return nil, nil, err
	}
	return store, func() { _ = store.Close() }, nil
}

func provideToolchain(cfg config.Config) *opsforgecli.Runner {
	return opsforgecli.New(cfg.Toolchain, cfg.Root, cfg.ToolchainTimeout)
}

func provideReadiness(store *sqlite.Store, toolchain *opsforgecli.Runner) []httpapi.ReadinessCheck {
	return []httpapi.ReadinessCheck{{Name: "database", Probe: store}, {Name: "toolchain", Probe: toolchain}}
}

func provideTerminals() (*terminal.Service, func()) {
	svc := terminal.NewService(ptyshell.Spawner{})
	return svc, svc.Shutdown
}

// provideShells is the lab shell of Phase 10: bash in the repository with the opsforge function. M1 moves it into
// each attempt's workspace.
func provideShells(cfg config.Config, svc *terminal.Service) (terminal.Shells, error) {
	spec, err := ptyshell.Bash(cfg.Root, filepath.Join(cfg.Root, ".opsforge"), 80, 24)
	if err != nil {
		return terminal.Shells{}, fmt.Errorf("terminal shell: %w", err)
	}
	return terminal.NewShells(svc, spec), nil
}

func provideHandler(build platform.Build, checks []httpapi.ReadinessCheck, shells terminal.Shells,
	log *slog.Logger) http.Handler {
	return httpapi.NewHandler(httpapi.Deps{Build: build, Readiness: checks, Web: web.FS(), Log: log, Terminals: shells})
}

func newApp(cfg config.Config, log *slog.Logger, handler http.Handler, terminals *terminal.Service) *App {
	return &App{cfg: cfg, log: log, handler: handler, terminals: terminals}
}
