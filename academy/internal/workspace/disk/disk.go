// Package disk is the workspace module's FileStore on the local disk: one folder per workspace under Base, every
// access through an os.Root, so neither a path nor a symlink made in the terminal reaches outside the folder.
package disk

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"

	"github.com/IhebBelhadj/opsforge/academy/internal/workspace"
)

// Store keeps workspaces in Base/<id>.
type Store struct {
	Base string
}

var _ workspace.FileStore = Store{}

// Create makes an empty tree; workspace.ErrExists if there is one.
func (s Store) Create(ws string) error {
	if err := os.MkdirAll(s.Base, 0o700); err != nil {
		return err
	}
	err := os.Mkdir(s.Dir(ws), 0o700)
	if errors.Is(err, fs.ErrExist) {
		return workspace.ErrExists
	}
	return err
}

// Dir is the tree's folder.
func (s Store) Dir(ws string) string { return filepath.Join(s.Base, ws) }

func (s Store) open(ws string) (*os.Root, error) {
	r, err := os.OpenRoot(s.Dir(ws))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, workspace.ErrNotFound
	}
	return r, err
}

// mapErr turns the os errors of a confined access into the module's.
func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, fs.ErrNotExist):
		return workspace.ErrNotFound
	case errors.Is(err, fs.ErrExist):
		return workspace.ErrExists
	}
	var pe *fs.PathError
	if errors.As(err, &pe) && pe.Err.Error() == "path escapes from parent" {
		return fmt.Errorf("%w: %s", workspace.ErrBadPath, pe.Path)
	}
	return err
}

// List is the tree's regular files (symlinks and folders left out), by path.
func (s Store) List(ws string) ([]workspace.Entry, error) {
	r, err := s.open(ws)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	var out []workspace.Entry
	err = fs.WalkDir(r.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, workspace.Entry{Path: p, Size: info.Size()})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, err
}

// Read returns a regular file's content.
func (s Store) Read(ws, p string) ([]byte, error) {
	r, err := s.open(ws)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	info, err := r.Stat(p)
	if err != nil {
		return nil, mapErr(err)
	}
	if !info.Mode().IsRegular() {
		return nil, workspace.ErrNotFound
	}
	if info.Size() > workspace.MaxTotal {
		return nil, workspace.ErrTooLarge
	}
	data, err := r.ReadFile(p)
	return data, mapErr(err)
}

// Write replaces or creates a file and its folders.
func (s Store) Write(ws, p string, data []byte) error {
	r, err := s.open(ws)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	if dir := path.Dir(p); dir != "." {
		if err := r.MkdirAll(dir, 0o700); err != nil {
			return mapErr(err)
		}
	}
	return mapErr(r.WriteFile(p, data, 0o600))
}

// Remove deletes a file.
func (s Store) Remove(ws, p string) error {
	r, err := s.open(ws)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	return mapErr(r.Remove(p))
}

// Rename moves a file, creating the target's folders.
func (s Store) Rename(ws, from, to string) error {
	r, err := s.open(ws)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	if _, err := r.Lstat(from); err != nil {
		return mapErr(err)
	}
	if dir := path.Dir(to); dir != "." {
		if err := r.MkdirAll(dir, 0o700); err != nil {
			return mapErr(err)
		}
	}
	return mapErr(r.Rename(from, to))
}
