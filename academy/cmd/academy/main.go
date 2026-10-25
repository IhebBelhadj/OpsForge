// Command academy is the OpsForge Academy platform (docs/courses/): the HTTP API and the embedded web app on
// 127.0.0.1, over SQLite, calling the opsforge CLI for everything that touches the lab.
//
//	academy [serve] [flags]          serve (default)
//	academy migrate-legacy [flags]   import Phase 10's labs.db (-legacy-db) once; again imports only what is new
//	academy compile [--json] [flags] [pack...]   compile the packs (default: the content folder's) and list errors
//	academy version                  print the build version and the toolchain contract it speaks
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/IhebBelhadj/opsforge/academy/internal/config"
	"github.com/IhebBelhadj/opsforge/academy/internal/opsforgeapi"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

// version is stamped by make academy (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	command := "serve"
	if len(args) > 0 && (args[0] == "serve" || args[0] == "version" || args[0] == "migrate-legacy" ||
		args[0] == "compile") {
		command, args = args[0], args[1:]
	}
	if command == "version" {
		fmt.Printf("academy %s (opsforge contract %d.%d or later)\n", version, opsforgeapi.Contract, opsforgeapi.MinMinor)
		return 0
	}
	if command == "compile" {
		return compilePacks(args)
	}
	cfg, err := config.Load(args, os.Getenv)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "academy: %v\n", err)
		return 2
	}
	if command == "migrate-legacy" && cfg.LegacyDB == "" {
		fmt.Fprintln(os.Stderr, "academy: migrate-legacy needs --legacy-db (OpsForge's .opsforge/labs.db, or a copy)")
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

// compilePacks is `academy compile`: the bundle on stdout with --json, else a summary; exit 1 when a pack has
// errors (authors and `make check` run it).
func compilePacks(args []string) int {
	cfg, c, err := config.LoadCompile(args, os.Getenv)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "academy: %v\n", err)
		return 2
	}
	opsforge, err := provideOpsForge(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "academy: %v\n", err)
		return 2
	}
	packs := provideCompiler(cfg, opsforge, platform.SystemClock{})
	var data json.RawMessage
	if len(c.Packs) > 0 {
		data, err = packs.CompilePaths(context.Background(), c.Packs)
	} else {
		data, err = packs.Compile(context.Background())
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "academy: compile: %v\n", err)
		return 1
	}
	var b struct {
		Packs []struct {
			Courses []json.RawMessage `json:"courses"`
			Labs    []json.RawMessage `json:"labs"`
		} `json:"packs"`
		Errors []struct {
			Pack    string `json:"pack"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(data, &b); err != nil {
		fmt.Fprintf(os.Stderr, "academy: compile: %v\n", err)
		return 1
	}
	if c.JSON {
		fmt.Println(string(data))
	} else {
		courses, labs := 0, 0
		for _, p := range b.Packs {
			courses, labs = courses+len(p.Courses), labs+len(p.Labs)
		}
		summary := "no errors"
		if len(b.Errors) > 0 {
			summary = fmt.Sprintf("%d error(s)", len(b.Errors))
		}
		fmt.Printf("%d pack(s), %d course(s), %d lab(s): %s\n", len(b.Packs), courses, labs, summary)
		for _, e := range b.Errors {
			fmt.Printf("  %s: %s\n", e.Pack, e.Message)
		}
	}
	if len(b.Errors) > 0 {
		return 1
	}
	return 0
}
