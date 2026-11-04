package hub

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// webDist is the web client built into a published jarvisd (scripts/release/publish.sh copies web/dist here before building);
// in other builds only the placeholder is there.
//
//go:embed all:webdist
var webDist embed.FS

// webApp serves the web client (web/, docs/web.md) from <data dir>/web on the hub's own port, so it shares the
// API's origin: no CORS, and the WebSocket and images authenticate the same way. Without that directory the copy built
// into jarvisd serves (published builds), and without either every non-API path is a 404, as before. Paths without a
// file fall back to index.html (client-side routes).
func webApp(dir string) http.Handler {
	embedded, _ := fs.Sub(webDist, "webdist")
	return webFrom(dir, embedded)
}

func webFrom(dir string, embedded fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// The data dir wins (scripts/deploy/deploy.sh web swaps it in whole), checked on every request like before.
		var site fs.FS
		if st, err := os.Stat(filepath.Join(dir, "index.html")); err == nil && !st.IsDir() {
			site = os.DirFS(dir)
		} else if embedded != nil {
			if st, err := fs.Stat(embedded, "index.html"); err == nil && !st.IsDir() {
				site = embedded
			}
		}
		if site == nil {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		file := name
		if st, err := fs.Stat(site, name); name == "" || err != nil || st.IsDir() || strings.HasPrefix(path.Base(name), ".") {
			if strings.HasPrefix(name, "assets/") { // a missing hashed asset is a real 404, not the app
				http.NotFound(w, r)
				return
			}
			file = "index.html"
		}
		switch {
		// the service worker and the manifest (docs/web.md「安装成 App」) are checked again on every load, like the shell
		case file == "index.html", file == "sw.js", file == "manifest.webmanifest":
			w.Header().Set("Cache-Control", "no-cache")
		case strings.HasPrefix(name, "assets/"):
			// Vite names them by content hash
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		if path.Ext(file) == ".webmanifest" { // not in Go's built-in table, and nosniff would leave it untyped
			w.Header().Set("Content-Type", "application/manifest+json")
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		f, err := site.Open(file)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		st, err := f.Stat()
		rs, ok := f.(io.ReadSeeker)
		if err != nil || !ok {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, path.Base(file), st.ModTime(), rs)
	})
}
