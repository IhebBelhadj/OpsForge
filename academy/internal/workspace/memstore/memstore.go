// Package memstore is a workspace.FileStore in memory, for tests (workspace, httpapi) and the fake OpsForge.
package memstore

import (
	"context"
	"sort"
	"sync"

	"github.com/IhebBelhadj/opsforge/academy/internal/workspace"
)

// Store holds trees by workspace id, files by path.
type Store struct {
	mu    sync.Mutex
	Trees map[string]map[string][]byte
}

var _ workspace.FileStore = (*Store)(nil)

// New returns an empty store.
func New() *Store { return &Store{Trees: map[string]map[string][]byte{}} }

func (m *Store) tree(ws string) (map[string][]byte, error) {
	t, ok := m.Trees[ws]
	if !ok {
		return nil, workspace.ErrNotFound
	}
	return t, nil
}

// Set puts a file in a tree, as a terminal would (tests).
func (m *Store) Set(ws, p string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Trees[ws][p] = data
}

// Delete drops a whole tree.
func (m *Store) Delete(ws string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.Trees[ws]; !ok {
		return workspace.ErrNotFound
	}
	delete(m.Trees, ws)
	return nil
}

// Create makes an empty tree; ErrExists if there is one.
func (m *Store) Create(_ context.Context, ws string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.Trees[ws]; ok {
		return workspace.ErrExists
	}
	m.Trees[ws] = map[string][]byte{}
	return nil
}

// List is the tree's files, by path.
func (m *Store) List(_ context.Context, ws string) ([]workspace.Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, err := m.tree(ws)
	out := []workspace.Entry{}
	for p, d := range t {
		out = append(out, workspace.Entry{Path: p, Size: int64(len(d))})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, err
}

// Read returns a file's content.
func (m *Store) Read(_ context.Context, ws, p string) ([]byte, error) {
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

// Write replaces or creates a file.
func (m *Store) Write(_ context.Context, ws, p string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, err := m.tree(ws)
	if err == nil {
		t[p] = data
	}
	return err
}

// Remove deletes a file.
func (m *Store) Remove(_ context.Context, ws, p string) error {
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

// Rename moves a file; ErrExists onto another one.
func (m *Store) Rename(_ context.Context, ws, from, to string) error {
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
	if _, taken := t[to]; taken {
		return workspace.ErrExists
	}
	delete(t, from)
	t[to] = d
	return nil
}
