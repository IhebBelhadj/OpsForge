package httpapi

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Hijack hands the connection to a WebSocket upgrade.
func (s *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	s.status = http.StatusSwitchingProtocols
	return http.NewResponseController(s.ResponseWriter).Hijack()
}

// hostGuard answers only requests addressed to this machine by a loopback name. A browser tricked by DNS rebinding
// (an attacker's name resolving to 127.0.0.1) sends that name as Host, and a shell is one request away.
func hostGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.Trim(host, "[]")
		if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			writeProblem(w, http.StatusMisdirectedRequest, "/problems/wrong-host", "Wrong host",
				"the platform answers only 127.0.0.1 and localhost")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requestLog gives every request an id (X-Request-Id) and logs it with its status and duration; health probes
// are logged at debug so they don't drown the log.
func requestLog(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := make([]byte, 8)
		_, _ = rand.Read(id)
		w.Header().Set("X-Request-Id", hex.EncodeToString(id))
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)
		level := slog.LevelInfo
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			level = slog.LevelDebug
		}
		log.LogAttrs(r.Context(), level, "request", slog.String("request_id", hex.EncodeToString(id)),
			slog.String("method", r.Method), slog.String("path", r.URL.Path), slog.Int("status", rec.status),
			slog.Duration("duration", time.Since(start)))
	})
}
