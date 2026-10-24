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
	case "fixture": // prints the file at OPSFORGE_FIXTURE
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
	return opsforgecli.New(os.Args[0], t.TempDir(), 5*time.Second)
}

func TestRunPassesTheCommandStdinAndWorkingDirectory(t *testing.T) {
	r := runner(t, "args")
	var out struct {
		Args  []string `json:"args"`
		Cwd   string   `json:"cwd"`
		Stdin string   `json:"stdin"`
	}
	if err := r.Run(context.Background(), "probe", map[string]any{"steps": []any{}}, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Join(out.Args, " ") != "academy probe --json" || out.Cwd != r.Root || out.Stdin != `{"steps":[]}` {
		t.Fatalf("got %+v (root %s)", out, r.Root)
	}
}

func TestRunErrors(t *testing.T) {
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
			err := runner(t, mode).Run(context.Background(), "state", nil, &struct{}{})
			if err == nil || !check(err) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestRunStopsAtTheTimeout(t *testing.T) {
	t.Setenv("OPSFORGE_FAKE", "slow")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	r := opsforgecli.New(os.Args[0], t.TempDir(), 100*time.Millisecond)
	start := time.Now()
	err := r.Run(context.Background(), "state", nil, &struct{}{})
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
