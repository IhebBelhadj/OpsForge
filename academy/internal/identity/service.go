package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

// Service opens and checks sessions.
type Service struct {
	repo  Repo
	clock platform.Clock
}

// NewService stores users and sessions in repo.
func NewService(repo Repo, clock platform.Clock) *Service {
	return &Service{repo: repo, clock: clock}
}

// OpenSession signs the local user in: the token goes in the cookie, the principal carries the CSRF token the
// page sends back on every change. Expired sessions are dropped on the way.
func (s *Service) OpenSession(ctx context.Context) (string, Principal, error) {
	now := s.clock.Now()
	if err := s.repo.DeleteExpired(ctx, now); err != nil {
		return "", Principal{}, fmt.Errorf("expired sessions: %w", err)
	}
	user, err := s.repo.EnsureUser(ctx, LocalUser, now)
	if err != nil {
		return "", Principal{}, fmt.Errorf("local user: %w", err)
	}
	token := secret()
	session := Session{
		TokenHash: hash(token), UserID: user.ID, CSRF: secret(), CreatedAt: now, ExpiresAt: now.Add(SessionTTL),
	}
	if err := s.repo.CreateSession(ctx, session); err != nil {
		return "", Principal{}, fmt.Errorf("session: %w", err)
	}
	return token, Principal{User: user, Session: session}, nil
}

// Authenticate returns who holds token, or ErrNoSession.
func (s *Service) Authenticate(ctx context.Context, token string) (Principal, error) {
	if token == "" {
		return Principal{}, ErrNoSession
	}
	session, err := s.repo.Session(ctx, hash(token))
	if err != nil {
		return Principal{}, err
	}
	if !s.clock.Now().Before(session.ExpiresAt) {
		return Principal{}, ErrNoSession
	}
	user, err := s.repo.User(ctx, session.UserID)
	if err != nil {
		return Principal{}, fmt.Errorf("session user: %w", err)
	}
	return Principal{User: user, Session: session}, nil
}

// CheckCSRF accepts a state-changing request only with its session's CSRF token (compared in constant time).
func CheckCSRF(p Principal, token string) error {
	if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(p.Session.CSRF)) != 1 {
		return ErrBadCSRF
	}
	return nil
}

// secret is 32 random bytes, URL-safe.
func secret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // never fails (crypto/rand panics instead)
	return base64.RawURLEncoding.EncodeToString(b)
}

func hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
