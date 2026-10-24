package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IhebBelhadj/opsforge/academy/internal/httpapi"
	"github.com/IhebBelhadj/opsforge/academy/internal/identity"
	identitysqlite "github.com/IhebBelhadj/opsforge/academy/internal/identity/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform/sqlite"
)

// identities is the real identity service on a temporary database.
func identities(t *testing.T) *identity.Service {
	t.Helper()
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "academy.db"), platform.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return identity.NewService(identitysqlite.NewRepo(store.DB), platform.SystemClock{})
}

// browser is a signed-in page: its cookie jar holds the session, csrf is what the page keeps in memory.
type browser struct {
	srv    *httptest.Server
	client *http.Client
	csrf   string
}

func signIn(t *testing.T, srv *httptest.Server) *browser {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	b := &browser{srv: srv, client: &http.Client{Jar: jar}}
	resp := b.send(t, http.MethodPost, "/api/v1/sessions", "", nil)
	defer func() { _ = resp.Body.Close() }()
	var me httpapi.Me
	if resp.StatusCode != http.StatusCreated || json.NewDecoder(resp.Body).Decode(&me) != nil {
		t.Fatalf("sign in: %d", resp.StatusCode)
	}
	b.csrf = me.Csrf
	return b
}

// send makes a request as the page would: same Origin, the CSRF token on changes; header overrides them.
func (b *browser) send(t *testing.T, method, path, body string, header http.Header) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, b.srv.URL+path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Origin", b.srv.URL)
	if b.csrf != "" && method != http.MethodGet {
		req.Header.Set("X-CSRF-Token", b.csrf)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := b.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// record is resp as a recorder, for checkContract.
func record(resp *http.Response) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	copyResponse(rec, resp)
	return rec
}

func apiServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv, _ := labServer(t)
	return srv
}

func TestSigningInSetsAStrictHttpOnlyCookie(t *testing.T) {
	srv := apiServer(t)
	anon := &browser{srv: srv, client: http.DefaultClient}
	resp := anon.send(t, http.MethodPost, "/api/v1/sessions", "", nil)
	rec := record(resp)
	checkContract(t, "POST", "/api/v1/sessions", rec)
	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies %v", cookies)
	}
	c := cookies[0]
	if c.Name != "academy_session" || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" ||
		c.MaxAge != int(identity.SessionTTL.Seconds()) || len(c.Value) < 40 {
		t.Fatalf("cookie %+v", c)
	}
	me := decode[httpapi.Me](t, rec.Body.Bytes())
	if me.User.Name != identity.LocalUser || me.Csrf == "" {
		t.Fatalf("me %+v", me)
	}

	b := signIn(t, srv)
	rec = record(b.send(t, http.MethodGet, "/api/v1/me", "", nil))
	checkContract(t, "GET", "/api/v1/me", rec)
	if got := decode[httpapi.Me](t, rec.Body.Bytes()); got.Csrf != b.csrf || got.User.Name != identity.LocalUser {
		t.Fatalf("me %+v, csrf %q", got, b.csrf)
	}
}

func TestWithoutASessionTheAPIAnswers401(t *testing.T) {
	srv := apiServer(t)
	for name, header := range map[string]http.Header{
		"no cookie":      nil,
		"unknown cookie": {"Cookie": {"academy_session=forged"}},
	} {
		anon := &browser{srv: srv, client: http.DefaultClient}
		rec := record(anon.send(t, http.MethodGet, "/api/v1/me", "", header))
		checkContract(t, "GET", "/api/v1/me", rec)
		if rec.Code != http.StatusUnauthorized ||
			decode[httpapi.Problem](t, rec.Body.Bytes()).Type != "/problems/no-session" {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body)
		}
		rec = record(anon.send(t, http.MethodPost, "/api/v1/terminals", "", header))
		checkContract(t, "POST", "/api/v1/terminals", rec)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: terminals %d", name, rec.Code)
		}
	}
}

func TestChangesNeedTheSessionsCSRFToken(t *testing.T) {
	srv := apiServer(t)
	b := signIn(t, srv)
	other := signIn(t, srv)
	for name, token := range map[string]string{"none": "", "another session's": other.csrf, "forged": "x"} {
		rec := record(b.send(t, http.MethodPost, "/api/v1/terminals", "", http.Header{"X-Csrf-Token": {token}}))
		checkContract(t, "POST", "/api/v1/terminals", rec)
		if rec.Code != http.StatusForbidden ||
			decode[httpapi.Problem](t, rec.Body.Bytes()).Type != "/problems/bad-csrf" {
			t.Fatalf("%s token: %d %s", name, rec.Code, rec.Body)
		}
	}
	if resp := b.send(t, http.MethodPost, "/api/v1/terminals", "", nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("with the token: %d", resp.StatusCode)
	}
}

// A page on another site can make the browser send requests to 127.0.0.1 (no cookie, thanks to SameSite, but
// opening a session needs none): changes and WebSocket upgrades from another origin are refused before anything.
func TestChangesFromAnotherSiteAreRefused(t *testing.T) {
	srv := apiServer(t)
	anon := &browser{srv: srv, client: http.DefaultClient}
	for name, header := range map[string]http.Header{
		"other origin":    {"Origin": {"https://evil.example"}},
		"other port":      {"Origin": {"http://127.0.0.1:1"}},
		"opaque origin":   {"Origin": {"null"}},
		"cross-site":      {"Origin": nil, "Sec-Fetch-Site": {"cross-site"}},
		"same-site other": {"Origin": nil, "Sec-Fetch-Site": {"same-site"}},
	} {
		rec := record(anon.send(t, http.MethodPost, "/api/v1/sessions", "", header))
		checkContract(t, "POST", "/api/v1/sessions", rec)
		if rec.Code != http.StatusForbidden ||
			decode[httpapi.Problem](t, rec.Body.Bytes()).Type != "/problems/cross-origin" {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	// Tools on the machine (curl, the e2e runner) send no Origin; the page sends its own
	for name, header := range map[string]http.Header{
		"no origin":   {"Origin": nil},
		"same origin": nil,
		"same fetch":  {"Origin": nil, "Sec-Fetch-Site": {"same-origin"}},
	} {
		if resp := anon.send(t, http.MethodPost, "/api/v1/sessions", "", header); resp.StatusCode != http.StatusCreated {
			t.Fatalf("%s: %d", name, resp.StatusCode)
		}
	}
}

func TestReadsNeedNoCSRFToken(t *testing.T) {
	b := signIn(t, apiServer(t))
	b.csrf = ""
	if resp := b.send(t, http.MethodGet, "/api/v1/me", "", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("%d", resp.StatusCode)
	}
}
