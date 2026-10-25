package opsforgeapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/lab"
	"github.com/IhebBelhadj/opsforge/academy/internal/opsforgeapi"
	"github.com/IhebBelhadj/opsforge/academy/internal/terminal"
	"github.com/IhebBelhadj/opsforge/academy/internal/workspace"
)

// real is a client of a real `opsforge api serve` (ACADEMY_OPSFORGE_URL and ACADEMY_OPSFORGE_TOKEN_FILE, as
// scripts/with-opsforge-api.sh sets them for `make academy-test`); the test is skipped without one. The lab need
// not be up: lab work runs host steps.
func real(t *testing.T) *opsforgeapi.Client {
	t.Helper()
	url, tokenFile := os.Getenv("ACADEMY_OPSFORGE_URL"), os.Getenv("ACADEMY_OPSFORGE_TOKEN_FILE")
	if url == "" || tokenFile == "" {
		t.Skip("no opsforge api serve (scripts/with-opsforge-api.sh go test ./...)")
	}
	c, err := opsforgeapi.New(url, opsforgeapi.TokenFile(tokenFile), 30*time.Second, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func echo(name string, value int) json.RawMessage {
	data, _ := json.Marshal(map[string]any{"name": name, "run": map[string]any{"cmd": "echo " + string(rune('0'+value))}})
	return data
}

func TestRealOpsForge(t *testing.T) {
	c := real(t)
	ctx := context.Background()
	if err := c.Ready(ctx); err != nil {
		t.Fatalf("ready: %v", err)
	}

	errs, err := c.CheckSteps(ctx, []lab.StepSet{
		{Steps: []any{map[string]any{"sleep": "1s"}}},
		{Steps: []any{map[string]any{"partition": "c"}}, NoFaults: true},
	})
	if err != nil || len(errs) != 1 || errs[0].Set != 1 || !strings.Contains(errs[0].Message, "fault steps") {
		t.Fatalf("steps %+v %v", errs, err)
	}

	res, err := c.Run(ctx, lab.RunRequest{Phase: lab.Setup, Steps: []json.RawMessage{echo("x", 7)}})
	if err != nil || res.Error != "" || res.Metrics["x.value"] != 7 || len(res.Log) == 0 {
		t.Fatalf("run %+v %v", res, err)
	}
	probe, err := c.Probe(ctx, lab.ProbeRequest{Steps: []json.RawMessage{echo("a", 1), echo("b", 2)},
		Expect: json.RawMessage(`{"a.value": "== 0", "b.value": "== 2"}`)})
	if err != nil || probe.Passed || len(probe.Results) != 2 || probe.Results[1].Checked {
		t.Fatalf("probe stops at the first failing named step: %+v %v", probe, err)
	}
	var p *opsforgeapi.Problem
	if _, err := c.Run(ctx, lab.RunRequest{Phase: lab.Setup, Namespace: "kube-system"}); !errors.As(err, &p) ||
		p.Status != 400 || p.Slug() != "bad-input" {
		t.Fatalf("a namespace the platform does not own: %v", err)
	}
}

func TestRealWorkspaceAndShell(t *testing.T) {
	c := real(t)
	ctx := context.Background()
	files := c.Files()
	ws := "lab-it-" + time.Now().Format("150405")
	if err := files.Create(ctx, ws); err != nil {
		t.Fatal(err)
	}
	if err := files.Write(ctx, ws, "k8s/hello.yaml", []byte("kind: ConfigMap\n")); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../escape", "a//b"} {
		if err := files.Write(ctx, ws, bad, []byte("x")); !errors.Is(err, workspace.ErrBadPath) {
			t.Errorf("%s: %v", bad, err)
		}
	}

	// bash on the lab host, in the workspace: Phase 10's terminal (paste, a resize, job control, exit)
	shell, err := c.Shells().Spawn(terminal.Spec{Workspace: ws, Cols: 100, Rows: 30})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = shell.Close() }()
	readUntil(t, shell, "Lab terminal in")
	_, _ = shell.Write([]byte("\x1b[200~echo parity-$((6*7))\x1b[201~\r"))
	readUntil(t, shell, "parity-42")
	_, _ = shell.Write([]byte("stty size; cat k8s/hello.yaml; type opsforge | head -1\r"))
	out := readUntil(t, shell, "opsforge is a function")
	if !strings.Contains(out, "30 100") || !strings.Contains(out, "kind: ConfigMap") {
		t.Fatalf("shell: %q", out)
	}
	if err := shell.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	_, _ = shell.Write([]byte("stty size; set -o | grep -q 'monitor *on' && echo job-control\r"))
	readUntil(t, shell, "job-control")
	_, _ = shell.Write([]byte("echo made-here > new.txt; exit\r"))
	buf := make([]byte, 4096)
	for {
		if _, err := shell.Read(buf); err != nil {
			break
		}
	}
	if got, err := files.Read(ctx, ws, "new.txt"); err != nil || string(got) != "made-here\n" {
		t.Fatalf("a file made in the terminal: %q %v", got, err)
	}
}
