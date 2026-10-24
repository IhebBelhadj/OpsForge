package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/config"
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
