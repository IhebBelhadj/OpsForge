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
	Root             string        // the OpsForge repository (packs, ./opsforge, .opsforge/)
	DB               string        // SQLite database
	Workspaces       string        // the learners' workspaces (one folder per lab attempt)
	Toolchain        string        // the opsforge CLI the platform calls (docs/courses/architecture.md)
	ToolchainTimeout time.Duration // default limit for one toolchain call
	LabTimeout       time.Duration // limit for a call that runs steps on the lab (setup, a check, teardown)
	ShutdownTimeout  time.Duration // how long running requests get on SIGTERM
	LogLevel         slog.Level
}

// Load reads the flags in args (without the program and command names) and the environment through getenv.
func Load(args []string, getenv func(string) string) (Config, error) {
	or := func(key, def string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return def
	}
	fs := flag.NewFlagSet("academy serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", or("ACADEMY_ROOT", "."), "OpsForge repository root (ACADEMY_ROOT)")
	addr := fs.String("addr", or("ACADEMY_ADDR", "127.0.0.1:8090"), "listen address, loopback only (ACADEMY_ADDR)")
	db := fs.String("db", getenv("ACADEMY_DB"), "SQLite database (ACADEMY_DB, default <root>/.opsforge/academy.db)")
	workspaces := fs.String("workspaces", getenv("ACADEMY_WORKSPACES"),
		"workspaces folder (ACADEMY_WORKSPACES, default <root>/.opsforge/workspaces)")
	toolchain := fs.String("toolchain", getenv("ACADEMY_TOOLCHAIN"), "opsforge CLI (default <root>/opsforge)")
	toolchainTimeout := fs.Duration("toolchain-timeout", 30*time.Second, "limit for one toolchain call")
	labTimeout := fs.Duration("lab-timeout", 20*time.Minute, "limit for a toolchain call running steps on the lab")
	shutdownTimeout := fs.Duration("shutdown-timeout", 15*time.Second, "grace period on SIGTERM")
	level := fs.String("log-level", or("ACADEMY_LOG_LEVEL", "info"), "debug, info, warn or error (ACADEMY_LOG_LEVEL)")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}

	absRoot, err := filepath.Abs(*root)
	if err != nil {
		return Config{}, fmt.Errorf("root: %w", err)
	}
	cfg := Config{
		Addr: *addr, Root: absRoot, DB: *db, Workspaces: *workspaces, Toolchain: *toolchain,
		ToolchainTimeout: *toolchainTimeout, LabTimeout: *labTimeout, ShutdownTimeout: *shutdownTimeout,
	}
	if cfg.DB == "" {
		cfg.DB = filepath.Join(absRoot, ".opsforge", "academy.db")
	}
	if cfg.Workspaces == "" {
		cfg.Workspaces = filepath.Join(absRoot, ".opsforge", "workspaces")
	}
	if cfg.Toolchain == "" {
		cfg.Toolchain = filepath.Join(absRoot, "opsforge")
	}
	if err := cfg.LogLevel.UnmarshalText([]byte(*level)); err != nil {
		return Config{}, fmt.Errorf("log-level: %w", err)
	}
	return cfg, cfg.validate()
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
	if !filepath.IsAbs(c.DB) || !filepath.IsAbs(c.Toolchain) || !filepath.IsAbs(c.Workspaces) {
		errs = append(errs, errors.New("db, workspaces and toolchain must be absolute paths"))
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
