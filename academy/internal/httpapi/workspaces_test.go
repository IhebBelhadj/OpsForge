package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IhebBelhadj/opsforge/academy/internal/httpapi"
)

const (
	treeOp  = "/api/v1/workspaces/{workspace}/tree"
	filesOp = "/api/v1/workspaces/{workspace}/files"
	movesOp = "/api/v1/workspaces/{workspace}/moves"
)

func filePath(ws, path string) string {
	return "/api/v1/workspaces/" + ws + "/files?path=" + url.QueryEscape(path)
}

func sendAs(t *testing.T, b *browser, method, path, op, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := record(b.send(t, method, path, body, nil))
	checkContract(t, method, op, rec)
	return rec
}

func TestTheLabsWorkspaceIsEditable(t *testing.T) {
	w := newWorld(t)
	b := signIn(t, apiServer(t, w.deps))
	ws := startLab(t, w, b, "meet-the-cluster").Attempt.Workspace

	rec := get(t, b, "/api/v1/workspaces/"+ws+"/tree", treeOp)
	tree := decode[httpapi.WorkspaceTree](t, rec.Body.Bytes())
	if rec.Code != 200 || len(tree.Files) != 1 || tree.Files[0].Path != "hello.yaml" || tree.LimitBytes != 10<<20 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rec = get(t, b, filePath(ws, "hello.yaml"), filesOp)
	f := decode[httpapi.WorkspaceFile](t, rec.Body.Bytes())
	if rec.Code != 200 || !strings.Contains(f.Content, "kind: ConfigMap") || len(f.Sha256) != 64 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	body, _ := json.Marshal(map[string]string{"content": "kind: Pod\n", "base_sha256": f.Sha256})
	rec = sendAs(t, b, http.MethodPut, filePath(ws, "hello.yaml"), filesOp, string(body))
	saved := decode[httpapi.WorkspaceFile](t, rec.Body.Bytes())
	if rec.Code != 200 || saved.Sha256 == f.Sha256 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rec = sendAs(t, b, http.MethodPut, filePath(ws, "hello.yaml"), filesOp, string(body)) // the old version again
	if rec.Code != 409 || decode[httpapi.Problem](t, rec.Body.Bytes()).Type != "/problems/conflict" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := sendAs(t, b, http.MethodPost, "/api/v1/workspaces/"+ws+"/moves", movesOp,
		`{"from":"hello.yaml","to":"k8s/pod.yaml"}`); rec.Code != 204 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	sendAs(t, b, http.MethodPut, filePath(ws, "other.yaml"), filesOp, `{"content":"x"}`)
	if rec := sendAs(t, b, http.MethodPost, "/api/v1/workspaces/"+ws+"/moves", movesOp,
		`{"from":"other.yaml","to":"k8s/pod.yaml"}`); rec.Code != 409 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := sendAs(t, b, http.MethodDelete, filePath(ws, "k8s/pod.yaml"), filesOp, ""); rec.Code != 204 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}

	// Refusals, one per documented status
	dir, _ := w.spaces.Dir(t.Context(), 1, ws)
	if err := os.WriteFile(filepath.Join(dir, "bin"), []byte{0xff}, 0o600); err != nil {
		t.Fatal(err)
	}
	big := `{"content":"` + strings.Repeat("x", 1<<20+1) + `"}`
	for _, tc := range []struct {
		method, path, op, body string
		code                   int
	}{
		{http.MethodGet, "/api/v1/workspaces/lab-99/tree", treeOp, "", 404},
		{http.MethodGet, filePath(ws, "nope"), filesOp, "", 404},
		{http.MethodGet, filePath(ws, "bin"), filesOp, "", 415},
		{http.MethodGet, filePath(ws, "../etc/passwd"), filesOp, "", 422},
		{http.MethodPut, filePath("lab-99", "x"), filesOp, `{"content":""}`, 404},
		{http.MethodPut, filePath(ws, "big"), filesOp, big, 413},
		{http.MethodPut, filePath(ws, "/abs"), filesOp, `{"content":""}`, 422},
		{http.MethodDelete, filePath(ws, "nope"), filesOp, "", 404},
		{http.MethodDelete, filePath(ws, "a/../../b"), filesOp, "", 422},
		{http.MethodPost, "/api/v1/workspaces/" + ws + "/moves", movesOp, `{"from":"nope","to":"x"}`, 404},
		{http.MethodPost, "/api/v1/workspaces/" + ws + "/moves", movesOp, `{"from":"other.yaml","to":"../x"}`, 422},
	} {
		if rec := sendAs(t, b, tc.method, tc.path, tc.op, tc.body); rec.Code != tc.code {
			t.Errorf("%s %s: %d %s", tc.method, tc.path, rec.Code, rec.Body)
		}
	}
}

func TestATerminalStartsInTheWorkspace(t *testing.T) {
	w := newWorld(t)
	b := signIn(t, apiServer(t, w.deps))
	ws := startLab(t, w, b, "meet-the-cluster").Attempt.Workspace
	rec := post(t, b, "/api/v1/terminals", "/api/v1/terminals", `{"workspace":"`+ws+`"}`)
	if rec.Code != 201 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rec = post(t, b, "/api/v1/terminals", "/api/v1/terminals", `{"workspace":"lab-99"}`)
	if rec.Code != 404 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}
