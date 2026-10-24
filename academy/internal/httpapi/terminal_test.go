package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/IhebBelhadj/opsforge/academy/internal/httpapi"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
	"github.com/IhebBelhadj/opsforge/academy/internal/terminal"
	"github.com/IhebBelhadj/opsforge/academy/internal/terminal/ptyshell"
)

// labServer is the transport over real shells (bash on ptys, as in production), on a loopback listener.
func labServer(t *testing.T) (*httptest.Server, *terminal.Service) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "opsforge"), []byte("#!/bin/sh\necho \"opsforge $*\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	spec, err := ptyshell.Bash(root, filepath.Join(root, ".opsforge"), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	svc := terminal.NewService(ptyshell.Spawner{})
	srv := httptest.NewServer(httpapi.NewHandler(httpapi.Deps{
		Build:     platform.Build{Version: "test"},
		Web:       built,
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Terminals: terminal.NewShells(svc, spec),
	}))
	t.Cleanup(func() {
		svc.Shutdown()
		srv.Close()
	})
	return srv, svc
}

func openTerminal(t *testing.T, srv *httptest.Server, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(srv.URL+"/api/v1/terminals", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func sessionID(t *testing.T, srv *httptest.Server, body string) string {
	t.Helper()
	resp := openTerminal(t, srv, body)
	defer func() { _ = resp.Body.Close() }()
	var s httpapi.TerminalSession
	if resp.StatusCode != http.StatusCreated || json.NewDecoder(resp.Body).Decode(&s) != nil {
		t.Fatalf("open: %d", resp.StatusCode)
	}
	return s.Session
}

func dial(t *testing.T, srv *httptest.Server, id string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/terminal/"+id, nil)
	if err != nil {
		t.Fatalf("dial: %v (%v)", err, resp)
	}
	return c
}

func send(t *testing.T, c *websocket.Conn, msg any) {
	t.Helper()
	data, _ := json.Marshal(msg)
	if err := c.Write(context.Background(), websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
}

// expect reads binary frames until the output holds want; it returns everything read.
func expect(t *testing.T, c *websocket.Conn, want string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out strings.Builder
	for !strings.Contains(out.String(), want) {
		typ, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for %q: %v; got %q", want, err, out.String())
		}
		if typ != websocket.MessageBinary {
			t.Fatalf("output frame of type %v", typ)
		}
		out.Write(data)
	}
	return out.String()
}

// The Phase 10 terminal's behaviour, on the new stack (spike S6): typed input, bracketed paste, resize, reconnect
// with replay, and the end of the shell.
func TestTerminalParityWithPhase10(t *testing.T) {
	srv, _ := labServer(t)
	id := sessionID(t, srv, `{"cols": 100, "rows": 30}`)
	c := dial(t, srv, id)
	expect(t, c, "Lab terminal in")

	send(t, c, map[string]any{"t": "i", "d": "\x1b[200~echo parity-$((6*7))\x1b[201~"})
	send(t, c, map[string]any{"t": "i", "d": "\r"})
	expect(t, c, "parity-42")

	send(t, c, map[string]any{"t": "i", "d": "stty size\r"})
	expect(t, c, "30 100")
	send(t, c, map[string]any{"t": "r", "cols": 120, "rows": 40})
	send(t, c, map[string]any{"t": "i", "d": "stty size\r"})
	expect(t, c, "40 120")

	send(t, c, map[string]any{"t": "i", "d": "opsforge kubectl get nodes\r"})
	expect(t, c, "opsforge kubectl get nodes\r\n")

	_ = c.CloseNow() // the tab dropped its socket
	again := dial(t, srv, id)
	replay := expect(t, again, "parity-42")
	if !strings.Contains(replay, "Lab terminal in") {
		t.Fatalf("replay misses the start of the session: %q", replay)
	}

	send(t, again, map[string]any{"t": "i", "d": "exit\r"})
	_, _, err := again.Read(context.Background())
	for err == nil {
		_, _, err = again.Read(context.Background())
	}
	if websocket.CloseStatus(err) != websocket.StatusNormalClosure || !strings.Contains(err.Error(), "exited") {
		t.Fatalf("close after exit: %v", err)
	}
	resp, err := http.Get(srv.URL + "/ws/terminal/" + id)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("an ended session answers %d", resp.StatusCode)
	}
}

func TestASecondTabTakesTheSessionOver(t *testing.T) {
	srv, _ := labServer(t)
	id := sessionID(t, srv, "")
	first := dial(t, srv, id)
	expect(t, first, "Lab terminal in")
	second := dial(t, srv, id)
	expect(t, second, "Lab terminal in")
	_, _, err := first.Read(context.Background())
	for err == nil {
		_, _, err = first.Read(context.Background())
	}
	if websocket.CloseStatus(err) != httpapi.StatusReplaced {
		t.Fatalf("first tab: %v", err)
	}
}

func TestShutdownClosesTerminalsAsGoingAway(t *testing.T) {
	srv, svc := labServer(t)
	c := dial(t, srv, sessionID(t, srv, ""))
	expect(t, c, "Lab terminal in")
	svc.Shutdown()
	_, _, err := c.Read(context.Background())
	for err == nil {
		_, _, err = c.Read(context.Background())
	}
	if websocket.CloseStatus(err) != websocket.StatusGoingAway {
		t.Fatalf("close: %v", err)
	}
}

func TestOpenTerminalContract(t *testing.T) {
	srv, _ := labServer(t)
	for range terminal.MaxSessions {
		rec := httptest.NewRecorder()
		resp := openTerminal(t, srv, "")
		copyResponse(rec, resp)
		checkContract(t, "POST", "/api/v1/terminals", rec)
	}
	rec := httptest.NewRecorder()
	copyResponse(rec, openTerminal(t, srv, `{"cols": 80, "rows": 24}`))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("past the cap: %d", rec.Code)
	}
	checkContract(t, "POST", "/api/v1/terminals", rec)
}

func TestOpenTerminalRejectsAnAbsurdSize(t *testing.T) {
	srv, _ := labServer(t)
	rec := httptest.NewRecorder()
	copyResponse(rec, openTerminal(t, srv, `{"cols": 0, "rows": 24}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
	checkContract(t, "POST", "/api/v1/terminals", rec)
}

func TestUnknownTerminalSessionIs404(t *testing.T) {
	srv, _ := labServer(t)
	_, resp, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/terminal/nope", nil)
	if err == nil || resp == nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("err %v, resp %v", err, resp)
	}
}

// A page on another site may open a WebSocket to 127.0.0.1: the upgrade must check its Origin.
func TestCrossSiteWebSocketsAreRefused(t *testing.T) {
	srv, _ := labServer(t)
	id := sessionID(t, srv, "")
	_, resp, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/terminal/"+id,
		&websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"https://evil.example"}}})
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("err %v, resp %v", err, resp)
	}
}

// DNS rebinding: a name the attacker controls resolving to 127.0.0.1 makes the browser send Host: that name.
func TestRequestsForAnotherHostAreRefused(t *testing.T) {
	srv, _ := labServer(t)
	for _, host := range []string{"evil.example", "evil.example:" + strings.Split(srv.URL, ":")[2]} {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/terminals", nil)
		req.Host = host
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusMisdirectedRequest {
			t.Fatalf("Host %s: %d", host, resp.StatusCode)
		}
	}
	for _, host := range []string{"localhost:8090", "127.0.0.1:8090", "[::1]:8090"} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/healthz", nil)
		req.Host = host
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Host %s: %d", host, resp.StatusCode)
		}
	}
}

func copyResponse(rec *httptest.ResponseRecorder, resp *http.Response) {
	defer func() { _ = resp.Body.Close() }()
	for k, v := range resp.Header {
		rec.Header()[k] = v
	}
	rec.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(rec, resp.Body)
}
