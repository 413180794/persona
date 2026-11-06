package node

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRelease serves body as this machine's herdr binary and pins sum for it (body's own sha256 when sum is "");
// herdr is "installed" once the file is in the returned directory. gate, when not nil, holds the download until it
// is closed.
type fakeRelease struct {
	dir  string
	hits atomic.Int32
	gate chan struct{}
	url  string
	srv  *httptest.Server
}

func withFakeRelease(t *testing.T, body []byte, sum string, gate chan struct{}) *fakeRelease {
	t.Helper()
	target, ok := herdrTarget(runtime.GOOS, runtime.GOARCH)
	if !ok {
		t.Skipf("herdr has no release for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if sum == "" {
		s := sha256.Sum256(body)
		sum = hex.EncodeToString(s[:])
	}
	fr := &fakeRelease{dir: t.TempDir(), gate: gate}
	fr.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v") { // the node's own probes of listening ports (ports.go)
			http.NotFound(w, r)
			return
		}
		fr.hits.Add(1)
		if fr.gate != nil {
			select {
			case <-fr.gate:
			case <-r.Context().Done():
				return
			}
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(fr.srv.Close)
	fr.url = fr.srv.URL
	prevRel, prevDir, prevFind, prevWait := herdrRelease, herdrInstallDir, findHerdr, herdrInstallWait
	herdrRelease.URL = func(version, target string) string { return fr.url + "/v" + version + "/herdr-" + target }
	herdrRelease.SHA256 = map[string]string{target: sum}
	herdrInstallDir = func() (string, error) { return fr.dir, nil }
	findHerdr = func(context.Context, string) string {
		if p := filepath.Join(fr.dir, "herdr"); fileExists(p) {
			return p
		}
		return ""
	}
	t.Cleanup(func() {
		herdrRelease, herdrInstallDir, findHerdr, herdrInstallWait = prevRel, prevDir, prevFind, prevWait
	})
	return fr
}

// installNode is a node on herdr with herdr down and not installed, whose starts are faked (fakeLaunch).
func installNode(t *testing.T, body []byte, sum string, gate chan struct{}) (*Node, string, *fakeLaunch, *fakeRelease) {
	t.Helper()
	n, sock := downHerdrNode(t)
	fl := &fakeLaunch{}
	withLaunchHerdr(t, fl)
	fr := withFakeRelease(t, body, sum, gate) // after withLaunchHerdr: its findHerdr wins
	t.Cleanup(n.hinstall.bg.Wait)             // runs first: the fakes stay until the install is done
	return n, sock, fl, fr
}

// "Start" on a machine without herdr: the pinned binary is downloaded, checked, put in ~/.local/bin as an executable
// herdr, and the server started from it — the call answers running.
func TestStartHerdrInstallsItFirst(t *testing.T) {
	body := []byte("#!/bin/sh\necho herdr\n")
	n, _, fl, fr := installNode(t, body, "", nil)
	out, err := request(t, n, "herdr.server.start", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if out["running"] != true {
		t.Fatalf("answer %v", out)
	}
	path := filepath.Join(fr.dir, "herdr")
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("installed %q, %v", got, err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v", st.Mode())
	}
	if fl.starts.Load() != 1 || fl.bins[0] != path {
		t.Fatalf("%d starts of %v", fl.starts.Load(), fl.bins)
	}
	if left, _ := filepath.Glob(filepath.Join(fr.dir, ".herdr-install-*")); len(left) != 0 {
		t.Fatalf("left behind %v", left)
	}
	// Installed now: another "Start" downloads nothing.
	if _, err := request(t, n, "herdr.server.start", map[string]any{}); err != nil || fr.hits.Load() != 1 {
		t.Fatalf("again: %v, %d downloads", err, fr.hits.Load())
	}
}

// A download whose sha256 is not the pinned one is not installed and nothing is started; the user is told why.
func TestStartHerdrInstallChecksum(t *testing.T) {
	n, _, fl, fr := installNode(t, []byte("not herdr"), strings.Repeat("0", 64), nil)
	_, err := request(t, n, "herdr.server.start", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "校验不对") {
		t.Fatalf("err %v", err)
	}
	if fileExists(filepath.Join(fr.dir, "herdr")) || fl.starts.Load() != 0 {
		t.Fatalf("installed or started (%d)", fl.starts.Load())
	}
	if left, _ := filepath.Glob(filepath.Join(fr.dir, ".herdr-install-*")); len(left) != 0 {
		t.Fatalf("left behind %v", left)
	}
	out, _ := request(t, n, "herdr.server.status", map[string]any{})
	if out["installing"] != false || !strings.Contains(out["install_error"].(string), "校验不对") {
		t.Fatalf("status %v", out)
	}
}

// GitHub out of reach: the failure says the machine may need a proxy, and nothing is started.
func TestStartHerdrInstallUnreachable(t *testing.T) {
	n, _, fl, fr := installNode(t, []byte("herdr"), "", nil)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fr.url = "http://" + ln.Addr().String() // nothing answers there once closed
	ln.Close()
	_, err = request(t, n, "herdr.server.start", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "没能下载 herdr") || !strings.Contains(err.Error(), "HTTPS_PROXY") {
		t.Fatalf("err %v", err)
	}
	if fl.starts.Load() != 0 {
		t.Fatalf("%d starts", fl.starts.Load())
	}
}

// A slow download: "Start" says it is installing instead of hanging, the status says so too, and once it is done the
// node starts herdr by itself.
func TestStartHerdrInstallGoesOn(t *testing.T) {
	gate := make(chan struct{})
	n, sock, fl, _ := installNode(t, []byte("herdr"), "", gate)
	herdrInstallWait = 100 * time.Millisecond
	_, err := request(t, n, "herdr.server.start", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "正在安装 herdr") {
		t.Fatalf("err %v", err)
	}
	if out, _ := request(t, n, "herdr.server.status", map[string]any{}); out["installing"] != true {
		t.Fatalf("status %v", out)
	}
	close(gate)
	deadline := time.Now().Add(5 * time.Second)
	for !herdrAlive(context.Background(), sock) {
		if time.Now().After(deadline) {
			t.Fatalf("not started after the install (%d starts)", fl.starts.Load())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if out, _ := request(t, n, "herdr.server.status", map[string]any{}); out["installing"] != false || out["install_error"] != "" {
		t.Fatalf("status %v", out)
	}
}

// The node's own starts install only when hub.json says herdr_autoinstall; otherwise not installed stays as before.
func TestEnsureHerdrAutoinstall(t *testing.T) {
	n, _, fl, fr := installNode(t, []byte("herdr"), "", nil)
	if err := n.ensureHerdr(context.Background(), "test"); !errors.Is(err, errHerdrNotInstalled) || fr.hits.Load() != 0 {
		t.Fatalf("off: %v, %d downloads", err, fr.hits.Load())
	}
	if _, err := request(t, n, "node.configure", map[string]any{"herdr_autoinstall": true}); err != nil {
		t.Fatal(err)
	}
	if err := n.ensureHerdr(context.Background(), "test"); err != nil || fr.hits.Load() != 1 || fl.starts.Load() != 1 {
		t.Fatalf("on: %v, %d downloads, %d starts", err, fr.hits.Load(), fl.starts.Load())
	}
}

func TestHerdrTarget(t *testing.T) {
	for _, c := range []struct{ goos, goarch, want string }{
		{"linux", "amd64", "linux-x86_64"}, {"linux", "arm64", "linux-aarch64"},
		{"darwin", "amd64", "macos-x86_64"}, {"darwin", "arm64", "macos-aarch64"},
		{"windows", "amd64", ""}, {"linux", "386", ""},
	} {
		got, ok := herdrTarget(c.goos, c.goarch)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("%s/%s: %q %v", c.goos, c.goarch, got, ok)
		}
		if ok && len(herdrRelease.SHA256[got]) != 64 {
			t.Errorf("%s: no pinned sha256", got)
		}
	}
}
