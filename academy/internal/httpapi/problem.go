package httpapi

import (
	"encoding/json"
	"net/http"
)

// writeProblem answers with an RFC 9457 problem document; `kind` is its stable type URI (/problems/...).
func writeProblem(w http.ResponseWriter, status int, kind, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Problem{Type: kind, Title: title, Status: status, Detail: &detail})
}
