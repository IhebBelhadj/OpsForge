package terminal

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// ErrDetached is input or a resize from a client that is no longer attached.
var ErrDetached = errors.New("terminal client is detached")

// errShutdown is a session opened while the platform stops.
var errShutdown = errors.New("terminals are shutting down")

// Service owns every terminal session. Lock order: Service.mu, then session.mu.
type Service struct {
	spawn    Spawner
	mu       sync.Mutex
	sessions map[string]*session
	closed   bool
}

// NewService runs shells with spawn.
func NewService(spawn Spawner) *Service {
	return &Service{spawn: spawn, sessions: map[string]*session{}}
}

type session struct {
	id     string
	svc    *Service
	pty    Pty
	mu     sync.Mutex
	ring   *ring
	att    *Attachment
	linger *time.Timer
	done   bool
}

// Open starts a shell. It lingers until a client attaches, like a session whose client left.
func (s *Service) Open(spec Spec) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", errShutdown
	}
	if len(s.sessions) >= MaxSessions {
		return "", ErrTooMany
	}
	pty, err := s.spawn.Spawn(spec)
	if err != nil {
		return "", err
	}
	sess := &session{id: newID(), svc: s, pty: pty, ring: newRing(ReplayBytes)}
	s.sessions[sess.id] = sess
	sess.mu.Lock()
	sess.startLingerLocked()
	sess.mu.Unlock()
	go sess.pump()
	return sess.id, nil
}

// Attach connects a client to session id: its output starts with the replay of recent output. A client already
// attached is detached (ReasonReplaced): the newest connection wins, as after a half-dead socket.
func (s *Service) Attach(id string) (*Attachment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[id]
	if sess == nil {
		return nil, ErrNotFound
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.done {
		return nil, ErrNotFound
	}
	if sess.att != nil {
		sess.att.finish(ReasonReplaced)
	}
	if sess.linger != nil {
		sess.linger.Stop()
		sess.linger = nil
	}
	a := &Attachment{s: sess, out: make(chan []byte, outputQueue)}
	if replay := sess.ring.Bytes(); len(replay) > 0 {
		a.out <- replay
	}
	sess.att = a
	return a, nil
}

// Shutdown hangs up every shell; attached clients end with ReasonClosed. No session opens afterwards.
func (s *Service) Shutdown() {
	s.mu.Lock()
	s.closed = true
	sessions := make([]*session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.mu.Unlock()
	for _, sess := range sessions {
		sess.end(ReasonClosed)
	}
}

// pump copies the shell's output into the replay buffer and to the attached client, until the shell ends.
func (sess *session) pump() {
	buf := make([]byte, 32<<10)
	for {
		n, err := sess.pty.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			sess.mu.Lock()
			sess.ring.Write(chunk)
			if a := sess.att; a != nil {
				select {
				case a.out <- chunk:
				default: // the client is too far behind: let it go, it reconnects to the replay
					a.finish(ReasonSlow)
					sess.att = nil
					sess.startLingerLocked()
				}
			}
			sess.mu.Unlock()
		}
		if err != nil { // EOF, or EIO once the shell has exited
			sess.end(ReasonExited)
			return
		}
	}
}

// startLingerLocked ends the session after Linger unless a client attaches first. Holds sess.mu.
func (sess *session) startLingerLocked() {
	sess.linger = time.AfterFunc(Linger, func() {
		sess.mu.Lock()
		claimed := sess.att != nil || sess.done
		sess.mu.Unlock()
		if !claimed {
			sess.end(ReasonExited)
		}
	})
}

// end hangs up the shell once, tells the attached client why and forgets the session.
func (sess *session) end(reason string) {
	sess.mu.Lock()
	if sess.done {
		sess.mu.Unlock()
		return
	}
	sess.done = true
	if sess.linger != nil {
		sess.linger.Stop()
	}
	if sess.att != nil {
		sess.att.finish(reason)
		sess.att = nil
	}
	sess.mu.Unlock()
	_ = sess.pty.Close()
	sess.svc.mu.Lock()
	delete(sess.svc.sessions, sess.id)
	sess.svc.mu.Unlock()
}

// Attachment is one client of a session: it reads Output and sends input and resizes.
type Attachment struct {
	s      *session
	out    chan []byte
	once   sync.Once
	reason string
}

// Output is the shell's output for this client, the replay first. It closes when the client is detached; Reason
// then says why.
func (a *Attachment) Output() <-chan []byte { return a.out }

// Reason is why Output closed (empty while it is open, or after the client's own Close).
func (a *Attachment) Reason() string {
	a.s.mu.Lock()
	defer a.s.mu.Unlock()
	return a.reason
}

// Write types p into the shell.
func (a *Attachment) Write(p []byte) error {
	if !a.attached() {
		return ErrDetached
	}
	_, err := a.s.pty.Write(p)
	return err
}

// Resize sets the terminal's size (the shell gets SIGWINCH).
func (a *Attachment) Resize(cols, rows uint16) error {
	if !a.attached() {
		return ErrDetached
	}
	return a.s.pty.Resize(cols, rows)
}

// Close is the client going away: the session lingers for a reconnect.
func (a *Attachment) Close() {
	a.s.mu.Lock()
	defer a.s.mu.Unlock()
	if a.s.att == a {
		a.s.att = nil
		a.finish("")
		a.s.startLingerLocked()
	}
}

func (a *Attachment) attached() bool {
	a.s.mu.Lock()
	defer a.s.mu.Unlock()
	return a.s.att == a && !a.s.done
}

// finish closes the output once. Holds the session's mu.
func (a *Attachment) finish(reason string) {
	a.once.Do(func() {
		a.reason = reason
		close(a.out)
	})
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // never fails (crypto/rand panics instead)
	return hex.EncodeToString(b)
}

// Shells opens sessions of one shell (base), sized by the client.
type Shells struct {
	svc  *Service
	base Spec
}

// NewShells serves base's shell from svc.
func NewShells(svc *Service, base Spec) Shells { return Shells{svc: svc, base: base} }

// Open starts the shell at cols × rows.
func (s Shells) Open(cols, rows uint16) (string, error) {
	spec := s.base
	spec.Cols, spec.Rows = cols, rows
	return s.svc.Open(spec)
}

// Attach connects a client to a session (Service.Attach).
func (s Shells) Attach(id string) (*Attachment, error) { return s.svc.Attach(id) }
