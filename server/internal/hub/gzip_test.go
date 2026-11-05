package hub

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// bigJSON is an answer like a Feishu chat's messages: large and repetitive.
var bigJSON = `{"messages":[` + strings.Repeat(`{"kind":"card","text":"the same card again and again"},`, 200) + `{}]}`

func gunzip(t *testing.T, b []byte) string {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("not gzip: %v", err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func serveGzip(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	gzipResponses(log.Printf, h).ServeHTTP(w, r)
	return w
}

func answer(contentType, body string, more ...func(http.Header)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		for _, f := range more {
			f(w.Header())
		}
		_, _ = io.WriteString(w, body)
	})
}

func gzipRequest(path, accept string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if accept != "" {
		r.Header.Set("Accept-Encoding", accept)
	}
	return r
}

func TestGzipCompressesALargeJSONAnswer(t *testing.T) {
	w := serveGzip(answer("application/json", bigJSON), gzipRequest("/api/v1/inbox", "gzip, deflate, br"))
	h := w.Header()
	if w.Code != 200 || h.Get("Content-Encoding") != "gzip" || h.Get("Vary") != "Accept-Encoding" || h.Get("Content-Length") != "" {
		t.Fatalf("%d %v", w.Code, h)
	}
	if gunzip(t, w.Body.Bytes()) != bigJSON {
		t.Fatal("the body changed")
	}
	if w.Body.Len() > len(bigJSON)/10 {
		t.Fatalf("%d bytes for %d", w.Body.Len(), len(bigJSON))
	}
}

func TestGzipLeavesAlone(t *testing.T) {
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00\x01", 2000)
	cases := []struct {
		name string
		h    http.Handler
		r    *http.Request
		want string
	}{
		{"a client that did not ask", answer("application/json", bigJSON), gzipRequest("/api/v1/inbox", ""), bigJSON},
		{"a client that refuses gzip", answer("application/json", bigJSON), gzipRequest("/api/v1/inbox", "gzip;q=0, identity"), bigJSON},
		{"a client that takes only identity", answer("application/json", bigJSON), gzipRequest("/api/v1/inbox", "identity"), bigJSON},
		{"a small answer", answer("application/json", `{"ok":true}`), gzipRequest("/api/v1/x", "gzip"), `{"ok":true}`},
		{"an image", answer("image/png", png), gzipRequest("/api/v1/lark/media/k", "gzip"), png},
		{"an event stream", answer("text/event-stream", bigJSON), gzipRequest("/api/v1/x/events", "gzip"), bigJSON},
		{"an answer with its own encoding", answer("application/json", bigJSON, func(h http.Header) { h.Set("Content-Encoding", "identity") }), gzipRequest("/api/v1/x", "gzip"), bigJSON},
		{"an answer that sets a cookie", answer("application/json", bigJSON, func(h http.Header) { h.Add("Set-Cookie", "s=v") }), gzipRequest("/api/v1/x", "gzip"), bigJSON},
		{"the admin API", answer("application/json", bigJSON), gzipRequest("/api/v1/admin/state", "gzip"), bigJSON},
		{"the admin pages", answer("text/html", bigJSON), gzipRequest("/admin/", "gzip"), bigJSON},
		{"a long-lived path", answer("application/json", bigJSON), gzipRequest("/api/v1/extensions/x/y", "gzip"), bigJSON},
	}
	for _, c := range cases {
		w := serveGzip(c.h, c.r)
		if w.Header().Get("Content-Encoding") == "gzip" || w.Body.String() != c.want {
			t.Errorf("%s: compressed (%q) or changed", c.name, w.Header().Get("Content-Encoding"))
		}
	}
}

func TestGzipLeavesARangeToServeContent(t *testing.T) {
	content := strings.Repeat("abcdefghij", 300)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "a.js", time.Time{}, strings.NewReader(content))
	})
	r := gzipRequest("/assets/a.js", "gzip")
	r.Header.Set("Range", "bytes=0-9")
	if w := serveGzip(h, r); w.Code != http.StatusPartialContent || w.Body.String() != "abcdefghij" || w.Header().Get("Content-Encoding") != "" {
		t.Fatalf("%d %q %v", w.Code, w.Body, w.Header())
	}
	// The whole file, as the web app serves it: Content-Length goes, the content stays.
	w := serveGzip(h, gzipRequest("/assets/a.js", "gzip"))
	if w.Code != 200 || w.Header().Get("Content-Encoding") != "gzip" || w.Header().Get("Content-Length") != "" || gunzip(t, w.Body.Bytes()) != content {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("type %q", ct)
	}
}

func TestGzipKeepsTheStatusAndTypes(t *testing.T) {
	teapot := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTeapot)
		_, _ = io.WriteString(w, bigJSON)
	})
	if w := serveGzip(teapot, gzipRequest("/api/v1/x", "gzip")); w.Code != http.StatusTeapot || gunzip(t, w.Body.Bytes()) != bigJSON {
		t.Fatalf("%d", w.Code)
	}
	notModified := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotModified) })
	if w := serveGzip(notModified, gzipRequest("/assets/a.js", "gzip")); w.Code != http.StatusNotModified || w.Header().Get("Content-Encoding") != "" || w.Body.Len() != 0 {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	// No type given: it is the uncompressed bytes' type, not that of the gzip that went out.
	html := "<!DOCTYPE html><html>" + strings.Repeat("<p>hello</p>", 200) + "</html>"
	w := serveGzip(answer("", html), gzipRequest("/", "gzip"))
	if w.Header().Get("Content-Encoding") != "gzip" || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") || gunzip(t, w.Body.Bytes()) != html {
		t.Fatalf("%v", w.Header())
	}
	// A strong ETag names the uncompressed bytes: the compressed answer's is weak.
	w = serveGzip(answer("application/json", bigJSON, func(h http.Header) { h.Set("ETag", `"v1"`) }), gzipRequest("/api/v1/x", "gzip"))
	if w.Header().Get("ETag") != `W/"v1"` {
		t.Fatalf("etag %q", w.Header().Get("ETag"))
	}
}

// An upgrade gets the connection's own writer, so the WebSocket library can take the connection over.
func TestGzipPassesAnUpgradeThrough(t *testing.T) {
	var wrapped bool
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, wrapped = w.(*gzipWriter) })
	r := gzipRequest("/api/v1/bus", "gzip")
	r.Header.Set("Connection", "Upgrade")
	r.Header.Set("Upgrade", "websocket")
	serveGzip(h, r)
	if wrapped {
		t.Fatal("an upgrade went through the gzip writer")
	}
}

// What a handler flushes reaches the client at once, compressed: a stream is not held for the rest.
func TestGzipStreamsWhatIsFlushed(t *testing.T) {
	more := make(chan struct{})
	srv := httptest.NewServer(gzipResponses(log.Printf, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{\"n\":1}\n")
		_ = http.NewResponseController(w).Flush()
		<-more
		_, _ = io.WriteString(w, "{\"n\":2}\n")
	})))
	defer srv.Close()
	defer close(more)
	resp, err := srv.Client().Get(srv.URL + "/api/v1/stream") // Go's client asks for gzip and undoes it
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if !resp.Uncompressed {
		t.Fatal("the stream did not come gzipped")
	}
	lines := bufio.NewReader(resp.Body)
	got := make(chan string, 1)
	go func() {
		line, _ := lines.ReadString('\n')
		got <- line
	}()
	select {
	case line := <-got:
		if line != "{\"n\":1}\n" {
			t.Fatalf("first line %q", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the flushed line did not arrive before the handler went on")
	}
}

func TestAcceptsGzip(t *testing.T) {
	for v, want := range map[string]bool{
		"gzip": true, "deflate, gzip;q=0.5": true, "GZIP": true, "*": true, "br;q=1, *;q=0.1": true,
		"": false, "identity": false, "gzip;q=0": false, "gzip; q=0.0, *": false, "br": false, "*;q=0": false,
	} {
		if got := acceptsGzip([]string{v}); got != want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", v, got, want)
		}
	}
}

// Through the hub's own routes: the web app's bundle goes out compressed with its cache headers, and an auth refusal is
// still logged once (securityError marks the request logger's statusWriter, which the handlers still get).
func TestRoutesGzipTheWebAppAndKeepTheRequestLog(t *testing.T) {
	h := newTestHub(t)
	var mu sync.Mutex
	var logs bytes.Buffer
	h.logger = log.New(lockedWriter{&mu, &logs}, "", 0)
	web := filepath.Join(h.cfg.dir, "web")
	if err := os.MkdirAll(filepath.Join(web, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	js := "console.log(1);\n" + strings.Repeat("export const a = 'persona';\n", 300)
	_ = os.WriteFile(filepath.Join(web, "index.html"), []byte("<html>app</html>"), 0o644)
	_ = os.WriteFile(filepath.Join(web, "assets", "main-1.js"), []byte(js), 0o644)
	handler := h.routes()
	serve := func(r *http.Request) *httptest.ResponseRecorder {
		r.RemoteAddr = "127.0.0.1:1"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	w := serve(gzipRequest("/assets/main-1.js", "gzip"))
	if w.Code != 200 || w.Header().Get("Content-Encoding") != "gzip" || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") ||
		w.Header().Get("X-Request-Id") == "" || gunzip(t, w.Body.Bytes()) != js {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	r := gzipRequest("/api/v1/machines", "gzip")
	r.Header.Set("Authorization", "Bearer not-a-token")
	if w := serve(r); w.Code != http.StatusUnauthorized {
		t.Fatalf("%d", w.Code)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Contains(logs.String(), "/api/v1/machines -> 401") {
		t.Fatalf("the request log repeats the refusal: %q", logs.String())
	}
}

type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// A handler that declares a Content-Length and breaks off before it (a download whose later chunk failed, #2367)
// must not reach the client as a complete gzipped body: the connection is aborted and the client sees an error.
// One that writes all it declared still goes out compressed and whole.
func TestGzipAbortsABodyShorterThanItsContentLength(t *testing.T) {
	body := strings.Repeat("a log line that is still growing\n", 64)[:2048]
	for _, tc := range []struct {
		name    string
		write   int
		aborted bool
	}{
		{"short", 1500, true},
		{"full", 2048, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var logs []string
			logf := func(format string, args ...any) {
				mu.Lock()
				defer mu.Unlock()
				logs = append(logs, fmt.Sprintf(format, args...))
			}
			srv := httptest.NewServer(gzipResponses(logf, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.Header().Set("Content-Length", "2048")
				// The first part reaches the client before the rest fails, as with a download's first chunk.
				_, _ = io.WriteString(w, body[:1100])
				_ = http.NewResponseController(w).Flush()
				_, _ = io.WriteString(w, body[1100:tc.write])
			})))
			defer srv.Close()
			resp, err := srv.Client().Get(srv.URL + "/api/v1/machines/m/files/download")
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if !resp.Uncompressed {
				t.Fatal("the response did not come gzipped")
			}
			got, err := io.ReadAll(resp.Body)
			mu.Lock()
			defer mu.Unlock()
			if tc.aborted {
				if err == nil {
					t.Fatalf("a %d-byte body declared as 2048 was read without an error", len(got))
				}
				if len(logs) != 1 || !strings.Contains(logs[0], "1500 of 2048") {
					t.Fatalf("logs %q", logs)
				}
				return
			}
			if err != nil || string(got) != body {
				t.Fatalf("%d bytes, %v", len(got), err)
			}
			if len(logs) != 0 {
				t.Fatalf("logs %q", logs)
			}
		})
	}
}
