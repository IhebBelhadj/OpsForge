package workspace_test

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/workspace"
)

type clock struct{}

func (clock) Now() time.Time { return time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC) }

// memFiles is a FileStore in memory.
type memFiles struct {
	mu    sync.Mutex
	trees map[string]map[string][]byte
}

func (m *memFiles) tree(ws string) (map[string][]byte, error) {
	t, ok := m.trees[ws]
	if !ok {
		return nil, workspace.ErrNotFound
	}
	return t, nil
}

func (m *memFiles) Create(ws string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.trees[ws]; ok {
		return workspace.ErrExists
	}
	m.trees[ws] = map[string][]byte{}
	return nil
}

func (m *memFiles) List(ws string) ([]workspace.Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, err := m.tree(ws)
	var out []workspace.Entry
	for p, d := range t {
		out = append(out, workspace.Entry{Path: p, Size: int64(len(d))})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, err
}

func (m *memFiles) Read(ws, p string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, err := m.tree(ws)
	if err != nil {
		return nil, err
	}
	d, ok := t[p]
	if !ok {
		return nil, workspace.ErrNotFound
	}
	return d, nil
}

func (m *memFiles) Write(ws, p string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, err := m.tree(ws)
	if err == nil {
		t[p] = data
	}
	return err
}

func (m *memFiles) Remove(ws, p string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, err := m.tree(ws)
	if err != nil {
		return err
	}
	if _, ok := t[p]; !ok {
		return workspace.ErrNotFound
	}
	delete(t, p)
	return nil
}

func (m *memFiles) Rename(ws, from, to string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, err := m.tree(ws)
	if err != nil {
		return err
	}
	d, ok := t[from]
	if !ok {
		return workspace.ErrNotFound
	}
	delete(t, from)
	t[to] = d
	return nil
}

func (m *memFiles) Dir(ws string) string { return "/ws/" + ws }

type owners struct {
	mu sync.Mutex
	m  map[string]int64
}

func (o *owners) Create(_ context.Context, ws string, user int64, _ time.Time) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.m[ws] = user
	return nil
}

func (o *owners) Owner(_ context.Context, ws string) (int64, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	u, ok := o.m[ws]
	if !ok {
		return 0, workspace.ErrNotFound
	}
	return u, nil
}

func newService(t *testing.T) (*workspace.Service, *memFiles) {
	t.Helper()
	files := &memFiles{trees: map[string]map[string][]byte{}}
	svc := workspace.NewService(files, &owners{m: map[string]int64{}}, clock{})
	if err := svc.Create(context.Background(), 1, "lab-1", map[string]string{"web.yaml": "kind: Pod\n"}); err != nil {
		t.Fatal(err)
	}
	return svc, files
}

func TestCleanPath(t *testing.T) {
	for in, want := range map[string]string{"web.yaml": "web.yaml", "k8s/web.yaml": "k8s/web.yaml", ".env": ".env"} {
		if got, err := workspace.CleanPath(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "/etc/passwd", "../x", "a/../../x", "a/./b", "a//b", "a/", `a\b`, "a\x00",
		strings.Repeat("x", workspace.MaxPath+1)} {
		if _, err := workspace.CleanPath(bad); !errors.Is(err, workspace.ErrBadPath) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestCreateSeedsTheFiles(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	tree, err := svc.Tree(ctx, 1, "lab-1")
	if err != nil || len(tree.Files) != 1 || tree.Bytes != 10 {
		t.Fatalf("%+v %v", tree, err)
	}
	if dir, err := svc.Dir(ctx, 1, "lab-1"); err != nil || dir != "/ws/lab-1" {
		t.Fatalf("%q %v", dir, err)
	}
	for name, err := range map[string]error{
		"twice":    svc.Create(ctx, 1, "lab-1", nil),
		"bad id":   svc.Create(ctx, 1, "../x", nil),
		"bad file": svc.Create(ctx, 1, "lab-2", map[string]string{"../x": ""}),
		"too big":  svc.Create(ctx, 1, "lab-3", map[string]string{"x": strings.Repeat("x", workspace.MaxFile+1)}),
	} {
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestWorkspacesAreTheirUsers(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	if _, err := svc.Tree(ctx, 2, "lab-1"); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatalf("another user's tree: %v", err)
	}
	if _, err := svc.Read(ctx, 2, "lab-1", "web.yaml"); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatalf("another user's file: %v", err)
	}
	if _, err := svc.Dir(ctx, 1, "Bad/Id"); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatalf("invalid id: %v", err)
	}
}

func TestSavingAgainstAVersion(t *testing.T) {
	svc, files := newService(t)
	ctx := context.Background()
	f, err := svc.Read(ctx, 1, "lab-1", "web.yaml")
	if err != nil || f.Content != "kind: Pod\n" || f.Hash != workspace.Hash([]byte("kind: Pod\n")) {
		t.Fatalf("%+v %v", f, err)
	}
	saved, err := svc.Write(ctx, 1, "lab-1", "web.yaml", "kind: Deployment\n", &f.Hash)
	if err != nil || saved.Hash == f.Hash {
		t.Fatalf("%+v %v", saved, err)
	}
	// The terminal changed it in between: the editor's save on the old version is refused
	files.trees["lab-1"]["web.yaml"] = []byte("edited in vim\n")
	if _, err := svc.Write(ctx, 1, "lab-1", "web.yaml", "x", &saved.Hash); !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("stale base: %v", err)
	}
	empty := ""
	if _, err := svc.Write(ctx, 1, "lab-1", "web.yaml", "x", &empty); !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("create over an existing file: %v", err)
	}
	if _, err := svc.Write(ctx, 1, "lab-1", "new.yaml", "x", &empty); err != nil {
		t.Fatalf("create: %v", err)
	}
	gone := "abc"
	if _, err := svc.Write(ctx, 1, "lab-1", "missing.yaml", "x", &gone); !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("a base for a file that is gone: %v", err)
	}
	if _, err := svc.Write(ctx, 1, "lab-1", "web.yaml", "forced", nil); err != nil {
		t.Fatalf("without a base: %v", err)
	}
}

func TestLimits(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	if _, err := svc.Write(ctx, 1, "lab-1", "big", strings.Repeat("x", workspace.MaxFile+1), nil); !errors.Is(err, workspace.ErrTooLarge) {
		t.Fatalf("big file: %v", err)
	}
	chunk := strings.Repeat("x", workspace.MaxFile)
	for i := range 9 {
		if _, err := svc.Write(ctx, 1, "lab-1", string(rune('a'+i)), chunk, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Write(ctx, 1, "lab-1", "z", chunk, nil); !errors.Is(err, workspace.ErrTooLarge) {
		t.Fatalf("over the workspace limit: %v", err)
	}
	if _, err := svc.Write(ctx, 1, "lab-1", "a", "small", nil); err != nil {
		t.Fatalf("shrinking a file: %v", err)
	}
	if _, err := svc.Write(ctx, 1, "lab-1", "../x", "", nil); !errors.Is(err, workspace.ErrBadPath) {
		t.Fatalf("bad path: %v", err)
	}
}

func TestDeleteMoveAndText(t *testing.T) {
	svc, files := newService(t)
	ctx := context.Background()
	_, _ = svc.Write(ctx, 1, "lab-1", "b.yaml", "b", nil)
	if err := svc.Move(ctx, 1, "lab-1", "web.yaml", "b.yaml"); !errors.Is(err, workspace.ErrExists) {
		t.Fatalf("move onto a file: %v", err)
	}
	if err := svc.Move(ctx, 1, "lab-1", "web.yaml", "k8s/web.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Move(ctx, 1, "lab-1", "x", "../y"); !errors.Is(err, workspace.ErrBadPath) {
		t.Fatalf("bad target: %v", err)
	}
	if err := svc.Delete(ctx, 1, "lab-1", "b.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, 1, "lab-1", "b.yaml"); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
	files.trees["lab-1"]["bin"] = []byte{0xff, 0xfe}
	if _, err := svc.Read(ctx, 1, "lab-1", "bin"); !errors.Is(err, workspace.ErrNotText) {
		t.Fatalf("binary: %v", err)
	}
}
