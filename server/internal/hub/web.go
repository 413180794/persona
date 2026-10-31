package hub

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// webApp serves the web client (web/, docs/web.md) from <data dir>/web on the hub's own port, so it shares the
// API's origin: no CORS, and the WebSocket and images authenticate the same way. Without that directory every
// non-API path is a 404, as before. Paths without a file fall back to index.html (client-side routes).
func webApp(dir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		index := filepath.Join(dir, "index.html")
		if _, err := os.Stat(index); err != nil {
			http.NotFound(w, r)
			return
		}
		name := path.Clean("/" + r.URL.Path)
		file := filepath.Join(dir, filepath.FromSlash(name))
		if st, err := os.Stat(file); err != nil || st.IsDir() || strings.HasPrefix(path.Base(name), ".") {
			if strings.HasPrefix(name, "/assets/") { // a missing hashed asset is a real 404, not the app
				http.NotFound(w, r)
				return
			}
			file = index
		}
		if file == index {
			w.Header().Set("Cache-Control", "no-cache")
		} else if strings.HasPrefix(name, "/assets/") {
			// Vite names them by content hash
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		f, err := os.Open(file)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, filepath.Base(file), st.ModTime(), f)
	})
}
