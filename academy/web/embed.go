// Package web is the built React app (web/dist at the repository root, copied here by make academy), embedded
// into the binary.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS is the web build's root (index.html, assets/). Without a build it holds only .gitkeep.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // the embed directive guarantees dist exists
	}
	return sub
}
