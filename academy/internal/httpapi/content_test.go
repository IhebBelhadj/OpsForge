package httpapi_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
	"github.com/IhebBelhadj/opsforge/academy/internal/contentimport"
	"github.com/IhebBelhadj/opsforge/academy/internal/httpapi"
	"github.com/IhebBelhadj/opsforge/academy/internal/opsforgeapi"
)

type importer struct {
	report contentimport.Report
	err    error
}

func (i importer) Import(context.Context) (contentimport.Report, error) { return i.report, i.err }

func withImporter(i importer) func(*httpapi.Deps) {
	return func(d *httpapi.Deps) { d.Importer = i }
}

func TestImportReportsWhatChanged(t *testing.T) {
	report := contentimport.Report{Changed: true, Added: map[catalog.Kind]int{catalog.KindLab: 6},
		Updated: map[catalog.Kind]int{}, Retired: map[catalog.Kind]int{catalog.KindBadge: 1},
		Unchanged: map[catalog.Kind]int{catalog.KindPack: 1}}
	b := signIn(t, apiServer(t, withImporter(importer{report: report})))
	rec := record(b.send(t, http.MethodPost, "/api/v1/admin/import", "", nil))
	checkContract(t, "POST", "/api/v1/admin/import", rec)
	got := decode[httpapi.ImportReport](t, rec.Body.Bytes())
	if rec.Code != 200 || !got.Changed || got.Added["lab"] != 6 || got.Retired["badge"] != 1 || len(got.Updated) != 0 {
		t.Fatalf("%d %+v", rec.Code, got)
	}
}

func TestImportFailures(t *testing.T) {
	invalid := &contentimport.BundleErrors{Errors: []contentimport.BundleError{{Pack: "core", Message: "labs/x.yml: y"}}}
	for name, tc := range map[string]struct {
		err    error
		status int
		typ    string
	}{
		"invalid packs": {invalid, 422, "/problems/invalid-packs"},
		"toolchain": {fmt.Errorf("%w: %w", contentimport.ErrCompile,
			&opsforgeapi.Problem{Type: "/problems/lab-error", Detail: "boom"}), 503, "/problems/toolchain"},
		"contract": {fmt.Errorf("%w: %w", contentimport.ErrCompile, &opsforgeapi.ContractError{Got: 2}), 503,
			"/problems/toolchain"},
		"unreachable": {fmt.Errorf("%w: %w", contentimport.ErrCompile, errors.New("exec: no such file")), 503,
			"/problems/toolchain"},
	} {
		t.Run(name, func(t *testing.T) {
			b := signIn(t, apiServer(t, withImporter(importer{err: tc.err})))
			rec := record(b.send(t, http.MethodPost, "/api/v1/admin/import", "", nil))
			checkContract(t, "POST", "/api/v1/admin/import", rec)
			p := decode[httpapi.Problem](t, rec.Body.Bytes())
			if rec.Code != tc.status || p.Type != tc.typ {
				t.Fatalf("%d %+v", rec.Code, p)
			}
			if name == "invalid packs" && (p.Errors == nil || (*p.Errors)[0].Message != "labs/x.yml: y") {
				t.Fatalf("errors %+v", p.Errors)
			}
		})
	}
}

func TestImportNeedsASessionAndTheToken(t *testing.T) {
	srv := apiServer(t, withImporter(importer{}))
	anon := &browser{srv: srv, client: http.DefaultClient}
	rec := record(anon.send(t, http.MethodPost, "/api/v1/admin/import", "", nil))
	checkContract(t, "POST", "/api/v1/admin/import", rec)
	b := signIn(t, srv)
	b.csrf = ""
	rec = record(b.send(t, http.MethodPost, "/api/v1/admin/import", "", nil))
	checkContract(t, "POST", "/api/v1/admin/import", rec)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("%d", rec.Code)
	}
}
