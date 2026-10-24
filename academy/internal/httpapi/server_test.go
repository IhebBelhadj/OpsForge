package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/IhebBelhadj/opsforge/academy/internal/httpapi"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

type probe struct{ err error }

func (p probe) Ready(context.Context) error { return p.err }

var built = fstest.MapFS{
	"index.html":        {Data: []byte("<!doctype html><title>OpsForge Academy</title>")},
	"assets/app-1a.js":  {Data: []byte("console.log(1)")},
	"favicon.svg":       {Data: []byte("<svg/>")},
	"assets/app-1a.css": {Data: []byte("body{}")},
}

func handler(web fstest.MapFS, checks ...httpapi.ReadinessCheck) http.Handler {
	return httpapi.NewHandler(httpapi.Deps{
		Build:     platform.Build{Version: "1.2.3"},
		Readiness: checks,
		Web:       web,
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func decode[T any](t *testing.T, body []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestHealthz(t *testing.T) {
	rec := do(handler(built), http.MethodGet, "/healthz")
	checkContract(t, "GET", "/healthz", rec)
	if h := decode[httpapi.Health](t, rec.Body.Bytes()); h.Status != "ok" || h.Version != "1.2.3" {
		t.Fatalf("got %+v", h)
	}
}

func TestReadyz(t *testing.T) {
	for name, tc := range map[string]struct {
		checks []httpapi.ReadinessCheck
		code   int
		failed string
	}{
		"all ready": {[]httpapi.ReadinessCheck{{Name: "database", Probe: probe{}}, {Name: "toolchain", Probe: probe{}}}, 200, ""},
		"toolchain down": {[]httpapi.ReadinessCheck{
			{Name: "database", Probe: probe{}},
			{Name: "toolchain", Probe: probe{errors.New("contract 2")}},
		}, 503, "toolchain"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := do(handler(built, tc.checks...), http.MethodGet, "/readyz")
			if rec.Code != tc.code {
				t.Fatalf("status %d, want %d: %s", rec.Code, tc.code, rec.Body)
			}
			checkContract(t, "GET", "/readyz", rec)
			r := decode[httpapi.Readiness](t, rec.Body.Bytes())
			if len(r.Checks) != len(tc.checks) {
				t.Fatalf("checks %+v", r.Checks)
			}
			for i, c := range r.Checks {
				if c.Name != tc.checks[i].Name || c.Ok != (c.Name != tc.failed) {
					t.Fatalf("check %d = %+v", i, c)
				}
				if !c.Ok && (c.Detail == nil || *c.Detail != "contract 2") {
					t.Fatalf("failed check without its reason: %+v", c)
				}
			}
		})
	}
}

func TestTheWebAppIsServedWithDeepLinks(t *testing.T) {
	h := handler(built)
	for _, path := range []string{"/", "/courses/k8s-intro", "/learn/k8s-intro/what-is-a-pod"} {
		rec := do(h, http.MethodGet, path)
		if rec.Code != 200 || rec.Body.String() != string(built["index.html"].Data) {
			t.Fatalf("%s: %d %q", path, rec.Code, rec.Body)
		}
		if rec.Header().Get("Cache-Control") != "no-cache" {
			t.Fatalf("%s: index.html must be revalidated, got %q", path, rec.Header().Get("Cache-Control"))
		}
	}
	rec := do(h, http.MethodGet, "/assets/app-1a.js")
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("hashed asset: %d %v", rec.Code, rec.Header())
	}
	if rec := do(h, http.MethodGet, "/assets/missing.js"); rec.Code != 404 {
		t.Fatalf("a missing asset is not the app: %d", rec.Code)
	}
}

func TestUnknownAPIPathsAreProblems(t *testing.T) {
	b := signIn(t, apiServer(t))
	for _, path := range []string{"/api/v1/nope", "/api/"} {
		rec := record(b.send(t, http.MethodGet, path, "", nil))
		if rec.Code != 404 || rec.Header().Get("Content-Type") != "application/problem+json" {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Header().Get("Content-Type"))
		}
		if p := decode[httpapi.Problem](t, rec.Body.Bytes()); p.Type != "/problems/not-found" || p.Status != 404 {
			t.Fatalf("%s: %+v", path, p)
		}
	}
}

func TestWithoutAWebBuildTheRootSaysHowToBuildIt(t *testing.T) {
	rec := do(handler(fstest.MapFS{".gitkeep": {}}), http.MethodGet, "/")
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "make academy") {
		t.Fatalf("%d %q", rec.Code, rec.Body)
	}
}

func TestResponsesCarryARequestID(t *testing.T) {
	rec := do(handler(built), http.MethodGet, "/healthz")
	if len(rec.Header().Get("X-Request-Id")) != 16 {
		t.Fatalf("X-Request-Id = %q", rec.Header().Get("X-Request-Id"))
	}
}
