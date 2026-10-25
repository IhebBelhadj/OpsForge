// Package opsforgefake is OpsForge's HTTP API in memory (academy/contract/openapi.yaml), for Academy's tests and
// the e2e suite (cmd/opsforge-fake): an empty lab by default, scripted lab work, workspaces in memory and a
// terminal that echoes what is typed. It answers as `opsforge api serve` does: the token on every route but
// /v1/version, problem documents, jobs that are running when started and done when polled.
package opsforgefake

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/coder/websocket"

	"github.com/IhebBelhadj/opsforge/academy/internal/lab"
	"github.com/IhebBelhadj/opsforge/academy/internal/workspace"
	"github.com/IhebBelhadj/opsforge/academy/internal/workspace/memstore"
)

// Fake is the API; set the funcs to script the lab (nil: an empty lab, every step valid, work that succeeds).
type Fake struct {
	Token   string
	Version string
	Minor   int
	State   func() lab.State
	Steps   func(sets []json.RawMessage) []lab.StepError
	Run     func(in json.RawMessage) (any, error) // the runOutput document, or a failed job
	Probe   func(in json.RawMessage) (any, error)
	Heal    func() (bool, error)
	Files   *memstore.Store

	mu    sync.Mutex
	jobs  map[string]*job
	Calls []string // "METHOD /path" of every authorized request
}

// job is started running and reads as done (or failed) from the first poll on.
type job struct {
	ID     string
	Kind   string
	Log    []string
	result map[string]any
}

// New returns an empty lab answering for token.
func New(token string) *Fake {
	return &Fake{Token: token, Version: "1.0.0-fake", Files: memstore.New(), jobs: map[string]*job{}}
}

func problem(w http.ResponseWriter, status int, slug, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"contract": 1, "type": "/problems/" + slug, "title": slug,
		"detail": detail})
}

func send(w http.ResponseWriter, status int, doc map[string]any) {
	doc["contract"] = 1
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(doc)
}

// ServeHTTP routes as lab/apiserver.py does.
func (f *Fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != "" {
		problem(w, http.StatusForbidden, "cross-origin", "browsers cannot call this API")
		return
	}
	if r.URL.Path == "/v1/version" && r.Method == http.MethodGet {
		send(w, http.StatusOK, map[string]any{"minor": f.Minor, "opsforge": f.Version})
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+f.Token {
		problem(w, http.StatusUnauthorized, "no-token", "send the token")
		return
	}
	f.mu.Lock()
	f.Calls = append(f.Calls, r.Method+" "+r.URL.Path)
	f.mu.Unlock()
	route := r.Method + " " + r.URL.Path
	switch {
	case route == "GET /v1/state":
		state := lab.State{Topology: "topology.yml", Sites: map[string]lab.Site{}, Links: map[string]lab.Link{},
			Nodes: []lab.Node{}, Placeholders: map[string]string{}}
		if f.State != nil {
			state = f.State()
		}
		data, _ := json.Marshal(state)
		var doc map[string]any
		_ = json.Unmarshal(data, &doc)
		send(w, http.StatusOK, doc)
	case route == "POST /v1/steps/validate":
		var in struct {
			Sets []json.RawMessage `json:"sets"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			problem(w, http.StatusBadRequest, "bad-input", "not JSON")
			return
		}
		errs := []lab.StepError{}
		if f.Steps != nil {
			errs = append(errs, f.Steps(in.Sets)...)
		}
		send(w, http.StatusOK, map[string]any{"errors": errs})
	case route == "POST /v1/runs", route == "POST /v1/probes", route == "POST /v1/heal":
		f.startJob(w, r)
	case strings.HasPrefix(route, "GET /v1/jobs/"):
		f.getJob(w, strings.TrimPrefix(r.URL.Path, "/v1/jobs/"))
	case strings.HasPrefix(r.URL.Path, "/v1/workspaces/"):
		f.workspaces(w, r)
	case route == "GET /v1/terminal":
		f.terminal(w, r)
	default:
		problem(w, http.StatusNotFound, "not-found", "no route "+r.URL.Path)
	}
}

func (f *Fake) startJob(w http.ResponseWriter, r *http.Request) {
	kind := map[string]string{"/v1/runs": "run", "/v1/probes": "probe", "/v1/heal": "heal"}[r.URL.Path]
	in, _ := io.ReadAll(r.Body)
	var out any
	var err error
	switch kind {
	case "run":
		out, err = map[string]any{"metrics": map[string]float64{}, "log": []string{}, "error": nil}, nil
		if f.Run != nil {
			out, err = f.Run(in)
		}
	case "probe":
		out, err = map[string]any{"passed": true, "results": []any{}, "log": []string{}, "error": nil}, nil
		if f.Probe != nil {
			out, err = f.Probe(in)
		}
	case "heal":
		healed := false
		if f.Heal != nil {
			healed, err = f.Heal()
		}
		out = map[string]any{"healed": healed}
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	j := &job{ID: hex.EncodeToString(b), Kind: kind, Log: []string{}}
	if err != nil {
		j.result = map[string]any{"status": "failed", "problem": map[string]any{"contract": 1,
			"type": "/problems/lab-error", "title": "Lab error", "detail": err.Error()}}
	} else {
		data, _ := json.Marshal(out)
		var doc map[string]any
		_ = json.Unmarshal(data, &doc)
		doc["contract"] = 1
		j.result = map[string]any{"status": "done", "output": doc}
	}
	f.mu.Lock()
	f.jobs[j.ID] = j
	f.mu.Unlock()
	w.Header().Set("Location", "/v1/jobs/"+j.ID)
	send(w, http.StatusAccepted, map[string]any{"id": j.ID, "kind": kind, "status": "running", "log": j.Log,
		"output": nil, "problem": nil})
}

func (f *Fake) getJob(w http.ResponseWriter, id string) {
	f.mu.Lock()
	j := f.jobs[id]
	f.mu.Unlock()
	if j == nil {
		problem(w, http.StatusNotFound, "not-found", "no job "+id)
		return
	}
	doc := map[string]any{"id": j.ID, "kind": j.Kind, "status": j.result["status"], "log": j.Log,
		"output": j.result["output"], "problem": j.result["problem"]}
	send(w, http.StatusOK, doc)
}

func (f *Fake) workspaces(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rest := strings.TrimPrefix(r.URL.EscapedPath(), "/v1/workspaces/")
	ws, tail, _ := strings.Cut(rest, "/")
	var err error
	switch {
	case tail == "" && r.Method == http.MethodPut:
		if err = f.Files.Create(ctx, ws); err == nil {
			w.WriteHeader(http.StatusCreated)
			return
		}
	case tail == "" && r.Method == http.MethodDelete:
		if err = f.Files.Delete(ws); err == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	case tail == "files" && r.Method == http.MethodGet:
		var files []workspace.Entry
		if files, err = f.Files.List(ctx, ws); err == nil {
			list, total := []map[string]any{}, int64(0)
			for _, e := range files {
				list = append(list, map[string]any{"path": e.Path, "size": e.Size})
				total += e.Size
			}
			send(w, http.StatusOK, map[string]any{"files": list, "bytes": total})
			return
		}
	case tail == "moves" && r.Method == http.MethodPost:
		var in struct{ From, To string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if err = f.Files.Rename(ctx, ws, in.From, in.To); err == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
	case strings.HasPrefix(tail, "files/"):
		p := strings.TrimPrefix(r.URL.Path, "/v1/workspaces/"+ws+"/files/")
		if _, cerr := workspace.CleanPath(p); cerr != nil {
			problem(w, http.StatusBadRequest, "bad-path", p)
			return
		}
		switch r.Method {
		case http.MethodGet:
			var data []byte
			if data, err = f.Files.Read(ctx, ws, p); err == nil {
				w.Header().Set("Content-Type", "application/octet-stream")
				_, _ = w.Write(data) //nolint:gosec // octet-stream, as OpsForge serves files (a test fake)
				return
			}
		case http.MethodPut:
			data, _ := io.ReadAll(r.Body)
			if len(data) > workspace.MaxFile {
				problem(w, http.StatusRequestEntityTooLarge, "too-large", p)
				return
			}
			if err = f.Files.Write(ctx, ws, p, data); err == nil {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		case http.MethodDelete:
			if err = f.Files.Remove(ctx, ws, p); err == nil {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
	default:
		problem(w, http.StatusNotFound, "not-found", r.URL.Path)
		return
	}
	switch {
	case errors.Is(err, workspace.ErrNotFound):
		problem(w, http.StatusNotFound, "not-found", err.Error())
	case errors.Is(err, workspace.ErrExists):
		problem(w, http.StatusConflict, "exists", err.Error())
	default:
		problem(w, http.StatusInternalServerError, "internal", fmt.Sprint(err))
	}
}

// terminal is a shell that echoes what is typed (as a tty does), answers `stty size` with the window size and
// closes (1000) on `exit`; its banner is the real shell's ("Lab terminal in <dir>").
func (f *Fake) terminal(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ws := q.Get("workspace")
	if ws != "" {
		if _, err := f.Files.List(r.Context(), ws); err != nil {
			problem(w, http.StatusNotFound, "not-found", "no workspace "+ws)
			return
		}
	}
	cols, rows := q.Get("cols"), q.Get("rows")
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	ctx := context.Background()
	out := func(s string) { _ = conn.Write(ctx, websocket.MessageBinary, []byte(s)) }
	out("Lab terminal in /workspaces/" + ws + ". Lab commands start with opsforge\r\n$ ")
	var line strings.Builder
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var frame struct {
			T    string `json:"t"`
			D    string `json:"d"`
			Cols int    `json:"cols"`
			Rows int    `json:"rows"`
		}
		if json.Unmarshal(data, &frame) != nil {
			continue
		}
		if frame.T == "r" {
			cols, rows = fmt.Sprint(frame.Cols), fmt.Sprint(frame.Rows)
			continue
		}
		for _, c := range frame.D {
			if c != '\r' && c != '\n' {
				line.WriteRune(c)
				out(string(c)) // the tty's echo
				continue
			}
			out("\r\n")
			switch cmd := strings.TrimSpace(line.String()); cmd {
			case "exit":
				_ = conn.Close(websocket.StatusNormalClosure, "")
				return
			case "stty size":
				out(rows + " " + cols + "\r\n")
			}
			line.Reset()
			out("$ ")
		}
	}
}
