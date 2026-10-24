package config_test

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/config"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestDefaultsDeriveFromTheRepositoryRoot(t *testing.T) {
	cfg, err := config.Load(nil, env(map[string]string{"ACADEMY_ROOT": "/repo"}))
	if err != nil {
		t.Fatal(err)
	}
	want := config.Config{
		Addr: "127.0.0.1:8090", Root: "/repo", DB: "/repo/.opsforge/academy.db", Toolchain: "/repo/opsforge",
		ToolchainTimeout: 30 * time.Second, LabTimeout: 20 * time.Minute, ShutdownTimeout: 15 * time.Second,
		LogLevel: slog.LevelInfo,
	}
	if cfg != want {
		t.Fatalf("got %+v\nwant %+v", cfg, want)
	}
}

func TestFlagsWinOverEnvironment(t *testing.T) {
	cfg, err := config.Load(
		[]string{"--addr", "localhost:9000", "--db", "/tmp/a.db", "--log-level", "debug"},
		env(map[string]string{"ACADEMY_ROOT": "/repo", "ACADEMY_ADDR": "127.0.0.1:1", "ACADEMY_LOG_LEVEL": "warn"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "localhost:9000" || cfg.DB != "/tmp/a.db" || cfg.LogLevel != slog.LevelDebug {
		t.Fatalf("got %+v", cfg)
	}
}

func TestInvalidConfigurationIsRejected(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"remote address":   {[]string{"--addr", "0.0.0.0:8090"}, "loopback"},
		"no port":          {[]string{"--addr", "127.0.0.1"}, "addr"},
		"unknown level":    {[]string{"--log-level", "loud"}, "log-level"},
		"zero timeout":     {[]string{"--toolchain-timeout", "0s"}, "toolchain-timeout"},
		"zero lab timeout": {[]string{"--lab-timeout", "0s"}, "lab-timeout"},
		"unknown flag":     {[]string{"--nope"}, "nope"},
		"positional args":  {[]string{"extra"}, "unexpected"},
		"relative db path": {[]string{"--db", "academy.db"}, "absolute"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := config.Load(tc.args, env(map[string]string{"ACADEMY_ROOT": "/repo"}))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}
