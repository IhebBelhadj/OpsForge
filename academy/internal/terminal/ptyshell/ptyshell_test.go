package ptyshell_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/terminal"
	"github.com/IhebBelhadj/opsforge/academy/internal/terminal/ptyshell"
)

// until reads from r until the output holds want, or fails after 5 s.
func until(t *testing.T, r io.Reader, want string) string {
	t.Helper()
	got := make(chan string, 1)
	go func() {
		var out bytes.Buffer
		buf := make([]byte, 4096)
		for !strings.Contains(out.String(), want) {
			n, err := r.Read(buf)
			out.Write(buf[:n])
			if err != nil {
				break
			}
		}
		got <- out.String()
	}()
	select {
	case s := <-got:
		if !strings.Contains(s, want) {
			t.Fatalf("output %q ended without %q", s, want)
		}
		return s
	case <-time.After(5 * time.Second):
		t.Fatalf("no %q within 5 s", want)
		return ""
	}
}

func TestSpawnRunsTheCommandOnASizedTerminal(t *testing.T) {
	dir := t.TempDir()
	pty, err := ptyshell.Spawner{}.Spawn(terminal.Spec{
		Argv: []string{"sh", "-c", `stty size; echo "cwd=$PWD term=$TERM x=$X"; read line; echo "got:$line"; stty size`},
		Dir:  dir, Env: []string{"X=1"}, Cols: 100, Rows: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pty.Close() }()
	until(t, pty, "30 100")
	until(t, pty, "cwd="+dir+" term=xterm-256color x=1")
	if err := pty.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	if _, err := pty.Write([]byte("hi\r")); err != nil {
		t.Fatal(err)
	}
	until(t, pty, "got:hi")
	until(t, pty, "40 120")
}

func TestCloseHangsUpTheShell(t *testing.T) {
	pty, err := ptyshell.Spawner{}.Spawn(terminal.Spec{Argv: []string{"sh", "-c", "echo ready; sleep 60"}, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	until(t, pty, "ready")
	start := time.Now()
	if err := pty.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := pty.Read(make([]byte, 16)); err == nil {
		t.Fatal("read after close succeeded")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("close took %v", time.Since(start))
	}
}

func TestReadsEndWhenTheShellExits(t *testing.T) {
	pty, err := ptyshell.Spawner{}.Spawn(terminal.Spec{Argv: []string{"sh", "-c", "echo bye"}, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pty.Close() }()
	out, _ := io.ReadAll(pty) // EIO after the exit is the end, not an error to report
	if !strings.Contains(string(out), "bye") {
		t.Fatalf("output %q", out)
	}
}

func TestBashSpecRunsTheLabShell(t *testing.T) {
	root := filepath.Join(t.TempDir(), "it's $HOME") // quoting: nothing in the path expands
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "opsforge"), []byte("#!/bin/sh\necho \"opsforge $*\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	spec, err := ptyshell.Bash(root, filepath.Join(root, ".opsforge"), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	pty, err := ptyshell.Spawner{}.Spawn(spec)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pty.Close() }()
	until(t, pty, "Lab terminal in "+root)
	if _, err := pty.Write([]byte("opsforge kubectl get nodes\r")); err != nil {
		t.Fatal(err)
	}
	until(t, pty, "opsforge kubectl get nodes\r\n")
}

func TestBashStartsInTheWorkspaceItIsGiven(t *testing.T) {
	root, ws := t.TempDir(), filepath.Join(t.TempDir(), "lab 1")
	if err := os.Mkdir(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	spec, err := ptyshell.Bash(root, filepath.Join(root, ".opsforge"), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	spec.Dir, spec.Env = ws, []string{"ACADEMY_WORKDIR=" + ws}
	pty, err := ptyshell.Spawner{}.Spawn(spec)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pty.Close() }()
	until(t, pty, "Lab terminal in "+ws)
}
