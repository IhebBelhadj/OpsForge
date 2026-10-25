package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/config"
	"github.com/IhebBelhadj/opsforge/academy/internal/legacy"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

// The wired application end to end: real SQLite in a temp folder, a fake toolchain script, a real listener.
func TestServeAnswersHealthAndReadinessThenShutsDownCleanly(t *testing.T) {
	dir := t.TempDir()
	toolchain := filepath.Join(dir, "opsforge")
	if err := os.WriteFile(toolchain, []byte("#!/bin/sh\necho '{\"contract\": 1}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load([]string{"--addr", "127.0.0.1:0", "--root", dir, "--log-level", "error"},
		func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	app, cleanup, err := initApp(ctx, cfg, platform.Build{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	app.ready = make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	base := "http://" + <-app.ready

	for path, want := range map[string]string{"/healthz": "ok", "/readyz": "ready"} {
		resp, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Status string `json:"status"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || body.Status != want {
			t.Fatalf("%s: %d %+v", path, resp.StatusCode, body)
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no shutdown within 5 s")
	}
	cleanup()
	if _, err := os.Stat(filepath.Join(dir, ".opsforge", "academy.db")); err != nil {
		t.Fatalf("database: %v", err)
	}
}

func TestVersionCommand(t *testing.T) {
	if code := run([]string{"version"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
}

func TestBadFlagsExitWithUsageError(t *testing.T) {
	if code := run([]string{"serve", "--addr", "0.0.0.0:1"}); code != 2 {
		t.Fatalf("exit %d", code)
	}
}

// migrate-legacy through the wired graph: the Phase 10 fixture, then the same file again.
func TestMigrateLegacyImportsOnce(t *testing.T) {
	dir := t.TempDir()
	script, err := os.ReadFile("../../testdata/labs.phase10.sql")
	if err != nil {
		t.Fatal(err)
	}
	legacyDB := filepath.Join(dir, "labs.db")
	db, err := sql.Open("sqlite", "file:"+legacyDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(script)); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	cfg, err := config.Load([]string{"--root", dir, "--legacy-db", legacyDB, "--log-level", "error"},
		func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	app, cleanup, err := initApp(ctx, cfg, platform.Build{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if r, err := app.MigrateLegacy(ctx); err != nil || r.Attempts != 2 || r.Events != 7 || r.Awards != 1 {
		t.Fatalf("first run: %+v %v", r, err)
	}
	if r, err := app.MigrateLegacy(ctx); err != nil || r != (legacy.Report{}) {
		t.Fatalf("second run: %+v %v", r, err)
	}
}
