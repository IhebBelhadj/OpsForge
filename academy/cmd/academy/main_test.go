package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/config"
	"github.com/IhebBelhadj/opsforge/academy/internal/legacy"
	"github.com/IhebBelhadj/opsforge/academy/internal/opsforgeapi/opsforgefake"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

// testConfig is the wired application's configuration over a fake OpsForge (its token copied into the data folder),
// with an empty content folder.
func testConfig(t *testing.T, dir string, args ...string) config.Config {
	t.Helper()
	fake := httptest.NewServer(opsforgefake.New("token"))
	t.Cleanup(fake.Close)
	if err := os.WriteFile(filepath.Join(dir, "opsforge-token"), []byte("token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(append([]string{"--addr", "127.0.0.1:0", "--data", dir, "--content",
		filepath.Join(dir, "content"), "--opsforge-url", fake.URL, "--log-level", "error"}, args...),
		func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// The wired application end to end: real SQLite in a temp folder, a fake OpsForge, a real listener.
func TestServeAnswersHealthAndReadinessThenShutsDownCleanly(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(t, dir)
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
	if _, err := os.Stat(filepath.Join(dir, "academy.db")); err != nil {
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
	if code := run([]string{"migrate-legacy", "--data", t.TempDir()}); code != 2 {
		t.Fatalf("migrate-legacy without --legacy-db: exit %d", code)
	}
}

// Without OpsForge's token Academy still starts (migrate-legacy needs no OpsForge), and readyz says what is missing.
func TestAMissingTokenIsAReadinessProblem(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(t, dir)
	cfg.OpsForgeTokenFile = filepath.Join(dir, "missing")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app, cleanup, err := initApp(ctx, cfg, platform.Build{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	app.ready = make(chan string, 1)
	go func() { _ = app.Run(ctx) }()
	resp, err := http.Get("http://" + <-app.ready + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(body), "opsforge-token-file") {
		t.Fatalf("%d %s", resp.StatusCode, body)
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
	cfg := testConfig(t, dir, "--legacy-db", legacyDB)
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
