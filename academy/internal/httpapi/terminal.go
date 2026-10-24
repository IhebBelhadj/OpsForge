package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/coder/websocket"

	"github.com/IhebBelhadj/opsforge/academy/internal/terminal"
)

// Terminals is what the transport needs from the terminal module.
type Terminals interface {
	Open(cols, rows uint16) (string, error)
	Attach(id string) (*terminal.Attachment, error)
}

// WebSocket close codes of /ws/terminal (4000-4999 are the application's). The client reconnects after
// StatusSlow and StatusGoingAway, never after StatusNormalClosure ("exited") or StatusReplaced.
const (
	StatusReplaced websocket.StatusCode = 4000 // another tab attached to the session
	StatusSlow     websocket.StatusCode = 4001 // the client fell behind the output
)

// maxInputFrame bounds one client frame (a paste included).
const maxInputFrame = 1 << 20

// closeFor maps why a session's output ended to the WebSocket close.
var closeFor = map[string]websocket.StatusCode{
	terminal.ReasonExited:   websocket.StatusNormalClosure,
	terminal.ReasonReplaced: StatusReplaced,
	terminal.ReasonSlow:     StatusSlow,
	terminal.ReasonClosed:   websocket.StatusGoingAway,
}

func (s *server) OpenTerminal(_ context.Context, req OpenTerminalRequestObject) (OpenTerminalResponseObject, error) {
	cols, rows := 80, 24
	if req.Body != nil {
		if req.Body.Cols != nil {
			cols = *req.Body.Cols
		}
		if req.Body.Rows != nil {
			rows = *req.Body.Rows
		}
	}
	if cols < 2 || cols > 1000 || rows < 2 || rows > 1000 {
		detail := "cols and rows must be between 2 and 1000"
		return OpenTerminal400ApplicationProblemPlusJSONResponse{Type: "/problems/bad-request", Title: "Bad request",
			Status: http.StatusBadRequest, Detail: &detail}, nil
	}
	id, err := s.deps.Terminals.Open(uint16(cols), uint16(rows)) //nolint:gosec // range checked above
	if errors.Is(err, terminal.ErrTooMany) {
		detail := "close a terminal tab first"
		return OpenTerminal429ApplicationProblemPlusJSONResponse{Type: "/problems/too-many-terminals",
			Title: "Too many terminals", Status: http.StatusTooManyRequests, Detail: &detail}, nil
	}
	if err != nil {
		return nil, err
	}
	return OpenTerminal201JSONResponse{Session: id}, nil
}

// clientFrame is a frame from xterm.js: {"t":"i","d":"..."} is typed input, {"t":"r","cols":..,"rows":..} a resize
// (Phase 10's protocol).
type clientFrame struct {
	T    string `json:"t"`
	D    string `json:"d"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// terminalSocket is /ws/terminal/{session}: binary frames carry the shell's output (the replay first), text frames
// from the client carry input and resizes. The socket closes with the reason the output ended (closeFor); when the
// client leaves, the session lingers for a reconnect.
func (s *server) terminalSocket(w http.ResponseWriter, r *http.Request) {
	att, err := s.deps.Terminals.Attach(r.PathValue("session"))
	if err != nil {
		writeProblem(w, http.StatusNotFound, "/problems/not-found", "No such terminal", "the session ended or never existed")
		return
	}
	// Accept refuses an Origin other than the request's host: a page elsewhere cannot drive this shell
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		att.Close()
		return
	}
	c.SetReadLimit(maxInputFrame)
	ctx := context.WithoutCancel(r.Context())

	go func() {
		for chunk := range att.Output() {
			if err := c.Write(ctx, websocket.MessageBinary, chunk); err != nil {
				_ = c.CloseNow()
				return
			}
		}
		if reason := att.Reason(); reason != "" {
			_ = c.Close(closeFor[reason], reason)
		}
	}()

	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			att.Close()
			return
		}
		var f clientFrame
		if typ != websocket.MessageText || json.Unmarshal(data, &f) != nil {
			continue
		}
		switch {
		case f.T == "i":
			err = att.Write([]byte(f.D))
		case f.T == "r" && f.Cols >= 2 && f.Rows >= 2:
			err = att.Resize(f.Cols, f.Rows)
		}
		if errors.Is(err, terminal.ErrDetached) {
			return // replaced or ended: the writer goroutine closes the socket with the reason
		}
	}
}
