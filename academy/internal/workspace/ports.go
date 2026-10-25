package workspace

import (
	"context"
	"time"
)

// FileStore holds the workspaces' trees; paths are already CleanPath'd, but a store must still refuse to leave a
// tree (a symlink made in the terminal).
type FileStore interface {
	// Create makes an empty tree; ErrExists if there is one.
	Create(ws string) error
	// List is the tree's regular files, by path.
	List(ws string) ([]Entry, error)
	// Read returns a file's content; ErrNotFound if there is none.
	Read(ws, path string) ([]byte, error)
	// Write replaces (or creates, with its folders) a file.
	Write(ws, path string, data []byte) error
	// Remove deletes a file; ErrNotFound if there is none.
	Remove(ws, path string) error
	// Rename moves a file (creating the target's folders); ErrNotFound if there is none.
	Rename(ws, from, to string) error
	// Dir is the tree's folder on disk (where a terminal starts).
	Dir(ws string) string
}

// Repo records who owns each workspace.
type Repo interface {
	Create(ctx context.Context, ws string, user int64, at time.Time) error
	// Owner is the workspace's user, or ErrNotFound.
	Owner(ctx context.Context, ws string) (int64, error)
}
