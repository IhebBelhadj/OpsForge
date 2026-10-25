package terminal_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/terminal"
)

// fakePty is a shell the test drives: Emit is its output, typed input and resizes are recorded.
type fakePty struct {
	r      *io.PipeReader
	w      *io.PipeWriter
	mu     sync.Mutex
	input  bytes.Buffer
	size   [2]uint16
	closed bool
}

func (p *fakePty) Read(b []byte) (int, error) { return p.r.Read(b) }

func (p *fakePty) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.input.Write(b)
}

func (p *fakePty) Resize(cols, rows uint16) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.size = [2]uint16{cols, rows}
	return nil
}

func (p *fakePty) Close() error {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	return p.w.Close()
}

// Emit is output from the shell; Exit ends it as a shell exiting does.
func (p *fakePty) Emit(s string) { _, _ = p.w.Write([]byte(s)) }
func (p *fakePty) Exit()         { _ = p.w.Close() }

func (p *fakePty) state() (string, [2]uint16, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.input.String(), p.size, p.closed
}

type fakeSpawner struct {
	mu    sync.Mutex
	ptys  []*fakePty
	specs []terminal.Spec
}

func (s *fakeSpawner) Spawn(spec terminal.Spec) (terminal.Pty, error) {
	r, w := io.Pipe()
	p := &fakePty{r: r, w: w, size: [2]uint16{spec.Cols, spec.Rows}}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ptys = append(s.ptys, p)
	s.specs = append(s.specs, spec)
	return p, nil
}

func (s *fakeSpawner) pty(i int) *fakePty {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ptys[i]
}

var spec = terminal.Spec{Workspace: "lab-1", Cols: 80, Rows: 24}

func newService() (*terminal.Service, *fakeSpawner) {
	spawner := &fakeSpawner{}
	return terminal.NewService(spawner), spawner
}

// read collects the attachment's output until it holds want (or the output ends).
func read(t *testing.T, a *terminal.Attachment, want string) string {
	t.Helper()
	var got strings.Builder
	for !strings.Contains(got.String(), want) {
		select {
		case chunk, ok := <-a.Output():
			if !ok {
				t.Fatalf("output ended (%s) with %q, want %q", a.Reason(), got.String(), want)
			}
			got.Write(chunk)
		case <-time.After(time.Second):
			t.Fatalf("got %q, want %q", got.String(), want)
		}
	}
	return got.String()
}

// ended waits for the attachment's output to close and returns why.
func ended(t *testing.T, a *terminal.Attachment) string {
	t.Helper()
	for {
		select {
		case _, ok := <-a.Output():
			if !ok {
				return a.Reason()
			}
		case <-time.After(time.Second):
			t.Fatal("output did not end")
		}
	}
}

func TestOutputInputAndResizeFlowThroughAnAttachment(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		svc, spawner := newService()
		defer svc.Shutdown()
		id, err := svc.Open(spec)
		if err != nil {
			t.Fatal(err)
		}
		a, err := svc.Attach(id)
		if err != nil {
			t.Fatal(err)
		}
		spawner.pty(0).Emit("lab $ ")
		read(t, a, "lab $ ")
		if err := a.Write([]byte("ls\r")); err != nil {
			t.Fatal(err)
		}
		if err := a.Resize(120, 40); err != nil {
			t.Fatal(err)
		}
		if input, size, _ := spawner.pty(0).state(); input != "ls\r" || size != [2]uint16{120, 40} {
			t.Fatalf("input %q size %v", input, size)
		}
	})
}

func TestAReconnectingClientGetsTheRecentOutputFirst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		svc, spawner := newService()
		defer svc.Shutdown()
		id, _ := svc.Open(spec)
		first, _ := svc.Attach(id)
		spawner.pty(0).Emit("before ")
		read(t, first, "before ")
		first.Close()

		spawner.pty(0).Emit("while away ") // output with nobody attached is kept too
		synctest.Wait()
		second, err := svc.Attach(id)
		if err != nil {
			t.Fatal(err)
		}
		spawner.pty(0).Emit("after")
		if got := read(t, second, "after"); got != "before while away after" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestReplayKeepsTheLast64KiB(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		svc, spawner := newService()
		defer svc.Shutdown()
		id, _ := svc.Open(spec)
		out := strings.Repeat("a", 50<<10) + strings.Repeat("b", 50<<10) + "END"
		spawner.pty(0).Emit(out)
		synctest.Wait()
		a, _ := svc.Attach(id)
		replay := <-a.Output()
		if len(replay) != terminal.ReplayBytes || string(replay) != out[len(out)-terminal.ReplayBytes:] {
			t.Fatalf("replay of %d bytes, ending %q", len(replay), replay[len(replay)-8:])
		}
	})
}

func TestASessionWithoutClientEndsAfterLinger(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		svc, spawner := newService()
		defer svc.Shutdown()
		id, _ := svc.Open(spec)
		a, _ := svc.Attach(id)
		a.Close()

		time.Sleep(terminal.Linger - time.Second)
		synctest.Wait()
		if _, _, closed := spawner.pty(0).state(); closed {
			t.Fatal("hung up before the linger time")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if _, _, closed := spawner.pty(0).state(); !closed {
			t.Fatal("still running after the linger time")
		}
		if _, err := svc.Attach(id); !errors.Is(err, terminal.ErrNotFound) {
			t.Fatalf("attach after linger: %v", err)
		}
	})
}

func TestReattachingCancelsTheLinger(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		svc, spawner := newService()
		defer svc.Shutdown()
		id, _ := svc.Open(spec)
		a, _ := svc.Attach(id)
		a.Close()
		time.Sleep(terminal.Linger / 2)
		if _, err := svc.Attach(id); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * terminal.Linger)
		synctest.Wait()
		if _, _, closed := spawner.pty(0).state(); closed {
			t.Fatal("an attached session was hung up")
		}
	})
}

func TestEachDisconnectGetsAFullLinger(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		svc, spawner := newService()
		defer svc.Shutdown()
		id, _ := svc.Open(spec)
		a, _ := svc.Attach(id)
		a.Close() // first linger would end at 60 s
		time.Sleep(30 * time.Second)
		b, _ := svc.Attach(id)
		time.Sleep(10 * time.Second)
		b.Close() // second linger ends at 100 s
		time.Sleep(50 * time.Second)
		synctest.Wait()
		if _, _, closed := spawner.pty(0).state(); closed {
			t.Fatal("hung up by the first disconnect's timer")
		}
		time.Sleep(10 * time.Second)
		synctest.Wait()
		if _, _, closed := spawner.pty(0).state(); !closed {
			t.Fatal("still running a full linger after the last disconnect")
		}
	})
}

func TestASessionNeverAttachedLingersToo(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		svc, spawner := newService()
		defer svc.Shutdown()
		if _, err := svc.Open(spec); err != nil {
			t.Fatal(err)
		}
		time.Sleep(terminal.Linger)
		synctest.Wait()
		if _, _, closed := spawner.pty(0).state(); !closed {
			t.Fatal("an unclaimed session was kept")
		}
	})
}

func TestASecondClientReplacesTheFirst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		svc, _ := newService()
		defer svc.Shutdown()
		id, _ := svc.Open(spec)
		first, _ := svc.Attach(id)
		if _, err := svc.Attach(id); err != nil {
			t.Fatal(err)
		}
		if reason := ended(t, first); reason != terminal.ReasonReplaced {
			t.Fatalf("reason %q", reason)
		}
		if err := first.Write([]byte("x")); err == nil {
			t.Fatal("a replaced client can still type")
		}
	})
}

func TestTheShellExitingEndsTheSession(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		svc, spawner := newService()
		defer svc.Shutdown()
		id, _ := svc.Open(spec)
		a, _ := svc.Attach(id)
		spawner.pty(0).Emit("logout\r\n")
		spawner.pty(0).Exit()
		read(t, a, "logout")
		if reason := ended(t, a); reason != terminal.ReasonExited {
			t.Fatalf("reason %q", reason)
		}
		if _, err := svc.Attach(id); !errors.Is(err, terminal.ErrNotFound) {
			t.Fatalf("attach after exit: %v", err)
		}
	})
}

func TestAClientThatFallsBehindIsDetachedButTheSessionStays(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		svc, spawner := newService()
		defer svc.Shutdown()
		id, _ := svc.Open(spec)
		a, _ := svc.Attach(id)
		for range 300 { // more chunks than the queue holds, none read
			spawner.pty(0).Emit("x")
			synctest.Wait()
		}
		if reason := ended(t, a); reason != terminal.ReasonSlow {
			t.Fatalf("reason %q", reason)
		}
		if _, err := svc.Attach(id); err != nil {
			t.Fatalf("the session should linger for a reconnect: %v", err)
		}
	})
}

func TestSessionsAreCapped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		svc, spawner := newService()
		defer svc.Shutdown()
		for range terminal.MaxSessions {
			if _, err := svc.Open(spec); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := svc.Open(spec); !errors.Is(err, terminal.ErrTooMany) {
			t.Fatalf("err = %v", err)
		}
		spawner.pty(0).Exit()
		synctest.Wait()
		if _, err := svc.Open(spec); err != nil {
			t.Fatalf("a slot freed by an exit: %v", err)
		}
	})
}

func TestShutdownHangsUpEveryShell(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		svc, spawner := newService()
		id, _ := svc.Open(spec)
		_, _ = svc.Open(spec)
		a, _ := svc.Attach(id)
		svc.Shutdown()
		if reason := ended(t, a); reason != terminal.ReasonClosed {
			t.Fatalf("reason %q", reason)
		}
		for i := range 2 {
			if _, _, closed := spawner.pty(i).state(); !closed {
				t.Fatalf("pty %d still open", i)
			}
		}
		if _, err := svc.Open(spec); err == nil {
			t.Fatal("opened a session after shutdown")
		}
	})
}

func TestSessionIDsAreUnguessable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		svc, _ := newService()
		defer svc.Shutdown()
		a, _ := svc.Open(spec)
		b, _ := svc.Open(spec)
		if len(a) != 32 || a == b {
			t.Fatalf("ids %q %q", a, b)
		}
	})
}
