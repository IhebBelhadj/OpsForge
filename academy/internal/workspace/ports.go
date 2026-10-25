package workspace

import (
	"context"
	"time"
)

// FileStore holds the workspaces' trees (on the lab host: opsforgeapi.Files); paths are already CleanPath'd, but a
// store must still refuse to leave a tree (a symlink made in the terminal).
type FileStore interface {
	// Create makes an empty tree; ErrExists if there is one.
	Create(ctx context.Context, ws string) error
	// List is the tree's regular files, by path.
	List(ctx context.Context, ws string) ([]Entry, error)
	// Read returns a file's content; ErrNotFound if there is none.
	Read(ctx context.Context, ws, path string) ([]byte, error)
	// Write replaces (or creates, with its folders) a file.
	Write(ctx context.Context, ws, path string, data []byte) error
	// Remove deletes a file; ErrNotFound if there is none.
	Remove(ctx context.Context, ws, path string) error
	// Rename moves a file (creating the target's folders); ErrNotFound if there is none.
	Rename(ctx context.Context, ws, from, to string) error
}

// Repo records who owns each workspace.
type Repo interface {
	Create(ctx context.Context, ws string, user int64, at time.Time) error
	// Owner is the workspace's user, or ErrNotFound.
	Owner(ctx context.Context, ws string) (int64, error)
}
