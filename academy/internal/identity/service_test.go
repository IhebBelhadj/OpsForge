package identity_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/identity"
)

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

// memRepo is the Repo in memory.
type memRepo struct {
	mu       sync.Mutex
	users    []identity.User
	sessions map[string]identity.Session
}

func (r *memRepo) EnsureUser(_ context.Context, name string, now time.Time) (identity.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.users {
		if u.Name == name {
			return u, nil
		}
	}
	u := identity.User{ID: int64(len(r.users) + 1), Name: name, CreatedAt: now}
	r.users = append(r.users, u)
	return u, nil
}

func (r *memRepo) User(_ context.Context, id int64) (identity.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.users {
		if u.ID == id {
			return u, nil
		}
	}
	return identity.User{}, errors.New("no such user")
}

func (r *memRepo) CreateSession(_ context.Context, s identity.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[s.TokenHash] = s
	return nil
}

func (r *memRepo) Session(_ context.Context, hash string) (identity.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[hash]
	if !ok {
		return identity.Session{}, identity.ErrNoSession
	}
	return s, nil
}

func (r *memRepo) DeleteExpired(_ context.Context, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for h, s := range r.sessions {
		if s.ExpiresAt.Before(now) {
			delete(r.sessions, h)
		}
	}
	return nil
}

func newService() (*identity.Service, *memRepo, *clock) {
	repo := &memRepo{sessions: map[string]identity.Session{}}
	c := &clock{now: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)}
	return identity.NewService(repo, c), repo, c
}

func TestOpenSessionSignsInTheLocalUser(t *testing.T) {
	svc, repo, c := newService()
	ctx := context.Background()
	token, p, err := svc.OpenSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p.User.Name != identity.LocalUser || p.Session.UserID != p.User.ID || len(token) < 40 || len(p.Session.CSRF) < 40 {
		t.Fatalf("token %q, principal %+v", token, p)
	}
	if !p.Session.ExpiresAt.Equal(c.now.Add(identity.SessionTTL)) {
		t.Fatalf("expires %v", p.Session.ExpiresAt)
	}
	for hash := range repo.sessions {
		if strings.Contains(hash, token) || hash == token {
			t.Fatal("the token itself is stored")
		}
	}

	again, p2, err := svc.OpenSession(ctx)
	if err != nil || again == token || p2.Session.CSRF == p.Session.CSRF || p2.User.ID != p.User.ID {
		t.Fatalf("second session: %q %+v %v", again, p2, err)
	}
	if len(repo.users) != 1 {
		t.Fatalf("%d users, want the one local user", len(repo.users))
	}
}

func TestAuthenticate(t *testing.T) {
	svc, _, c := newService()
	ctx := context.Background()
	token, opened, _ := svc.OpenSession(ctx)

	p, err := svc.Authenticate(ctx, token)
	if err != nil || p.User.Name != identity.LocalUser || p.Session.CSRF != opened.Session.CSRF {
		t.Fatalf("got %+v, %v", p, err)
	}
	for name, tok := range map[string]string{"empty": "", "unknown": "x" + token[1:]} {
		if _, err := svc.Authenticate(ctx, tok); !errors.Is(err, identity.ErrNoSession) {
			t.Errorf("%s token: err = %v", name, err)
		}
	}
	c.now = c.now.Add(identity.SessionTTL + time.Second)
	if _, err := svc.Authenticate(ctx, token); !errors.Is(err, identity.ErrNoSession) {
		t.Fatalf("expired session: err = %v", err)
	}
}

func TestOpeningASessionForgetsExpiredOnes(t *testing.T) {
	svc, repo, c := newService()
	ctx := context.Background()
	_, _, _ = svc.OpenSession(ctx)
	c.now = c.now.Add(identity.SessionTTL + time.Hour)
	_, _, _ = svc.OpenSession(ctx)
	if len(repo.sessions) != 1 {
		t.Fatalf("%d sessions kept", len(repo.sessions))
	}
}

func TestCheckCSRF(t *testing.T) {
	svc, _, _ := newService()
	_, p, _ := svc.OpenSession(context.Background())
	if err := identity.CheckCSRF(p, p.Session.CSRF); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "nope", p.Session.CSRF + "x"} {
		if err := identity.CheckCSRF(p, bad); !errors.Is(err, identity.ErrBadCSRF) {
			t.Errorf("token %q: err = %v", bad, err)
		}
	}
}
