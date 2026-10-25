// Package config is the platform's one typed configuration: flags over ACADEMY_* environment variables over
// defaults derived from the data folder, validated before anything starts. Academy reads nothing of an OpsForge
// checkout: OpsForge is a URL and a token (docs/PLAN.md Phase 11, split step 5).
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"path/filepath"
	"time"
)

// Config is injected like any dependency (cmd/academy/wire.go).
type Config struct {
	Addr              string        // HTTP listen address; loopback only (the terminal is a shell)
	Data              string        // Academy's own state: the database, OpsForge's token
	Content           string        // the content folder: one pack per sub-folder with a pack.yml
	DB                string        // SQLite database
	OpsForgeURL       string        // OpsForge's API (`opsforge api serve`)
	OpsForgeTokenFile string        // a copy of OpsForge's .opsforge/api-token
	OpsForgeTimeout   time.Duration // default limit for one call to OpsForge
	LabTimeout        time.Duration // limit for lab work (setup, a check, teardown: a job)
	ShutdownTimeout   time.Duration // how long running requests get on SIGTERM
	LegacyDB          string        // Phase 10's labs.db, read by migrate-legacy
	LogLevel          slog.Level
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

// defaultData is $XDG_DATA_HOME/opsforge-academy, else ~/.local/share/opsforge-academy.
func defaultData(getenv func(string) string) string {
	if d := getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "opsforge-academy")
	}
	return filepath.Join(getenv("HOME"), ".local", "share", "opsforge-academy")
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
	data := fs.String("data", or("ACADEMY_DATA", defaultData(getenv)),
		"Academy's state folder (ACADEMY_DATA, default $XDG_DATA_HOME/opsforge-academy)")
	content := fs.String("content", or("ACADEMY_CONTENT", "labs"), "content packs folder (ACADEMY_CONTENT)")
	var compile Compile
	if command == "compile" {
		fs.BoolVar(&compile.JSON, "json", false, "print the bundle")
	}
	addr := fs.String("addr", or("ACADEMY_ADDR", "127.0.0.1:8090"), "listen address, loopback only (ACADEMY_ADDR)")
	db := fs.String("db", getenv("ACADEMY_DB"), "SQLite database (ACADEMY_DB, default <data>/academy.db)")
	opsforgeURL := fs.String("opsforge-url", or("ACADEMY_OPSFORGE_URL", "http://127.0.0.1:8091"),
		"OpsForge's API, `opsforge api serve` (ACADEMY_OPSFORGE_URL)")
	tokenFile := fs.String("opsforge-token-file", getenv("ACADEMY_OPSFORGE_TOKEN_FILE"),
		"OpsForge's API token, a copy of its .opsforge/api-token (ACADEMY_OPSFORGE_TOKEN_FILE, default "+
			"<data>/opsforge-token)")
	legacyDB := fs.String("legacy-db", getenv("ACADEMY_LEGACY_DB"),
		"Phase 10 database for migrate-legacy (ACADEMY_LEGACY_DB)")
	opsforgeTimeout := fs.Duration("opsforge-timeout", 30*time.Second, "limit for one call to OpsForge")
	labTimeout := fs.Duration("lab-timeout", 20*time.Minute, "limit for lab work (one OpsForge job)")
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

	cfg := Config{Addr: *addr, OpsForgeURL: *opsforgeURL, OpsForgeTimeout: *opsforgeTimeout, LabTimeout: *labTimeout,
		ShutdownTimeout: *shutdownTimeout}
	for _, f := range []struct {
		name, in string
		out      *string
		fallback string // under the data folder
	}{
		{"data", *data, &cfg.Data, ""}, {"content", *content, &cfg.Content, ""},
		{"db", *db, &cfg.DB, "academy.db"}, {"opsforge-token-file", *tokenFile, &cfg.OpsForgeTokenFile, "opsforge-token"},
		{"legacy-db", *legacyDB, &cfg.LegacyDB, ""},
	} {
		in := f.in
		if in == "" && f.fallback != "" {
			in = filepath.Join(cfg.Data, f.fallback)
		}
		if in == "" {
			continue
		}
		a, err := filepath.Abs(in)
		if err != nil {
			return Config{}, compile, fmt.Errorf("%s: %w", f.name, err)
		}
		*f.out = a
	}
	if err := cfg.LogLevel.UnmarshalText([]byte(*level)); err != nil {
		return Config{}, compile, fmt.Errorf("log-level: %w", err)
	}
	return cfg, compile, cfg.validate()
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (c Config) validate() error {
	host, _, err := net.SplitHostPort(c.Addr)
	if err != nil {
		return fmt.Errorf("addr %q: %w", c.Addr, err)
	}
	if !loopback(host) {
		return fmt.Errorf("addr %q: must be a loopback address (127.0.0.1 or localhost)", c.Addr)
	}
	var errs []error
	u, err := url.Parse(c.OpsForgeURL)
	switch {
	case err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "":
		errs = append(errs, fmt.Errorf("opsforge-url %q: not an http(s) URL", c.OpsForgeURL))
	case u.Scheme == "http" && !loopback(u.Hostname()):
		errs = append(errs, fmt.Errorf("opsforge-url %q: the token would cross the network in clear: use https "+
			"(opsforge api serve --tls-cert) or an SSH tunnel to a loopback address", c.OpsForgeURL))
	}
	if c.OpsForgeTimeout <= 0 {
		errs = append(errs, errors.New("opsforge-timeout must be positive"))
	}
	if c.LabTimeout <= 0 {
		errs = append(errs, errors.New("lab-timeout must be positive"))
	}
	if c.ShutdownTimeout <= 0 {
		errs = append(errs, errors.New("shutdown-timeout must be positive"))
	}
	return errors.Join(errs...)
}
