// Command academy is the OpsForge Academy platform (docs/courses/): the HTTP API and the embedded web app on
// 127.0.0.1, over SQLite, calling the opsforge CLI for everything that touches the lab.
//
//	academy [serve] [flags]          serve (default)
//	academy migrate-legacy [flags]   import Phase 10's labs.db (-legacy-db) once; again imports only what is new
//	academy version                  print the build version and the toolchain contract it speaks
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/IhebBelhadj/opsforge/academy/internal/config"
	"github.com/IhebBelhadj/opsforge/academy/internal/opsforgecli"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

// version is stamped by make academy (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	command := "serve"
	if len(args) > 0 && (args[0] == "serve" || args[0] == "version" || args[0] == "migrate-legacy") {
		command, args = args[0], args[1:]
	}
	if command == "version" {
		fmt.Printf("academy %s (toolchain contract %d)\n", version, opsforgecli.Contract)
		return 0
	}
	cfg, err := config.Load(args, os.Getenv)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "academy: %v\n", err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app, cleanup, err := initApp(ctx, cfg, platform.Build{Version: version})
	if err != nil {
		fmt.Fprintf(os.Stderr, "academy: %v\n", err)
		return 1
	}
	defer cleanup()
	if command == "migrate-legacy" {
		r, err := app.MigrateLegacy(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "academy: migrate-legacy: %v\n", err)
			return 1
		}
		fmt.Printf("imported from %s: %d attempt(s), %d step result(s), %d hint(s), %d event(s), %d award(s)\n",
			cfg.LegacyDB, r.Attempts, r.Steps, r.Hints, r.Events, r.Awards)
		return 0
	}
	if err := app.Run(ctx); err != nil {
		app.log.Error("stopped", "error", err)
		return 1
	}
	return 0
}
