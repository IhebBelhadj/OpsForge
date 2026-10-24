package httpapi

import (
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

const notBuilt = `<!doctype html><meta charset="utf-8"><title>OpsForge Academy</title>
<p>The web app is not built into this binary. Build it with <code>make academy</code>.</p>
`

// webApp serves the built React app: files as they are (Vite's hashed assets/ cached for a year), and index.html
// for every other path so the client router handles deep links (/courses/x, /learn/x/y).
func webApp(web fs.FS) http.Handler {
	files := http.FileServerFS(web)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index, err := fs.ReadFile(web, "index.html")
		if err != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(notBuilt))
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" && name != "index.html" {
			if _, err := fs.Stat(web, name); err == nil {
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			} else if !errors.Is(err, fs.ErrNotExist) || strings.HasPrefix(name, "assets/") {
				http.NotFound(w, r) // a missing asset is a broken build, not a page of the app
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	})
}
