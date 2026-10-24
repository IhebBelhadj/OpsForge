package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/IhebBelhadj/opsforge/academy/internal/identity"
)

// Identity is what the transport needs from the identity module.
type Identity interface {
	OpenSession(ctx context.Context) (string, identity.Principal, error)
	Authenticate(ctx context.Context, token string) (identity.Principal, error)
}

// SessionCookie holds the session token (HttpOnly: the page never reads it).
const SessionCookie = "academy_session"

// CSRFHeader carries the session's CSRF token on every change.
const CSRFHeader = "X-CSRF-Token"

type principalKey struct{}

// principal is the signed-in user of a request that passed guard.
func principal(ctx context.Context) (identity.Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(identity.Principal)
	return p, ok
}

// guard is the security of docs/courses/architecture.md: a change or WebSocket upgrade from another site's page
// is refused (403); /api/ and /ws/ need a session (401), except opening one; a change also needs the session's CSRF
// token (403). The rest (health, the web app) is public.
func guard(id Identity, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		change := r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
		upgrade := strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
		if (change || upgrade) && crossSite(r) {
			writeProblem(w, http.StatusForbidden, "/problems/cross-origin", "Cross-origin request",
				"changes are accepted only from the platform's own pages")
			return
		}
		path := r.URL.Path
		protected := strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/ws/")
		if !protected || (r.Method == http.MethodPost && path == "/api/v1/sessions") {
			next.ServeHTTP(w, r)
			return
		}
		var token string
		if c, err := r.Cookie(SessionCookie); err == nil {
			token = c.Value
		}
		p, err := id.Authenticate(r.Context(), token)
		if errors.Is(err, identity.ErrNoSession) {
			writeProblem(w, http.StatusUnauthorized, "/problems/no-session", "No session",
				"sign in with POST /api/v1/sessions")
			return
		}
		if err != nil {
			writeProblem(w, http.StatusInternalServerError, "/problems/internal", "Internal error", "")
			return
		}
		if change && identity.CheckCSRF(p, r.Header.Get(CSRFHeader)) != nil {
			writeProblem(w, http.StatusForbidden, "/problems/bad-csrf", "Missing or wrong CSRF token",
				"send the token from GET /api/v1/me as "+CSRFHeader)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	})
}

// crossSite is a request a browser sent for another site's page: its Origin is not this host (an opaque "null"
// included), or, without Origin, Sec-Fetch-Site says so. Requests from tools on the machine carry neither.
func crossSite(r *http.Request) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		return err != nil || u.Scheme != "http" || u.Host != r.Host
	}
	site := r.Header.Get("Sec-Fetch-Site")
	return site != "" && site != "same-origin" && site != "none"
}

// OpenSession signs the local user in and sets the session cookie.
func (s *server) OpenSession(ctx context.Context, _ OpenSessionRequestObject) (OpenSessionResponseObject, error) {
	token, p, err := s.deps.Identity.OpenSession(ctx)
	if err != nil {
		return nil, err
	}
	// Not Secure: the platform is plain HTTP on loopback only (config refuses other addresses), where a Secure
	// cookie would depend on each browser's localhost exception
	cookie := (&http.Cookie{ //nolint:gosec // G124, see above
		Name: SessionCookie, Value: token, Path: "/", MaxAge: int(identity.SessionTTL.Seconds()),
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
	}).String()
	return OpenSession201JSONResponse{Body: me(p), Headers: OpenSession201ResponseHeaders{SetCookie: &cookie}}, nil
}

// GetMe is the signed-in user and the session's CSRF token.
func (s *server) GetMe(ctx context.Context, _ GetMeRequestObject) (GetMeResponseObject, error) {
	p, ok := principal(ctx)
	if !ok {
		return nil, errors.New("getMe without a principal: guard is not in front of the API")
	}
	return GetMe200JSONResponse(me(p)), nil
}

func me(p identity.Principal) Me {
	return Me{User: User{Id: p.User.ID, Name: p.User.Name}, Csrf: p.Session.CSRF}
}
