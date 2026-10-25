package opsforgeapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/lab"
	"github.com/IhebBelhadj/opsforge/academy/internal/opsforgeapi"
	"github.com/IhebBelhadj/opsforge/academy/internal/opsforgeapi/opsforgefake"
	"github.com/IhebBelhadj/opsforge/academy/internal/terminal"
	"github.com/IhebBelhadj/opsforge/academy/internal/workspace"
)

func client(t *testing.T, url, token string) *opsforgeapi.Client {
	t.Helper()
	c, err := opsforgeapi.New(url, opsforgeapi.StaticToken(token), 5*time.Second, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c.Wait = time.Second
	return c
}

func fake(t *testing.T) (*opsforgefake.Fake, *opsforgeapi.Client) {
	t.Helper()
	f := opsforgefake.New("token")
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, client(t, srv.URL, "token")
}

// golden is one of the pinned contract's golden documents (academy/contract/golden/<name>).
func golden(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "contract", "golden", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// serve answers each "METHOD /path" with a status and a body (problem bodies get their media type).
func serve(t *testing.T, routes map[string]func() (int, string)) *opsforgeapi.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		answer, ok := routes[r.Method+" "+r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		status, body := answer()
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(body, `"/problems/`) && status >= 400 {
			w.Header().Set("Content-Type", "application/problem+json")
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return client(t, srv.URL, "token")
}

func TestNewRefusesWhatIsNotAnHTTPURL(t *testing.T) {
	for _, u := range []string{"127.0.0.1:8091", "ftp://x", "http://"} {
		if _, err := opsforgeapi.New(u, opsforgeapi.StaticToken("t"), time.Second, time.Second); err == nil {
			t.Errorf("%s: no error", u)
		}
	}
}

func TestReadiness(t *testing.T) {
	f, c := fake(t)
	if err := c.Ready(context.Background()); err != nil {
		t.Fatalf("ready: %v", err)
	}
	f.Minor = 7 // newer is fine
	if err := c.Ready(context.Background()); err != nil {
		t.Fatalf("newer minor: %v", err)
	}
	// Below MinMinor (0 today: only reachable once Academy uses an addition) readyz names the release it needs
	old := &opsforgeapi.TooOldError{Got: opsforgeapi.Version{Contract: 1, Minor: 0, OpsForge: "1.0.0"}}
	if !strings.Contains(old.Error(), "academy needs 1.0 (opsforge 1.0.0 or later)") {
		t.Fatalf("too old: %v", old)
	}
	wrong := client(t, c.URL(), "nope")
	var p *opsforgeapi.Problem
	if err := wrong.Ready(context.Background()); !errors.As(err, &p) || p.Status != 401 || p.Slug() != "no-token" {
		t.Fatalf("wrong token: %v", err)
	}
	missing, _ := opsforgeapi.New(c.URL(), opsforgeapi.TokenFile(filepath.Join(t.TempDir(), "token")), time.Second,
		time.Second)
	if err := missing.Ready(context.Background()); err == nil || !strings.Contains(err.Error(), "opsforge-token-file") {
		t.Fatalf("missing token file: %v", err)
	}
}

func TestAnotherMajorIsRefused(t *testing.T) {
	c := serve(t, map[string]func() (int, string){
		"GET /v1/version": func() (int, string) { return 200, `{"contract": 2, "minor": 0, "opsforge": "2.0.0"}` },
		"GET /v1/state": func() (int, string) {
			return 400, `{"contract": 2, "type": "/problems/x", "title": "x", "detail": "x"}`
		},
	})
	var ce *opsforgeapi.ContractError
	if _, err := c.Version(context.Background()); !errors.As(err, &ce) || ce.Got != 2 {
		t.Fatalf("version: %v", err)
	}
	if _, err := c.State(context.Background()); !errors.As(err, &ce) {
		t.Fatalf("problem of another major: %v", err)
	}
}

// The golden documents of the pinned contract decode into the platform's types.
func TestGoldenDocuments(t *testing.T) {
	jobOf := func(output string) func() (int, string) {
		return func() (int, string) {
			var job map[string]any
			_ = json.Unmarshal(golden(t, "job.json"), &job)
			var out any
			_ = json.Unmarshal(golden(t, output), &out)
			job["output"] = out
			data, _ := json.Marshal(job)
			return 200, string(data)
		}
	}
	running := func() (int, string) {
		return 202, `{"contract": 1, "id": "0123456789abcdef", "kind": "run", "status": "running", "log": [],
			"output": null, "problem": null}`
	}
	routes := map[string]func() (int, string){
		"GET /v1/version":               func() (int, string) { return 200, string(golden(t, "version.json")) },
		"GET /v1/state":                 func() (int, string) { return 200, string(golden(t, "state.up.json")) },
		"POST /v1/steps/validate":       func() (int, string) { return 200, string(golden(t, "validate-steps.json")) },
		"POST /v1/runs":                 running,
		"GET /v1/jobs/0123456789abcdef": jobOf("run.golden.json"),
	}
	c := serve(t, routes)
	ctx := context.Background()
	if v, err := c.Version(ctx); err != nil || v.Contract != 1 {
		t.Fatalf("version %+v %v", v, err)
	}
	if s, err := c.State(ctx); err != nil || !s.Up || len(s.Placeholders) == 0 || s.Links["a-c"].Condition != "cut" {
		t.Fatalf("state %+v %v", s, err)
	}
	if errs, err := c.CheckSteps(ctx, []lab.StepSet{{}}); err != nil || len(errs) != 5 || errs[0].Set != 1 {
		t.Fatalf("steps %+v %v", errs, err)
	}
	res, err := c.Run(ctx, lab.RunRequest{Phase: lab.Setup})
	if err != nil || res.Metrics["x.value"] != 42 || res.Error != "" || len(res.Log) == 0 {
		t.Fatalf("run %+v %v", res, err)
	}
	for _, name := range []string{"probe.passed.json", "probe.failed.json"} {
		routes["POST /v1/probes"] = running
		routes["GET /v1/jobs/0123456789abcdef"] = jobOf(name)
		p, err := serve(t, routes).Probe(ctx, lab.ProbeRequest{})
		if err != nil || len(p.Results) == 0 || p.Passed != (name == "probe.passed.json") {
			t.Fatalf("%s: %+v %v", name, p, err)
		}
	}
	routes["POST /v1/heal"] = running
	routes["GET /v1/jobs/0123456789abcdef"] = jobOf("heal.json")
	if healed, err := serve(t, routes).Heal(ctx); err != nil || !healed {
		t.Fatalf("heal %v %v", healed, err)
	}
}

// Outputs are open: a newer OpsForge of the same major may add fields.
func TestOutputsMayGainFields(t *testing.T) {
	c := serve(t, map[string]func() (int, string){
		"GET /v1/version": func() (int, string) {
			return 200, `{"contract": 1, "minor": 3, "opsforge": "1.3.0", "features": ["new"]}`
		},
	})
	if v, err := c.Version(context.Background()); err != nil || v.Minor != 3 {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestInvalidDocumentsAreRefused(t *testing.T) {
	c := serve(t, map[string]func() (int, string){
		"GET /v1/version": func() (int, string) { return 200, `{"contract": 1, "minor": "zero"}` },
	})
	if _, err := c.Version(context.Background()); !errors.Is(err, opsforgeapi.ErrInvalidDocument) {
		t.Fatalf("err = %v", err)
	}
}

func TestJobs(t *testing.T) {
	f, c := fake(t)
	ctx := context.Background()
	f.Run = func(in json.RawMessage) (any, error) {
		if !strings.Contains(string(in), `"namespace":"lab-7"`) || !strings.Contains(string(in), `"contract":1`) {
			t.Errorf("run input %s", in)
		}
		return map[string]any{"metrics": map[string]float64{"x.value": 1}, "log": []string{"0.0s x"}, "error": nil}, nil
	}
	res, err := c.Run(ctx, lab.RunRequest{Phase: lab.Setup, Steps: []json.RawMessage{json.RawMessage(`{"sleep":"1s"}`)},
		Namespace: "lab-7"})
	if err != nil || res.Metrics["x.value"] != 1 {
		t.Fatalf("run %+v %v", res, err)
	}
	f.Heal = func() (bool, error) { return false, errors.New("lab is down") }
	var p *opsforgeapi.Problem
	if _, err := c.Heal(ctx); !errors.As(err, &p) || p.Slug() != "lab-error" || p.Detail != "lab is down" {
		t.Fatalf("failed job: %v", err)
	}
	busy := serve(t, map[string]func() (int, string){
		"POST /v1/heal": func() (int, string) {
			return 409, `{"contract": 1, "type": "/problems/lab-busy", "title": "busy", "detail": "a run job"}`
		},
	})
	if _, err := busy.Heal(ctx); !errors.As(err, &p) || p.Status != 409 || p.Slug() != "lab-busy" {
		t.Fatalf("busy: %v", err)
	}
	lost := serve(t, map[string]func() (int, string){
		"POST /v1/heal": func() (int, string) {
			return 202, `{"contract": 1, "id": "0123456789abcdef", "kind": "heal", "status": "running", "log": [],
				"output": null, "problem": null}`
		},
		"GET /v1/jobs/0123456789abcdef": func() (int, string) {
			return 404, `{"contract": 1, "type": "/problems/not-found", "title": "Not found", "detail": "no job"}`
		},
	})
	if _, err := lost.Heal(ctx); !errors.Is(err, opsforgeapi.ErrJobLost) {
		t.Fatalf("lost: %v", err)
	}
}

func TestAJobOutlivesItsPolls(t *testing.T) {
	polls := 0
	c := serve(t, map[string]func() (int, string){
		"POST /v1/heal": func() (int, string) {
			return 202, `{"contract": 1, "id": "0123456789abcdef", "kind": "heal", "status": "running", "log": [],
				"output": null, "problem": null}`
		},
		"GET /v1/jobs/0123456789abcdef": func() (int, string) {
			polls++
			status, output := "running", "null"
			if polls == 3 {
				status, output = "done", `{"contract": 1, "healed": true}`
			}
			return 200, `{"contract": 1, "id": "0123456789abcdef", "kind": "heal", "status": "` + status +
				`", "log": [], "output": ` + output + `, "problem": null}`
		},
	})
	if healed, err := c.Heal(context.Background()); err != nil || !healed || polls != 3 {
		t.Fatalf("healed %v after %d polls: %v", healed, polls, err)
	}
}

func TestFiles(t *testing.T) {
	_, c := fake(t)
	files, ctx := c.Files(), context.Background()
	if err := files.Create(ctx, "lab-7"); err != nil {
		t.Fatal(err)
	}
	if err := files.Create(ctx, "lab-7"); !errors.Is(err, workspace.ErrExists) {
		t.Fatalf("again: %v", err)
	}
	if err := files.Write(ctx, "lab-7", "k8s/pod one.yaml", []byte("kind: Pod\n")); err != nil {
		t.Fatal(err)
	}
	if err := files.Write(ctx, "lab-7", "empty", nil); err != nil {
		t.Fatal(err)
	}
	if got, err := files.Read(ctx, "lab-7", "k8s/pod one.yaml"); err != nil || string(got) != "kind: Pod\n" {
		t.Fatalf("read %q %v", got, err)
	}
	if list, err := files.List(ctx, "lab-7"); err != nil || len(list) != 2 || list[1].Path != "k8s/pod one.yaml" ||
		list[1].Size != 10 {
		t.Fatalf("list %+v %v", list, err)
	}
	if err := files.Rename(ctx, "lab-7", "empty", "k8s/pod one.yaml"); !errors.Is(err, workspace.ErrExists) {
		t.Fatalf("onto a file: %v", err)
	}
	if err := files.Rename(ctx, "lab-7", "empty", "moved"); err != nil {
		t.Fatal(err)
	}
	if err := files.Remove(ctx, "lab-7", "moved"); err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{
		"read":   func() error { _, err := files.Read(ctx, "lab-7", "moved"); return err }(),
		"list":   func() error { _, err := files.List(ctx, "lab-99"); return err }(),
		"remove": files.Remove(ctx, "lab-7", "moved"),
	} {
		if !errors.Is(err, workspace.ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := files.Write(ctx, "lab-7", "big", make([]byte, workspace.MaxFile+1)); !errors.Is(err, workspace.ErrTooLarge) {
		t.Fatalf("too large: %v", err)
	}
}

// readUntil reads a shell's output until it holds want.
func readUntil(t *testing.T, p terminal.Pty, want string) string {
	t.Helper()
	var out strings.Builder
	buf := make([]byte, 4096)
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("no %q in %q", want, out.String())
		}
		n, err := p.Read(buf)
		out.Write(buf[:n])
		if err != nil && !strings.Contains(out.String(), want) {
			t.Fatalf("waiting for %q: %v; got %q", want, err, out.String())
		}
	}
	return out.String()
}

func TestShells(t *testing.T) {
	f, c := fake(t)
	_ = f.Files.Create(context.Background(), "lab-7")
	p, err := c.Shells().Spawn(terminal.Spec{Workspace: "lab-7", Cols: 100, Rows: 30})
	if err != nil {
		t.Fatal(err)
	}
	readUntil(t, p, "Lab terminal in /workspaces/lab-7")
	_, _ = p.Write([]byte("stty size\r"))
	readUntil(t, p, "30 100")
	if err := p.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	_, _ = p.Write([]byte("stty size\r"))
	readUntil(t, p, "40 120")
	_, _ = p.Write([]byte("exit\r"))
	buf := make([]byte, 4096)
	for {
		if _, err := p.Read(buf); err != nil {
			break
		}
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Shells().Spawn(terminal.Spec{Workspace: "lab-99"}); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatalf("unknown workspace: %v", err)
	}
}
