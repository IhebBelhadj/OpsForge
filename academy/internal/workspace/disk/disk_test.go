package disk_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/IhebBelhadj/opsforge/academy/internal/workspace"
	"github.com/IhebBelhadj/opsforge/academy/internal/workspace/disk"
)

func TestStore(t *testing.T) {
	s := disk.Store{Base: filepath.Join(t.TempDir(), "workspaces")}
	if err := s.Create("lab-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Create("lab-1"); !errors.Is(err, workspace.ErrExists) {
		t.Fatalf("twice: %v", err)
	}
	if err := s.Write("lab-1", "k8s/web.yaml", []byte("kind: Pod\n")); err != nil {
		t.Fatal(err)
	}
	if err := s.Write("lab-1", "a.txt", []byte("a")); err != nil {
		t.Fatal(err)
	}
	if data, err := s.Read("lab-1", "k8s/web.yaml"); err != nil || string(data) != "kind: Pod\n" {
		t.Fatalf("%q %v", data, err)
	}
	if err := s.Rename("lab-1", "a.txt", "b/c.txt"); err != nil {
		t.Fatal(err)
	}
	list, err := s.List("lab-1")
	if err != nil || len(list) != 2 || list[0].Path != "b/c.txt" || list[1].Path != "k8s/web.yaml" || list[1].Size != 10 {
		t.Fatalf("%+v %v", list, err)
	}
	if err := s.Remove("lab-1", "b/c.txt"); err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{
		"read missing":   func() error { _, err := s.Read("lab-1", "nope"); return err }(),
		"read a folder":  func() error { _, err := s.Read("lab-1", "k8s"); return err }(),
		"remove missing": s.Remove("lab-1", "nope"),
		"rename missing": s.Rename("lab-1", "nope", "x"),
		"list no tree":   func() error { _, err := s.List("lab-9"); return err }(),
		"write no tree":  s.Write("lab-9", "x", nil),
		"remove no tree": s.Remove("lab-9", "x"),
		"rename no tree": s.Rename("lab-9", "x", "y"),
		"read no tree":   func() error { _, err := s.Read("lab-9", "x"); return err }(),
	} {
		if !errors.Is(err, workspace.ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A symlink made in the terminal must not let the editor read or write outside the workspace.
func TestSymlinksDoNotLeadOut(t *testing.T) {
	base := t.TempDir()
	s := disk.Store{Base: filepath.Join(base, "workspaces")}
	if err := s.Create("lab-1"); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(base, "secret")
	if err := os.WriteFile(secret, []byte("token"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(s.Dir("lab-1"), "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(base, filepath.Join(s.Dir("lab-1"), "up")); err != nil {
		t.Fatal(err)
	}
	if data, err := s.Read("lab-1", "link"); err == nil {
		t.Fatalf("read through a symlink: %q", data)
	}
	if err := s.Write("lab-1", "up/evil", []byte("x")); err == nil {
		t.Fatal("wrote through a symlinked folder")
	}
	if _, err := os.Stat(filepath.Join(base, "evil")); err == nil {
		t.Fatal("a file appeared outside the workspace")
	}
	if list, _ := s.List("lab-1"); len(list) != 0 {
		t.Fatalf("symlinks listed: %+v", list)
	}
}
