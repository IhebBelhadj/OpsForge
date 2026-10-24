package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
	"github.com/IhebBelhadj/opsforge/academy/internal/contentimport"
)

// Importer is what the transport needs from contentimport.
type Importer interface {
	Import(ctx context.Context) (contentimport.Report, error)
}

// ImportContent re-imports the packs.
func (s *server) ImportContent(ctx context.Context, _ ImportContentRequestObject) (ImportContentResponseObject, error) {
	report, err := s.deps.Importer.Import(ctx)
	var invalid *contentimport.BundleErrors
	switch {
	case errors.As(err, &invalid):
		list := make([]PackError, len(invalid.Errors))
		for i, e := range invalid.Errors {
			list[i] = PackError{Pack: e.Pack, Message: e.Message}
		}
		detail := "fix the packs (./opsforge academy compile lists the errors), then import again"
		return ImportContent422ApplicationProblemPlusJSONResponse{Type: "/problems/invalid-packs",
			Title: "The packs have errors", Status: http.StatusUnprocessableEntity, Detail: &detail, Errors: &list}, nil
	case errors.Is(err, contentimport.ErrCompile):
		detail := err.Error()
		return ImportContent503ApplicationProblemPlusJSONResponse{Type: "/problems/toolchain",
			Title: "The lab toolchain failed", Status: http.StatusServiceUnavailable, Detail: &detail}, nil
	case err != nil:
		return nil, err
	}
	return ImportContent200JSONResponse{Changed: report.Changed, Added: counts(report.Added),
		Updated: counts(report.Updated), Retired: counts(report.Retired), Unchanged: counts(report.Unchanged)}, nil
}

func counts(m map[catalog.Kind]int) EntryCounts {
	out := EntryCounts{}
	for k, n := range m {
		if n > 0 {
			out[string(k)] = n
		}
	}
	return out
}
