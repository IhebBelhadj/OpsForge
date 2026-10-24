// Package migrations holds the goose SQL migrations, embedded into the binary and applied at start.
package migrations

import "embed"

// FS is every migration, in version order by file name.
//
//go:embed *.sql
var FS embed.FS
