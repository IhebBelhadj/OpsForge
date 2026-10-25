// Package config is the platform's one typed configuration: flags over ACADEMY_* environment variables over
// defaults derived from the repository root, validated before anything starts.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"time"
)

// Config is injected like any dependency (cmd/academy/wire.go).
type Config struct {
	Addr             string        // HTTP listen address; loopback only (the terminal is a shell)
	Root             string        // the OpsForge repository (./opsforge, .opsforge/)
	Content          string        // the content folder: one pack per sub-folder with a pack.yml
	DB               string        // SQLite database
	Workspaces       string        // the learners' workspaces (one folder per lab attempt)
	Toolchain        string        // the opsforge CLI the platform calls (docs/courses/architecture.md)
	ToolchainTimeout time.Duration // default limit for one toolchain call
	LabTimeout       time.Duration // limit for a call that runs steps on the lab (setup, a check, teardown)
	ShutdownTimeout  time.Duration // how long running requests get on SIGTERM
	LegacyDB         string        // Phase 10's labs.db, read by migrate-legacy
	LogLevel         slog.Level
}

// Load reads the flags in args (without the program and command names) and the environment through getenv.
func Load(args []string, getenv func(string) string) (Config, error) {
	cfg, _, err := load("serve", args, getenv)
	return cfg, err
}

// Compile is what `academy compile` takes besides the configuration: pack folders (none: the content folder's
// packs) and whether to print the bundle.
type Compile struct {
	Packs []string
	JSON  bool
}

// LoadCompile reads the flags of `academy compile [--json] [flags] [pack...]`.
func LoadCompile(args []string, getenv func(string) string) (Config, Compile, error) {
	return load("compile", args, getenv)
}

func load(command string, args []string, getenv func(string) string) (Config, Compile, error) {
	or := func(key, def string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return def
	}
	fs := flag.NewFlagSet("academy "+command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", or("ACADEMY_ROOT", "."), "OpsForge repository root (ACADEMY_ROOT)")
	content := fs.String("content", getenv("ACADEMY_CONTENT"), "content packs folder (ACADEMY_CONTENT, default <root>/labs)")
	var compile Compile
	if command == "compile" {
		fs.BoolVar(&compile.JSON, "json", false, "print the bundle")
	}
	addr := fs.String("addr", or("ACADEMY_ADDR", "127.0.0.1:8090"), "listen address, loopback only (ACADEMY_ADDR)")
	db := fs.String("db", getenv("ACADEMY_DB"), "SQLite database (ACADEMY_DB, default <root>/.opsforge/academy.db)")
	workspaces := fs.String("workspaces", getenv("ACADEMY_WORKSPACES"),
		"workspaces folder (ACADEMY_WORKSPACES, default <root>/.opsforge/workspaces)")
	legacyDB := fs.String("legacy-db", getenv("ACADEMY_LEGACY_DB"),
		"Phase 10 database for migrate-legacy (ACADEMY_LEGACY_DB, default <root>/.opsforge/labs.db)")
	toolchain := fs.String("toolchain", getenv("ACADEMY_TOOLCHAIN"), "opsforge CLI (default <root>/opsforge)")
	toolchainTimeout := fs.Duration("toolchain-timeout", 30*time.Second, "limit for one toolchain call")
	labTimeout := fs.Duration("lab-timeout", 20*time.Minute, "limit for a toolchain call running steps on the lab")
	shutdownTimeout := fs.Duration("shutdown-timeout", 15*time.Second, "grace period on SIGTERM")
	level := fs.String("log-level", or("ACADEMY_LOG_LEVEL", "info"), "debug, info, warn or error (ACADEMY_LOG_LEVEL)")
	if err := fs.Parse(args); err != nil {
		return Config{}, compile, err
	}
	if command == "compile" {
		compile.Packs = fs.Args()
	} else if fs.NArg() > 0 {
		return Config{}, compile, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}

	absRoot, err := filepath.Abs(*root)
	if err != nil {
		return Config{}, compile, fmt.Errorf("root: %w", err)
	}
	absContent := filepath.Join(absRoot, "labs")
	if *content != "" {
		if absContent, err = filepath.Abs(*content); err != nil {
			return Config{}, compile, fmt.Errorf("content: %w", err)
		}
	}
	cfg := Config{
		Addr: *addr, Root: absRoot, Content: absContent, DB: *db, Workspaces: *workspaces, Toolchain: *toolchain,
		ToolchainTimeout: *toolchainTimeout, LabTimeout: *labTimeout, ShutdownTimeout: *shutdownTimeout,
		LegacyDB: *legacyDB,
	}
	if cfg.DB == "" {
		cfg.DB = filepath.Join(absRoot, ".opsforge", "academy.db")
	}
	if cfg.Workspaces == "" {
		cfg.Workspaces = filepath.Join(absRoot, ".opsforge", "workspaces")
	}
	if cfg.LegacyDB == "" {
		cfg.LegacyDB = filepath.Join(absRoot, ".opsforge", "labs.db")
	}
	if cfg.Toolchain == "" {
		cfg.Toolchain = filepath.Join(absRoot, "opsforge")
	}
	if err := cfg.LogLevel.UnmarshalText([]byte(*level)); err != nil {
		return Config{}, compile, fmt.Errorf("log-level: %w", err)
	}
	return cfg, compile, cfg.validate()
}

func (c Config) validate() error {
	host, _, err := net.SplitHostPort(c.Addr)
	if err != nil {
		return fmt.Errorf("addr %q: %w", c.Addr, err)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("addr %q: must be a loopback address (127.0.0.1 or localhost)", c.Addr)
	}
	var errs []error
	if !filepath.IsAbs(c.DB) || !filepath.IsAbs(c.Toolchain) || !filepath.IsAbs(c.Workspaces) || !filepath.IsAbs(c.LegacyDB) {
		errs = append(errs, errors.New("db, workspaces, toolchain and legacy-db must be absolute paths"))
	}
	if c.ToolchainTimeout <= 0 {
		errs = append(errs, errors.New("toolchain-timeout must be positive"))
	}
	if c.LabTimeout <= 0 {
		errs = append(errs, errors.New("lab-timeout must be positive"))
	}
	if c.ShutdownTimeout <= 0 {
		errs = append(errs, errors.New("shutdown-timeout must be positive"))
	}
	return errors.Join(errs...)
}
