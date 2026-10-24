package opsforgecli_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/lab"
	"github.com/IhebBelhadj/opsforge/academy/internal/opsforgecli"
)

// The test binary doubles as a fake `opsforge`: run with OPSFORGE_FAKE=<mode>, it answers like the CLI would.
func TestMain(m *testing.M) {
	if mode := os.Getenv("OPSFORGE_FAKE"); mode != "" {
		os.Exit(fake(mode, os.Args[1:]))
	}
	os.Exit(m.Run())
}

func fake(mode string, args []string) int {
	switch mode {
	case "args":
		in, _ := io.ReadAll(os.Stdin)
		wd, _ := os.Getwd()
		out, _ := json.Marshal(map[string]any{"contract": 1, "args": args, "cwd": wd, "stdin": string(in)})
		fmt.Println(string(out))
	case "fixture": // prints the file at OPSFORGE_FIXTURE; saves stdin and argv to OPSFORGE_RECORD if set
		if record := os.Getenv("OPSFORGE_RECORD"); record != "" {
			in, _ := io.ReadAll(os.Stdin)
			if err := os.WriteFile(record, []byte(strings.Join(args, " ")+"\n"+string(in)), 0o600); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
		}
		data, err := os.ReadFile(os.Getenv("OPSFORGE_FIXTURE"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		_, _ = os.Stdout.Write(data)
	case "contract2":
		fmt.Println(`{"contract": 2}`)
	case "problem":
		fmt.Fprintln(os.Stderr, `{"contract": 1, "type": "/problems/lab-error", "title": "Lab error", "detail": "lab is down"}`)
		return 1
	case "crash":
		fmt.Fprintln(os.Stderr, "Traceback (most recent call last):\nKeyError: 'x'")
		return 2
	case "garbage":
		fmt.Println("not json")
	case "slow":
		time.Sleep(10 * time.Second)
	}
	return 0
}

// fixture makes the fake print academy/testdata/<name> (one of the documents shared with the Python tests).
func fixture(t *testing.T, name string) {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPSFORGE_FIXTURE", path)
}

func runner(t *testing.T, mode string) *opsforgecli.Runner {
	t.Helper()
	t.Setenv("OPSFORGE_FAKE", mode)
	t.Setenv("GORACE", "atexit_sleep_ms=0") // a -race child otherwise sleeps 1 s on exit
	return opsforgecli.New(os.Args[0], t.TempDir(), 5*time.Second, 5*time.Second)
}

func TestCallPassesTheCommandStdinAndWorkingDirectory(t *testing.T) {
	r := runner(t, "args")
	var out struct {
		Args  []string `json:"args"`
		Cwd   string   `json:"cwd"`
		Stdin string   `json:"stdin"`
	}
	if err := r.Call(context.Background(), "probe", map[string]any{"steps": []any{}}, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Join(out.Args, " ") != "academy probe --json" || out.Cwd != r.Root || out.Stdin != `{"steps":[]}` {
		t.Fatalf("got %+v (root %s)", out, r.Root)
	}
}

func TestCallErrors(t *testing.T) {
	for mode, check := range map[string]func(error) bool{
		"problem": func(err error) bool {
			var p *opsforgecli.Problem
			return errors.As(err, &p) && p.Type == "/problems/lab-error" && p.Detail == "lab is down"
		},
		"contract2": func(err error) bool {
			var c *opsforgecli.ContractError
			return errors.As(err, &c) && c.Got == 2
		},
		"crash":   func(err error) bool { return strings.Contains(err.Error(), "KeyError: 'x'") },
		"garbage": func(err error) bool { return strings.Contains(err.Error(), "not a JSON document") },
	} {
		t.Run(mode, func(t *testing.T) {
			err := runner(t, mode).Call(context.Background(), "state", nil, &struct{}{})
			if err == nil || !check(err) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestCallStopsAtTheTimeout(t *testing.T) {
	t.Setenv("OPSFORGE_FAKE", "slow")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	r := opsforgecli.New(os.Args[0], t.TempDir(), 100*time.Millisecond, time.Minute)
	start := time.Now()
	err := r.Call(context.Background(), "state", nil, &struct{}{})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 3*time.Second {
		t.Fatalf("err = %v after %v", err, time.Since(start))
	}
}

func TestReadyChecksTheContractVersion(t *testing.T) {
	fixture(t, "version.json")
	if err := runner(t, "fixture").Ready(context.Background()); err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if err := runner(t, "contract2").Ready(context.Background()); err == nil {
		t.Fatal("Ready with contract 2: want an error")
	}
}

func TestStateDecodesTheSharedFixtures(t *testing.T) {
	fixture(t, "state.up.json")
	state, err := runner(t, "fixture").State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !state.Up || !state.Fault || state.Layout == nil || *state.Layout != "stretched" {
		t.Fatalf("got %+v", state)
	}
	if l := state.Links["a-c"]; l.Condition != "cut" || l.Live.Loss != "100%" || l.Desired.Loss != "1%" {
		t.Fatalf("link a-c = %+v", l)
	}
	if n := state.Nodes[0]; n.Name != "a-cp1" || n.Status != "Running" || state.Sites["a"].PublicIP == "" {
		t.Fatalf("nodes[0] = %+v, sites = %+v", n, state.Sites)
	}

	fixture(t, "state.down.json")
	if state, err = runner(t, "fixture").State(context.Background()); err != nil || state.Up {
		t.Fatalf("down: %+v, %v", state, err)
	}
}

func TestCompileValidatesTheBundle(t *testing.T) {
	fixture(t, "bundle.golden.json")
	bundle, err := runner(t, "fixture").Compile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Packs []struct {
			ID string `json:"id"`
		} `json:"packs"`
	}
	if err := json.Unmarshal(bundle, &doc); err != nil || len(doc.Packs) != 1 || doc.Packs[0].ID != "demo" {
		t.Fatalf("bundle: %v %+v", err, doc)
	}

	fixture(t, "bundle.invalid.json")
	if _, err := runner(t, "fixture").Compile(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "bundle.schema.json") {
		t.Fatalf("invalid bundle: err = %v", err)
	}
}

// record makes the fake save its argv and stdin; the returned function reads them back.
func record(t *testing.T) func() (args, stdin string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "call")
	t.Setenv("OPSFORGE_RECORD", path)
	return func() (string, string) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		args, stdin, _ := strings.Cut(string(data), "\n")
		return args, stdin
	}
}

func steps(docs ...string) []json.RawMessage {
	out := make([]json.RawMessage, len(docs))
	for i, d := range docs {
		out[i] = json.RawMessage(d)
	}
	return out
}

func TestRunSendsTheContractDocumentAndDecodesTheResult(t *testing.T) {
	fixture(t, "run.golden.json")
	recorded := record(t)
	res, err := runner(t, "fixture").Run(context.Background(), lab.RunRequest{
		Phase: lab.Setup, Steps: steps(`{"name": "x", "run": {"cmd": "echo 42"}}`), Namespace: "lab-7",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Error != "" || res.Metrics["x.value"] != 42 || len(res.Log) != 3 {
		t.Fatalf("got %+v", res)
	}
	args, stdin := recorded()
	want := `{"contract":1,"phase":"setup","steps":[{"name":"x","run":{"cmd":"echo 42"}}],"namespace":"lab-7"}`
	if args != "academy run --json" || stdin != want {
		t.Fatalf("argv %q, stdin %s", args, stdin)
	}
}

func TestProbeKeepsTheExpectationsInTheAuthorsOrder(t *testing.T) {
	fixture(t, "probe.failed.json")
	recorded := record(t)
	res, err := runner(t, "fixture").Probe(context.Background(), lab.ProbeRequest{
		Steps: steps(`{"name": "b"}`), Expect: json.RawMessage(`{"b.value": "== 2", "a.value": "== 0"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed || len(res.Results) != 2 || res.Results[1].Checked || res.Results[1].Got != nil ||
		res.Results[0].Got == nil || *res.Results[0].Got != 1 {
		t.Fatalf("got %+v", res)
	}
	if _, stdin := recorded(); !strings.Contains(stdin, `"expect":{"b.value":"== 2","a.value":"== 0"}`) ||
		strings.Contains(stdin, "namespace") {
		t.Fatalf("stdin %s", stdin)
	}

	fixture(t, "probe.passed.json")
	if res, err = runner(t, "fixture").Probe(context.Background(), lab.ProbeRequest{}); err != nil || !res.Passed {
		t.Fatalf("passed: %+v, %v", res, err)
	}
}

func TestHeal(t *testing.T) {
	fixture(t, "heal.json")
	if healed, err := runner(t, "fixture").Heal(context.Background()); err != nil || !healed {
		t.Fatalf("healed %v, %v", healed, err)
	}
}

// A document that decodes but breaks the contract (a field renamed on one side) must fail loudly, not as zeros.
func TestLabDocumentsAreCheckedAgainstTheSchema(t *testing.T) {
	fixture(t, "state.up.json")
	r := runner(t, "fixture")
	for name, call := range map[string]func() error{
		"run":   func() error { _, err := r.Run(context.Background(), lab.RunRequest{Phase: lab.Setup}); return err },
		"probe": func() error { _, err := r.Probe(context.Background(), lab.ProbeRequest{}); return err },
		"heal":  func() error { _, err := r.Heal(context.Background()); return err },
	} {
		if err := call(); err == nil || !strings.Contains(err.Error(), "toolchain.schema.json") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

// Lab steps wait for the cluster (a wait_until may take minutes): they get the lab timeout, not the call timeout.
func TestLabCallsHaveTheirOwnTimeout(t *testing.T) {
	t.Setenv("OPSFORGE_FAKE", "slow")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	r := opsforgecli.New(os.Args[0], t.TempDir(), time.Minute, 100*time.Millisecond)
	start := time.Now()
	_, err := r.Probe(context.Background(), lab.ProbeRequest{})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 3*time.Second {
		t.Fatalf("err = %v after %v", err, time.Since(start))
	}
}
