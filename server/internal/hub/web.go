package hub

import (
	"bytes"
	"embed"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
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
		// the service worker and the manifest (docs/web.md "Install as an app") are checked again on every load, like the shell
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
		if file == "index.html" {
			serveIndex(w, r, site)
			return
		}
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

// serveIndex serves index.html with its <base> at the sub-path a reverse proxy serves the hub under (docs/web.md "Sub-path"):
// the page's files are relative to it and lib/base.ts reads it for the API, the WebSocket and the page's routes. The
// same file answers differently per X-Forwarded-Prefix, so a cache must key on it (Vary).
func serveIndex(w http.ResponseWriter, r *http.Request, site fs.FS) {
	b, err := fs.ReadFile(site, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var mod time.Time
	if st, err := fs.Stat(site, "index.html"); err == nil {
		mod = st.ModTime()
	}
	w.Header().Add("Vary", "X-Forwarded-Prefix")
	http.ServeContent(w, r, "index.html", mod, bytes.NewReader(withBase(b, forwardedPrefix(r))))
}

// maxPrefix bounds X-Forwarded-Prefix: a proxy's sub-path is a few short segments.
const maxPrefix = 256

// forwardedPrefix is the sub-path a reverse proxy serves the hub under, from its X-Forwarded-Prefix ("/persona",
// trailing slashes dropped), or "" at the root. Only plain path segments count: no dot segments, nothing that could
// leave the attribute (quotes, angle brackets, spaces, control characters, percent escapes); anything else is ignored.
func forwardedPrefix(r *http.Request) string {
	v := r.Header.Get("X-Forwarded-Prefix")
	if v == "" || len(v) > maxPrefix || v[0] != '/' {
		return ""
	}
	p := strings.TrimRight(v, "/")
	if p == "" {
		return ""
	}
	for _, seg := range strings.Split(p[1:], "/") {
		if seg == "" || seg == "." || seg == ".." {
			return ""
		}
		for i := 0; i < len(seg); i++ {
			c := seg[i]
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == '~') {
				return ""
			}
		}
	}
	return p
}

// baseTag is index.html's <base> as web/index.html writes it (and Vite keeps it).
var baseTag = []byte(`<base href="/" />`)

// withBase points index.html's <base> at prefix + "/"; at the root (prefix "") the page is as it was built.
func withBase(index []byte, prefix string) []byte {
	if prefix == "" {
		return index
	}
	return bytes.Replace(index, baseTag, []byte(`<base href="`+prefix+`/" />`), 1)
}
