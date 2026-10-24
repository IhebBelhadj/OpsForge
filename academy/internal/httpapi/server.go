// Package httpapi is the transport: the generated OpenAPI handlers (api.gen.go, from api/openapi.yaml) call the
// application services; it also serves the embedded web app. No business rules live here.
package httpapi

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

// readyTimeout bounds each readiness probe (the toolchain one starts a Python process).
const readyTimeout = 10 * time.Second

// Probe is something readyz asks: the database, the toolchain.
type Probe interface {
	Ready(ctx context.Context) error
}

// ReadinessCheck is one named probe of readyz, reported in this order.
type ReadinessCheck struct {
	Name  string
	Probe Probe
}

// Deps is what the transport needs, injected by cmd/academy.
type Deps struct {
	Build     platform.Build
	Readiness []ReadinessCheck
	Web       fs.FS
	Log       *slog.Logger
	Terminals Terminals
	Identity  Identity
}

type server struct {
	deps Deps
}

var _ StrictServerInterface = (*server)(nil)

// NewHandler is the whole HTTP surface: the API, then the web app for every other path.
func NewHandler(deps Deps) http.Handler {
	mux := http.NewServeMux()
	HandlerWithOptions(NewStrictHandler(&server{deps: deps}, nil), StdHTTPServerOptions{
		BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeProblem(w, http.StatusBadRequest, "/problems/bad-request", "Bad request", err.Error())
		},
	})
	mux.HandleFunc("GET /ws/terminal/{session}", (&server{deps: deps}).terminalSocket)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, http.StatusNotFound, "/problems/not-found", "Not found", r.URL.Path+" is not an API resource")
	})
	mux.Handle("/", webApp(deps.Web))
	return requestLog(deps.Log, hostGuard(guard(deps.Identity, mux)))
}

func (s *server) GetHealthz(context.Context, GetHealthzRequestObject) (GetHealthzResponseObject, error) {
	return GetHealthz200JSONResponse{Status: Ok, Version: s.deps.Build.Version}, nil
}

// GetReadyz runs every probe at once and reports each; 503 if any failed.
func (s *server) GetReadyz(ctx context.Context, _ GetReadyzRequestObject) (GetReadyzResponseObject, error) {
	results := make([]error, len(s.deps.Readiness))
	var wg sync.WaitGroup
	for i, c := range s.deps.Readiness {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(ctx, readyTimeout)
			defer cancel()
			results[i] = c.Probe.Ready(ctx)
		})
	}
	wg.Wait()
	ready := Readiness{Status: Ready, Checks: make([]Check, 0, len(results))}
	for i, c := range s.deps.Readiness {
		check := Check{Name: c.Name, Ok: results[i] == nil}
		if results[i] != nil {
			detail := results[i].Error()
			check.Detail = &detail
			ready.Status = Unavailable
		}
		ready.Checks = append(ready.Checks, check)
	}
	if ready.Status != Ready {
		return GetReadyz503JSONResponse(ready), nil
	}
	return GetReadyz200JSONResponse(ready), nil
}
