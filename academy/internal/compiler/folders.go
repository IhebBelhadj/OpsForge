package compiler

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrNotAPack is a folder without a pack.yml.
var ErrNotAPack = errors.New("not a pack (a pack is a folder with a pack.yml)")

// Sources are open pack folders: each is read through an os.Root, so no link leads out of its pack.
type Sources struct {
	List  []Source
	roots []*os.Root
}

// Close releases the folders.
func (s *Sources) Close() {
	for _, r := range s.roots {
		_ = r.Close()
	}
}

// Packs opens the packs of the content folder: its sub-folders with a pack.yml, in name order (none if the folder
// does not exist).
func Packs(dir string) (*Sources, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return &Sources{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("content: %w", err)
	}
	var paths []string
	for _, e := range entries {
		if info, err := os.Stat(filepath.Join(dir, e.Name(), "pack.yml")); err == nil && info.Mode().IsRegular() {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	return Folders(dir, paths)
}

// Folders opens the pack folders at paths; each is named in the bundle relative to the content folder dir when it
// is inside it, else by its absolute path.
func Folders(dir string, paths []string) (*Sources, error) {
	s := &Sources{}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			s.Close()
			return nil, err
		}
		if _, err := os.Stat(filepath.Join(abs, "pack.yml")); err != nil {
			s.Close()
			return nil, fmt.Errorf("%s: %w", p, ErrNotAPack)
		}
		root, err := os.OpenRoot(abs)
		if err != nil {
			s.Close()
			return nil, err
		}
		name := abs
		if rel, err := filepath.Rel(absDir, abs); err == nil && !strings.HasPrefix(rel, "..") {
			name = filepath.ToSlash(rel)
		}
		s.roots = append(s.roots, root)
		s.List = append(s.List, Source{Path: name, FS: root.FS()})
	}
	return s, nil
}
