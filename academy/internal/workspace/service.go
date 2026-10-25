package workspace

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"unicode/utf8"

	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

// Service manages users' workspaces.
type Service struct {
	files FileStore
	repo  Repo
	clock platform.Clock
}

// NewService keeps trees in files and owners in repo.
func NewService(files FileStore, repo Repo, clock platform.Clock) *Service {
	return &Service{files: files, repo: repo, clock: clock}
}

// Create makes user's workspace ws holding files (path → content).
func (s *Service) Create(ctx context.Context, user int64, ws string, files map[string]string) error {
	if !ValidID(ws) {
		return fmt.Errorf("workspace %q: %w", ws, ErrBadPath)
	}
	var total int64
	clean := make(map[string]string, len(files))
	for p, content := range files {
		c, err := CleanPath(p)
		if err != nil {
			return fmt.Errorf("file %q: %w", p, err)
		}
		if len(content) > MaxFile {
			return fmt.Errorf("file %q: %w", p, ErrTooLarge)
		}
		total += int64(len(content))
		clean[c] = content
	}
	if total > MaxTotal {
		return ErrTooLarge
	}
	if err := s.files.Create(ctx, ws); err != nil {
		return err
	}
	paths := make([]string, 0, len(clean))
	for p := range clean {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if err := s.files.Write(ctx, ws, p, []byte(clean[p])); err != nil {
			return err
		}
	}
	return s.repo.Create(ctx, ws, user, s.clock.Now())
}

// own checks that ws is user's.
func (s *Service) own(ctx context.Context, user int64, ws string) error {
	if !ValidID(ws) {
		return ErrNotFound
	}
	owner, err := s.repo.Owner(ctx, ws)
	if err != nil {
		return err
	}
	if owner != user {
		return ErrNotFound
	}
	return nil
}

// Workdir is user's workspace ws for a terminal to start in (ErrNotFound when it is another user's or none):
// the shell runs on the lab host, in the workspace's folder there.
func (s *Service) Workdir(ctx context.Context, user int64, ws string) (string, error) {
	if err := s.own(ctx, user, ws); err != nil {
		return "", err
	}
	return ws, nil
}

// Tree lists user's workspace ws.
func (s *Service) Tree(ctx context.Context, user int64, ws string) (Tree, error) {
	if err := s.own(ctx, user, ws); err != nil {
		return Tree{}, err
	}
	files, err := s.files.List(ctx, ws)
	if err != nil {
		return Tree{}, err
	}
	t := Tree{Files: files}
	for _, f := range files {
		t.Bytes += f.Size
	}
	return t, nil
}

// Read returns a text file of user's workspace ws.
func (s *Service) Read(ctx context.Context, user int64, ws, p string) (File, error) {
	if err := s.own(ctx, user, ws); err != nil {
		return File{}, err
	}
	c, err := CleanPath(p)
	if err != nil {
		return File{}, err
	}
	data, err := s.files.Read(ctx, ws, c)
	if err != nil {
		return File{}, err
	}
	if !utf8.Valid(data) {
		return File{}, ErrNotText
	}
	return File{Path: c, Content: string(data), Hash: Hash(data)}, nil
}

// Write saves a file of user's workspace ws. With base, the save goes through only if the file is still that
// version ("" = the file must not exist yet), so an editor never overwrites what the terminal changed.
func (s *Service) Write(ctx context.Context, user int64, ws, p, content string, base *string) (File, error) {
	if err := s.own(ctx, user, ws); err != nil {
		return File{}, err
	}
	c, err := CleanPath(p)
	if err != nil {
		return File{}, err
	}
	if len(content) > MaxFile {
		return File{}, ErrTooLarge
	}
	current, err := s.files.Read(ctx, ws, c)
	exists := err == nil
	if err != nil && !errors.Is(err, ErrNotFound) {
		return File{}, err
	}
	if base != nil && ((*base == "" && exists) || (*base != "" && (!exists || Hash(current) != *base))) {
		return File{}, ErrConflict
	}
	files, err := s.files.List(ctx, ws)
	if err != nil {
		return File{}, err
	}
	total := int64(len(content)) - int64(len(current))
	for _, f := range files {
		total += f.Size
	}
	if total > MaxTotal {
		return File{}, ErrTooLarge
	}
	if err := s.files.Write(ctx, ws, c, []byte(content)); err != nil {
		return File{}, err
	}
	return File{Path: c, Content: content, Hash: Hash([]byte(content))}, nil
}

// Delete removes a file of user's workspace ws.
func (s *Service) Delete(ctx context.Context, user int64, ws, p string) error {
	if err := s.own(ctx, user, ws); err != nil {
		return err
	}
	c, err := CleanPath(p)
	if err != nil {
		return err
	}
	return s.files.Remove(ctx, ws, c)
}

// Move renames a file of user's workspace ws; ErrExists if the target exists.
func (s *Service) Move(ctx context.Context, user int64, ws, from, to string) error {
	if err := s.own(ctx, user, ws); err != nil {
		return err
	}
	f, err := CleanPath(from)
	if err != nil {
		return err
	}
	t, err := CleanPath(to)
	if err != nil {
		return err
	}
	if _, err := s.files.Read(ctx, ws, t); err == nil {
		return ErrExists
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	return s.files.Rename(ctx, ws, f, t)
}
