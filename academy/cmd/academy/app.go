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

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
	catalogsqlite "github.com/IhebBelhadj/opsforge/academy/internal/catalog/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/config"
	"github.com/IhebBelhadj/opsforge/academy/internal/contentimport"
	"github.com/IhebBelhadj/opsforge/academy/internal/history"
	historysqlite "github.com/IhebBelhadj/opsforge/academy/internal/history/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/httpapi"
	"github.com/IhebBelhadj/opsforge/academy/internal/identity"
	identitysqlite "github.com/IhebBelhadj/opsforge/academy/internal/identity/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/labsession"
	labsqlite "github.com/IhebBelhadj/opsforge/academy/internal/labsession/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/operations"
	operationssqlite "github.com/IhebBelhadj/opsforge/academy/internal/operations/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/opsforgecli"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/progress"
	progresssqlite "github.com/IhebBelhadj/opsforge/academy/internal/progress/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/quiz"
	quizsqlite "github.com/IhebBelhadj/opsforge/academy/internal/quiz/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/terminal"
	"github.com/IhebBelhadj/opsforge/academy/internal/terminal/ptyshell"
	"github.com/IhebBelhadj/opsforge/academy/internal/workspace"
	"github.com/IhebBelhadj/opsforge/academy/internal/workspace/disk"
	workspacesqlite "github.com/IhebBelhadj/opsforge/academy/internal/workspace/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/web"
)

// App is the wired platform: one HTTP server over the services.
type App struct {
	cfg       config.Config
	log       *slog.Logger
	handler   http.Handler
	terminals *terminal.Service
	importer  *contentimport.Service
	hub       *platform.Hub
	ops       *operations.Service
	labs      *labsession.Service
	ready     chan string // receives the listen address once serving (tests use port 0)
}

// Run imports the packs, then serves until ctx ends, then stops accepting and gives running requests
// cfg.ShutdownTimeout. A failed import is logged, not fatal: the catalogue keeps the last good import.
func (a *App) Run(ctx context.Context) error {
	// What a stopped process left half done: its operations failed, its attempts in setup made abandonable
	if err := a.ops.Recover(ctx); err != nil {
		return fmt.Errorf("recovering operations: %w", err)
	}
	if n, err := a.labs.Recover(ctx); err != nil {
		return fmt.Errorf("recovering lab attempts: %w", err)
	} else if n > 0 {
		a.log.Warn("lab attempts interrupted during their setup; abandon them to tear them down", slog.Int64("count", n))
	}
	if report, err := a.importer.Import(ctx); err != nil {
		a.log.Warn("content import failed; serving the last imported catalogue", slog.Any("error", err))
	} else {
		a.log.Info("content imported", slog.Bool("changed", report.Changed), slog.Any("added", report.Added),
			slog.Any("updated", report.Updated), slog.Any("retired", report.Retired))
	}
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
	// Event streams never go idle either: closing the hub ends them (the pages reconnect to the next server)
	srv.RegisterOnShutdown(a.hub.Close)
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
	// Lab operations run past their requests: they get what is left of the grace period
	if err := a.ops.Wait(shutdown); err != nil {
		a.log.Warn("lab operations still running at shutdown; the next start marks them interrupted")
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
	return opsforgecli.New(cfg.Toolchain, cfg.Root, cfg.ToolchainTimeout, cfg.LabTimeout)
}

func provideReadiness(store *sqlite.Store, toolchain *opsforgecli.Runner) []httpapi.ReadinessCheck {
	return []httpapi.ReadinessCheck{{Name: "database", Probe: store}, {Name: "toolchain", Probe: toolchain}}
}

func provideTerminals() (*terminal.Service, func()) {
	svc := terminal.NewService(ptyshell.Spawner{})
	return svc, svc.Shutdown
}

// provideShells is the lab shell of Phase 10: bash in the repository with the opsforge function, or in a lab
// attempt's workspace when the terminal is opened for one.
func provideShells(cfg config.Config, svc *terminal.Service) (terminal.Shells, error) {
	spec, err := ptyshell.Bash(cfg.Root, filepath.Join(cfg.Root, ".opsforge"), 80, 24)
	if err != nil {
		return terminal.Shells{}, fmt.Errorf("terminal shell: %w", err)
	}
	return terminal.NewShells(svc, spec), nil
}

func provideIdentity(store *sqlite.Store, clock platform.Clock) *identity.Service {
	return identity.NewService(identitysqlite.NewRepo(store.DB), clock)
}

func provideHub() (*platform.Hub, func()) {
	hub := platform.NewHub()
	return hub, hub.Close
}

func provideHistory(store *sqlite.Store, hub *platform.Hub, clock platform.Clock, log *slog.Logger) *history.Service {
	return history.NewService(historysqlite.NewRepo(store.DB), hub, clock, log)
}

func provideCatalogRepo(store *sqlite.Store) *catalogsqlite.Repo {
	return catalogsqlite.NewRepo(store.DB)
}

func provideImporter(toolchain *opsforgecli.Runner, repo *catalogsqlite.Repo, clock platform.Clock) *contentimport.Service {
	return contentimport.NewService(toolchain, repo, clock)
}

func provideCatalog(repo *catalogsqlite.Repo) *catalog.Service { return catalog.NewService(repo) }

func provideOperations(store *sqlite.Store, hub *platform.Hub, clock platform.Clock, log *slog.Logger) *operations.Service {
	return operations.NewService(operationssqlite.NewRepo(store.DB), hub, clock, log)
}

func provideWorkspaces(cfg config.Config, store *sqlite.Store, clock platform.Clock) *workspace.Service {
	return workspace.NewService(disk.Store{Base: cfg.Workspaces}, workspacesqlite.NewRepo(store.DB), clock)
}

func provideLabs(store *sqlite.Store, courses *catalog.Service, toolchain *opsforgecli.Runner,
	spaces *workspace.Service, ops *operations.Service, events *history.Service, clock platform.Clock) *labsession.Service {
	return labsession.NewService(labsqlite.NewRepo(store.DB), courses, toolchain, spaces, ops, events, clock)
}

func provideQuizzes(store *sqlite.Store, courses *catalog.Service, events *history.Service,
	clock platform.Clock) *quiz.Service {
	return quiz.NewService(quizsqlite.NewRepo(store.DB), courses, events, platform.CryptoRandom{}, clock)
}

// provideProgress reads the items the other modules complete (passed quiz attempts and labs) and subscribes to the
// history (a completed course is noticed after the event that completed its last item).
func provideProgress(store *sqlite.Store, courses *catalog.Service, events *history.Service, clock platform.Clock,
	quizzes *quiz.Service, labs *labsession.Service) *progress.Service {
	passedQuizzes := progress.SourceFunc(func(ctx context.Context, user int64) ([]progress.Completion, error) {
		passed, err := quizzes.Passed(ctx, user)
		out := make([]progress.Completion, len(passed))
		for i, a := range passed {
			out[i] = progress.Completion{Type: catalog.ItemQuiz, Ref: a.Course + "/" + a.Item, Hash: a.Hash,
				At: *a.SubmittedAt}
		}
		return out, err
	})
	passedLabs := progress.SourceFunc(func(ctx context.Context, user int64) ([]progress.Completion, error) {
		passed, err := labs.Passed(ctx, user)
		out := make([]progress.Completion, len(passed))
		for i, a := range passed {
			out[i] = progress.Completion{Type: catalog.ItemLab, Ref: a.Lab, Hash: a.Hash, At: *a.FinishedAt}
		}
		return out, err
	})
	svc := progress.NewService(progresssqlite.NewRepo(store.DB), courses, events, clock,
		progress.Sources{passedQuizzes, passedLabs})
	events.Subscribe(svc.OnEvent)
	return svc
}

func provideHandler(build platform.Build, checks []httpapi.ReadinessCheck, shells terminal.Shells,
	ids *identity.Service, importer *contentimport.Service, courses *catalog.Service, events *history.Service,
	hub *platform.Hub, learning *progress.Service, quizzes *quiz.Service, labs *labsession.Service,
	ops *operations.Service, spaces *workspace.Service, log *slog.Logger) http.Handler {
	return httpapi.NewHandler(httpapi.Deps{
		Build: build, Readiness: checks, Web: web.FS(), Log: log, Terminals: shells, Identity: ids,
		Importer: importer, Catalog: courses, History: events, Streams: hub,
		Progress: learning, Quizzes: quizzes, Labs: labs, Operations: ops, Workspaces: spaces,
	})
}

func newApp(cfg config.Config, log *slog.Logger, handler http.Handler, terminals *terminal.Service,
	importer *contentimport.Service, hub *platform.Hub, ops *operations.Service, labs *labsession.Service) *App {
	return &App{cfg: cfg, log: log, handler: handler, terminals: terminals, importer: importer, hub: hub, ops: ops,
		labs: labs}
}
