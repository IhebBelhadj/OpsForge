package httpapi_test

import (
	"net/http"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/IhebBelhadj/opsforge/academy/internal/httpapi"
)

var pathParam = regexp.MustCompile(`\{[^}]+\}`)

// TestTheGuardAnswersEveryProtectedOperation sends every operation that documents 401 without a session, and
// every one that documents 403 without the CSRF token: guard answers before any handler runs.
func TestTheGuardAnswersEveryProtectedOperation(t *testing.T) {
	w := newWorld(t)
	srv := apiServer(t, w.deps)
	anon := &browser{srv: srv, client: http.DefaultClient}
	b := signIn(t, srv)
	b.csrf = ""
	doc, _ := spec()
	var paths []string
	for path := range doc["paths"].(map[string]any) {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		for method, op := range doc["paths"].(map[string]any)[path].(map[string]any) {
			if method == "parameters" {
				continue
			}
			responses := op.(map[string]any)["responses"].(map[string]any)
			target := pathParam.ReplaceAllString(path, "x")
			m := strings.ToUpper(method)
			for status, client := range map[string]*browser{"401": anon, "403": b} {
				if _, ok := responses[status]; !ok || (status == "403" && m == http.MethodGet) {
					continue
				}
				if status == "403" && path == "/api/v1/sessions" { // the open operation: refused cross-origin
					continue
				}
				rec := record(client.send(t, m, target, "{}", nil))
				checkContract(t, m, path, rec)
				want := map[string]string{"401": "/problems/no-session", "403": "/problems/bad-csrf"}[status]
				if rec.Code != map[string]int{"401": 401, "403": 403}[status] ||
					decode[httpapi.Problem](t, rec.Body.Bytes()).Type != want {
					t.Errorf("%s %s: %d %s, want %s", m, path, rec.Code, rec.Body, want)
				}
			}
		}
	}
}
