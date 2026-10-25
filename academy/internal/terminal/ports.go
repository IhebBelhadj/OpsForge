package terminal

import "io"

// Pty is a running shell on a pseudo-terminal: reading gives its output, writing types into it.
type Pty interface {
	io.ReadWriter
	Resize(cols, rows uint16) error
	// Close hangs up the shell (SIGHUP) and releases the terminal; reads then end.
	Close() error
}

// Spawner starts shells (adapter: opsforgeapi.Shells, a shell on the lab host).
type Spawner interface {
	Spawn(spec Spec) (Pty, error)
}
