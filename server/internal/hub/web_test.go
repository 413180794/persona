package hub

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestWebAppServesFilesAndFallsBackToIndex(t *testing.T) {
	dir := t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(dir, "assets"), 0o755))
	must(os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>app</html>"), 0o644))
	must(os.WriteFile(filepath.Join(dir, "assets", "a-1.js"), []byte("js"), 0o644))
	must(os.WriteFile(filepath.Join(dir, ".secret"), []byte("no"), 0o644))
	h := webApp(dir)
	get := func(p string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		return w
	}
	if w := get("/assets/a-1.js"); w.Code != 200 || w.Body.String() != "js" || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset: %d %q %q", w.Code, w.Body, w.Header().Get("Cache-Control"))
	}
	for _, p := range []string{"/", "/s/laptop/abc", "/.secret", "/../../etc/passwd"} {
		if w := get(p); w.Code != 200 || !strings.Contains(w.Body.String(), "app") || w.Header().Get("Cache-Control") != "no-cache" {
			t.Fatalf("%s: %d %q", p, w.Code, w.Body)
		}
	}
	if w := get("/assets/gone.js"); w.Code != 404 {
		t.Fatalf("missing asset: %d", w.Code)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post: %d", w.Code)
	}
	if w := httptest.NewRecorder(); true {
		webApp(t.TempDir()).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		if w.Code != 404 {
			t.Fatalf("no web dir: %d", w.Code)
		}
	}
}

// Without a data dir copy, the web client built into jarvisd serves; the data dir wins when it has one.
func TestWebAppFallsBackToTheEmbeddedCopy(t *testing.T) {
	embedded := fstest.MapFS{"index.html": {Data: []byte("<html>embedded</html>")}, "assets/b-2.js": {Data: []byte("ejs")}}
	dir := t.TempDir()
	h := webFrom(dir, embedded)
	get := func(p string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		return w
	}
	if w := get("/machines"); w.Code != 200 || !strings.Contains(w.Body.String(), "embedded") {
		t.Fatalf("embedded index: %d %q", w.Code, w.Body)
	}
	if w := get("/assets/b-2.js"); w.Code != 200 || w.Body.String() != "ejs" {
		t.Fatalf("embedded asset: %d %q", w.Code, w.Body)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>disk</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if w := get("/"); !strings.Contains(w.Body.String(), "disk") {
		t.Fatalf("data dir should win: %q", w.Body)
	}
	// A build without the web client (only the placeholder) is a 404, as before.
	if w := httptest.NewRecorder(); true {
		webApp(t.TempDir()).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		if w.Code != 404 {
			t.Fatalf("no web client: %d", w.Code)
		}
	}
}
