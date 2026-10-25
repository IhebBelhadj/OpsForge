package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/IhebBelhadj/opsforge/academy/internal/httpapi"
	"github.com/IhebBelhadj/opsforge/academy/internal/opsforgeapi"
	"github.com/IhebBelhadj/opsforge/academy/internal/opsforgeapi/opsforgefake"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
	"github.com/IhebBelhadj/opsforge/academy/internal/terminal"
)

// fakeOpsForge is OpsForge's API in memory (an echoing shell, workspaces in memory) and a client of it.
func fakeOpsForge(t *testing.T) (*opsforgefake.Fake, *opsforgeapi.Client) {
	t.Helper()
	fake := opsforgefake.New("token")
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	c, err := opsforgeapi.New(srv.URL, opsforgeapi.StaticToken("token"), 5*time.Second, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return fake, c
}

// shells serves terminals on the lab host of c.
func shells(t *testing.T, c *opsforgeapi.Client) (terminal.Shells, *terminal.Service) {
	t.Helper()
	svc := terminal.NewService(c.Shells())
	t.Cleanup(svc.Shutdown)
	return terminal.NewShells(svc), svc
}

// labServer is the transport with terminals on a fake OpsForge, on a loopback listener (real shells: the
// integration test against `opsforge api serve`, cmd/academy).
func labServer(t *testing.T, opts ...func(*httpapi.Deps)) (*httptest.Server, *terminal.Service) {
	t.Helper()
	_, c := fakeOpsForge(t)
	sh, svc := shells(t, c)
	deps := httpapi.Deps{
		Build:     platform.Build{Version: "test"},
		Web:       built,
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Terminals: sh,
		Identity:  identities(t),
	}
	for _, opt := range opts {
		opt(&deps)
	}
	srv := httptest.NewServer(httpapi.NewHandler(deps))
	t.Cleanup(func() {
		svc.Shutdown()
		srv.Close()
	})
	return srv, svc
}

func openTerminal(t *testing.T, b *browser, body string) *http.Response {
	t.Helper()
	return b.send(t, http.MethodPost, "/api/v1/terminals", body, nil)
}

func sessionID(t *testing.T, b *browser, body string) string {
	t.Helper()
	resp := openTerminal(t, b, body)
	defer func() { _ = resp.Body.Close() }()
	var s httpapi.TerminalSession
	if resp.StatusCode != http.StatusCreated || json.NewDecoder(resp.Body).Decode(&s) != nil {
		t.Fatalf("open: %d", resp.StatusCode)
	}
	return s.Session
}

func wsURL(srv *httptest.Server, id string) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/terminal/" + id
}

// dial connects the page's WebSocket (its cookie and Origin).
func dial(t *testing.T, b *browser, id string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, wsURL(b.srv, id), &websocket.DialOptions{HTTPClient: b.client})
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

// The browser terminal's transport over OpsForge's: typed input and a paste reach the shell, a resize too,
// reconnecting replays, and the end of the shell closes the socket (bash itself: cmd/academy's integration test).
func TestTerminalTransport(t *testing.T) {
	srv, _ := labServer(t)
	b := signIn(t, srv)
	id := sessionID(t, b, `{"cols": 100, "rows": 30}`)
	c := dial(t, b, id)
	expect(t, c, "Lab terminal in")

	send(t, c, map[string]any{"t": "i", "d": "\x1b[200~echo parity-42\x1b[201~"})
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
	again := dial(t, b, id)
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
	resp := b.send(t, http.MethodGet, "/ws/terminal/"+id, "", nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("an ended session answers %d", resp.StatusCode)
	}
}

func TestASecondTabTakesTheSessionOver(t *testing.T) {
	srv, _ := labServer(t)
	b := signIn(t, srv)
	id := sessionID(t, b, "")
	first := dial(t, b, id)
	expect(t, first, "Lab terminal in")
	second := dial(t, b, id)
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
	b := signIn(t, srv)
	c := dial(t, b, sessionID(t, b, ""))
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
	b := signIn(t, srv)
	for range terminal.MaxSessions {
		checkContract(t, "POST", "/api/v1/terminals", record(openTerminal(t, b, "")))
	}
	rec := record(openTerminal(t, b, `{"cols": 80, "rows": 24}`))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("past the cap: %d", rec.Code)
	}
	checkContract(t, "POST", "/api/v1/terminals", rec)
}

func TestOpenTerminalRejectsAnAbsurdSize(t *testing.T) {
	srv, _ := labServer(t)
	rec := record(openTerminal(t, signIn(t, srv), `{"cols": 0, "rows": 24}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
	checkContract(t, "POST", "/api/v1/terminals", rec)
}

func TestUnknownTerminalSessionIs404(t *testing.T) {
	srv, _ := labServer(t)
	b := signIn(t, srv)
	_, resp, err := websocket.Dial(context.Background(), wsURL(srv, "nope"), &websocket.DialOptions{HTTPClient: b.client})
	if err == nil || resp == nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("err %v, resp %v", err, resp)
	}
}

// The terminal is a shell: its WebSocket needs the session cookie like the API.
func TestTerminalSocketsNeedASession(t *testing.T) {
	srv, _ := labServer(t)
	id := sessionID(t, signIn(t, srv), "")
	_, resp, err := websocket.Dial(context.Background(), wsURL(srv, id), nil)
	if err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("err %v, resp %v", err, resp)
	}
}

// A page on another site may open a WebSocket to 127.0.0.1: the upgrade must check its Origin.
func TestCrossSiteWebSocketsAreRefused(t *testing.T) {
	srv, _ := labServer(t)
	b := signIn(t, srv)
	id := sessionID(t, b, "")
	_, resp, err := websocket.Dial(context.Background(), wsURL(srv, id),
		&websocket.DialOptions{HTTPClient: b.client, HTTPHeader: http.Header{"Origin": {"https://evil.example"}}})
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
