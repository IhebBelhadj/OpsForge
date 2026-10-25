package opsforgeapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/IhebBelhadj/opsforge/academy/internal/terminal"
)

// Shells is the terminal module's Spawner on OpsForge: each shell is a WebSocket to `GET /v1/terminal` (bash on
// a pty on the lab host, started in the workspace). Sessions, linger and replay stay the terminal module's.
type Shells struct {
	c *Client
}

var _ terminal.Spawner = Shells{}

// Shells returns the terminal spawner.
func (c *Client) Shells() Shells { return Shells{c: c} }

// Spawn opens a shell on the lab host.
func (s Shells) Spawn(spec terminal.Spec) (terminal.Pty, error) {
	u := *s.c.base
	u.Scheme = map[string]string{"http": "ws", "https": "wss"}[u.Scheme]
	u.Path += "/v1/terminal"
	q := url.Values{"cols": {strconv.Itoa(int(spec.Cols))}, "rows": {strconv.Itoa(int(spec.Rows))}}
	if spec.Workspace != "" {
		q.Set("workspace", spec.Workspace)
	}
	u.RawQuery = q.Encode()
	token, err := s.c.token()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.c.Timeout)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{
		HTTPClient: s.c.http,
		HTTPHeader: http.Header{"Authorization": {"Bearer " + token}},
	})
	if err != nil {
		if resp != nil && resp.StatusCode != http.StatusSwitchingProtocols {
			defer func() { _ = resp.Body.Close() }()
			return nil, mapErr(failure(http.MethodGet, "/v1/terminal", resp))
		}
		return nil, fmt.Errorf("opsforge terminal: %w", err)
	}
	conn.SetReadLimit(1 << 20)
	return &remote{conn: conn}, nil
}

// remote is a shell over the WebSocket: binary messages are its output, text messages carry input and resizes
// (Phase 10's frames, as the browser sends them to the platform).
type remote struct {
	conn *websocket.Conn
	rmu  sync.Mutex
	rest []byte // output received but not read yet
	once sync.Once
}

// writeTimeout bounds sending one input or resize frame.
const writeTimeout = 10 * time.Second

var errEnded = fmt.Errorf("shell ended: %w", os.ErrClosed)

// Read returns the shell's output; once the shell has exited (or the socket closed), errEnded.
func (r *remote) Read(b []byte) (int, error) {
	r.rmu.Lock()
	defer r.rmu.Unlock()
	for len(r.rest) == 0 {
		typ, data, err := r.conn.Read(context.Background())
		if err != nil {
			return 0, fmt.Errorf("%w (%w)", errEnded, err)
		}
		if typ == websocket.MessageBinary {
			r.rest = data
		}
	}
	n := copy(b, r.rest)
	r.rest = r.rest[n:]
	return n, nil
}

func (r *remote) send(frame any) error {
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	return r.conn.Write(ctx, websocket.MessageText, data)
}

// Write types p into the shell.
func (r *remote) Write(p []byte) (int, error) {
	if err := r.send(map[string]string{"t": "i", "d": string(p)}); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Resize sets the shell's window size.
func (r *remote) Resize(cols, rows uint16) error {
	return r.send(map[string]any{"t": "r", "cols": cols, "rows": rows})
}

// Close hangs the shell up (OpsForge sends SIGHUP when the socket closes); closing twice is a no-op.
func (r *remote) Close() error {
	r.once.Do(func() { _ = r.conn.CloseNow() })
	return nil
}
