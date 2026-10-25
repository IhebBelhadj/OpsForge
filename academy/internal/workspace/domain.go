// Package workspace is the learner's files: one tree per lab attempt, kept on the lab host (OpsForge's
// `/v1/workspaces/<id>`, opsforgeapi.Files) and seeded with the lab's files, shared by the editor (the API) and the
// terminal (its shell starts there). Paths are confined to the tree (no `..`, no absolute paths, symlinks never followed out of it) and sizes capped
// (docs/courses/architecture.md, "Security").
package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path"
	"regexp"
	"strings"
)

// Limits of one workspace.
const (
	MaxFile  = 1 << 20  // bytes per file
	MaxTotal = 10 << 20 // bytes per workspace
	MaxPath  = 512      // bytes per path
)

var (
	// ErrNotFound is a workspace that does not exist or is another user's, or a file that does not exist.
	ErrNotFound = errors.New("not found")
	// ErrBadPath is a path outside the workspace or not a plain relative file path.
	ErrBadPath = errors.New("not a path inside the workspace")
	// ErrTooLarge is a file over MaxFile or a workspace over MaxTotal.
	ErrTooLarge = errors.New("over the workspace's size limits")
	// ErrConflict is a save based on a version of the file that is no longer the current one.
	ErrConflict = errors.New("the file changed since it was read")
	// ErrExists is creating a workspace, or moving a file, onto one that exists.
	ErrExists = errors.New("already exists")
	// ErrNotText is reading a file that is not UTF-8 text (made in the terminal).
	ErrNotText = errors.New("not a text file")
)

var validID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// ValidID reports whether id can name a workspace (its folder).
func ValidID(id string) bool { return validID.MatchString(id) }

// CleanPath is p as a path relative to the workspace root, or ErrBadPath: no absolute path, no `..`, no empty or
// `.` segment, no backslash or NUL, at most MaxPath bytes.
func CleanPath(p string) (string, error) {
	if p == "" || len(p) > MaxPath || strings.ContainsAny(p, "\\\x00") || strings.HasPrefix(p, "/") {
		return "", ErrBadPath
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", ErrBadPath
		}
	}
	return path.Clean(p), nil
}

// Hash is the version of a file's content the editor saves against (hex SHA-256).
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Entry is a file in a workspace's tree.
type Entry struct {
	Path string
	Size int64
}

// Tree is a workspace's files (regular files only, by path) and its size.
type Tree struct {
	Files []Entry
	Bytes int64
}

// File is a text file with its version.
type File struct {
	Path    string
	Content string
	Hash    string
}
