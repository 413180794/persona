package extmgr

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/connector/github"
	"github.com/nwverse/persona/server/internal/extension"
)

// waitOp waits until operation id is in one of states, and returns it.
func waitOp(t *testing.T, h *Manager, id string, states ...string) ExtensionOperation {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		op := h.opByID(id)
		if op == nil {
			t.Fatalf("operation %s is gone", id)
		}
		v := h.snapshot(op)
		for _, s := range states {
			if v.State == s {
				return v
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("operation %s is %s, not %v: %+v", id, v.State, states, v)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func stepState(v ExtensionOperation, id string) ExtensionOpStep {
	for _, s := range v.Steps {
		if s.ID == id {
			return s
		}
	}
	return ExtensionOpStep{}
}

func opID(t *testing.T, code int, out map[string]any) string {
	t.Helper()
	if code != http.StatusAccepted {
		t.Fatalf("not 202: %d %v", code, out)
	}
	op, _ := out["operation"].(map[string]any)
	id, _ := op["id"].(string)
	if id == "" {
		t.Fatalf("no operation: %v", out)
	}
	return id
}

// TestExtensionOperations: an install from a market in steps (#3277): previewed in the background, waiting for the
// user, installed and started; a second time from the cache; a stalled download fails at its step and is retried; a
// failure after the user agreed goes on by itself when retried; an uninstall in steps.
func TestExtensionOperations(t *testing.T) {
	h, phone, reader := marketHub(t)
	mux := h.routes()
	dir := uiPackage(t)
	v1 := tarDir(t, dir)
	gh := &fakeGitHub{release: "v1", files: map[string][]byte{}, assets: map[string][]byte{"example-ui-1.0.0.tar.gz": v1}}
	srv := httptest.NewTLSServer(gh)
	defer srv.Close()
	storage := httptest.NewTLSServer(gh)
	defer storage.Close()
	gh.storage = "https://storage.example.com"
	tr := srv.Client().Transport.(*http.Transport).Clone()
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr == "storage.example.com:443" {
			addr = storage.Listener.Addr().String()
		}
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	h.extensions.markets.client, h.extensions.markets.githubAPI = &http.Client{Transport: tr}, srv.URL
	gh.files[extension.IndexFile] = marketIndex(t, "Example plugins", marketEntry(t, dir, "release:v1/example-ui-1.0.0.tar.gz", v1))
	_, mk := call(t, mux, "POST", "/api/v1/extensions/-/markets", phone, `{"spec": "github:example/plugins"}`)
	market := mk["id"].(string)
	preview := fmt.Sprintf(`{"market": %q, "extension": "example-ui", "async": true}`, market)

	if code, _ := call(t, mux, "GET", "/api/v1/extensions/-/operations", reader, ""); code != http.StatusForbidden {
		t.Fatalf("an answer-only device lists them: %d", code)
	}

	// Previewed in the background: it stops at the consent, with every step before it done.
	id := opCall(t, mux, "POST", "/api/v1/extensions/-/catalog/preview", phone, preview)
	v := waitOp(t, h, id, opNeedsConsent, opFailed)
	if v.State != opNeedsConsent || v.Extension != "example-ui" || v.Version != "1.0.0" || v.Market != market || v.PreviewID == "" {
		t.Fatalf("%+v", v)
	}
	for _, s := range []string{extension.StepDownload, extension.StepVerify, extension.StepUnpack, extension.StepCheck} {
		if st := stepState(v, s); st.State != extension.StepDone {
			t.Fatalf("%s: %+v", s, st)
		}
	}
	if d := stepState(v, extension.StepDownload); d.Bytes != int64(len(v1)) || d.Cached {
		t.Fatalf("download: %+v", d)
	}
	code, one := call(t, mux, "GET", "/api/v1/extensions/-/operations/"+id, phone, "")
	pv, _ := one["preview"].(map[string]any)
	if code != 200 || pv["preview_id"] != v.PreviewID {
		t.Fatalf("its preview: %d %v", code, one)
	}
	_, list := call(t, mux, "GET", "/api/v1/extensions/-/operations", phone, "")
	if ops, _ := list["operations"].([]any); len(ops) != 1 {
		t.Fatalf("listed: %v", list)
	}

	// Agreed to: installed and started in the background, in the same operation.
	body := fmt.Sprintf(`{"preview_id": %q, "permissions_digest": %q, "async": true}`, pv["preview_id"], pv["permissions_digest"])
	if got := opCall(t, mux, "POST", "/api/v1/extensions/-/install", phone, body); got != id {
		t.Fatalf("another operation: %s", got)
	}
	v = waitOp(t, h, id, opSucceeded, opFailed)
	if v.State != opSucceeded || stepState(v, extension.StepStart).State != extension.StepDone || stepState(v, extension.StepNodes).State != extension.StepSkipped || v.EndedAt == nil {
		t.Fatalf("%+v", v)
	}
	if h.extensions.reg.Get("example-ui") == nil {
		t.Fatal("not installed")
	}

	// Uninstalled in steps; then previewed again: the package comes from the cache.
	id = opCall(t, mux, "DELETE", "/api/v1/extensions/example-ui/-/?async=1", phone, "")
	if v = waitOp(t, h, id, opSucceeded, opFailed); v.State != opSucceeded || v.Kind != opUninstall || stepState(v, extension.StepRemove).State != extension.StepDone {
		t.Fatalf("%+v", v)
	}
	if code, _ := call(t, mux, "DELETE", "/api/v1/extensions/-/operations/"+id, phone, ""); code != http.StatusNoContent || h.opByID(id) != nil {
		t.Fatalf("a finished uninstall taken off the list: %d", code)
	}
	id = opCall(t, mux, "POST", "/api/v1/extensions/-/catalog/preview", phone, preview)
	if v = waitOp(t, h, id, opNeedsConsent, opFailed); !stepState(v, extension.StepDownload).Cached {
		t.Fatalf("not from the cache: %+v", v)
	}
	// Turned down: canceled, its preview dropped.
	if code, _ := call(t, mux, "DELETE", "/api/v1/extensions/-/operations/"+id, phone, ""); code != http.StatusNoContent {
		t.Fatalf("cancel: %d", code)
	}
	if v = waitOp(t, h, id, opCanceled); h.extensions.reg.PreviewOf(v.PreviewID) != nil {
		t.Fatal("its preview is kept")
	}

	// A download that stalls fails at its step, and a retry downloads it again.
	defer func(was time.Duration) { extension.StallAfter = was }(extension.StallAfter)
	extension.StallAfter = 300 * time.Millisecond
	_ = os.RemoveAll(h.packageCache())
	gh.mu.Lock()
	gh.stall = true
	gh.mu.Unlock()
	id = opCall(t, mux, "POST", "/api/v1/extensions/-/catalog/preview", phone, preview)
	v = waitOp(t, h, id, opFailed, opNeedsConsent)
	if v.State != opFailed || v.ErrorCode != "download_stalled" || v.Step != extension.StepDownload || stepState(v, extension.StepDownload).State != extension.StepFailed || !v.Retryable || v.Error == "" {
		t.Fatalf("%+v", v)
	}
	if d := stepState(v, extension.StepDownload); d.Bytes == 0 {
		t.Fatalf("its bytes so far: %+v", d)
	}
	gh.mu.Lock()
	gh.stall = false
	gh.mu.Unlock()
	next := opCall(t, mux, "POST", "/api/v1/extensions/-/operations/"+id+"/retry", phone, "")
	if v = waitOp(t, h, next, opNeedsConsent, opFailed); v.State != opNeedsConsent {
		t.Fatalf("retried: %+v", v)
	}
	if code, _ := call(t, mux, "POST", "/api/v1/extensions/-/operations/"+next+"/retry", phone, ""); code != http.StatusConflict {
		t.Fatalf("a retry of one that did not fail: %d", code)
	}

	// Agreed to, then refused when installing: a retry goes on without asking again (the same digest).
	var refuse atomic.Bool
	refuse.Store(true)
	was := h.extensions.reg.CanEnable
	h.extensions.reg.CanEnable = func(ctx context.Context, source string, m *extension.Manifest) error {
		if refuse.Load() {
			return errors.New("not now")
		}
		if was != nil {
			return was(ctx, source, m)
		}
		return nil
	}
	_, one = call(t, mux, "GET", "/api/v1/extensions/-/operations/"+next, phone, "")
	pv = one["preview"].(map[string]any)
	code, out := call(t, mux, "POST", "/api/v1/extensions/-/install", phone, fmt.Sprintf(`{"preview_id": %q, "permissions_digest": %q}`, pv["preview_id"], pv["permissions_digest"]))
	if code != http.StatusBadRequest {
		t.Fatalf("refused: %d %v", code, out)
	}
	if v = waitOp(t, h, next, opFailed); v.Step != extension.StepInstall || v.ErrorCode != "install_failed" {
		t.Fatalf("%+v", v)
	}
	refuse.Store(false)
	again := opCall(t, mux, "POST", "/api/v1/extensions/-/operations/"+next+"/retry", phone, "")
	if v = waitOp(t, h, again, opSucceeded, opFailed, opNeedsConsent); v.State != opSucceeded || stepState(v, extension.StepConsent).State != extension.StepDone {
		t.Fatalf("went on by itself: %+v", v)
	}

	// A synchronous call (an older app) is an operation too, and names it.
	code, out = call(t, mux, "DELETE", "/api/v1/extensions/example-ui/-/", phone, "")
	if code != 200 || out["ok"] != true {
		t.Fatalf("uninstall: %d %v", code, out)
	}
	code, out = call(t, mux, "POST", "/api/v1/extensions/-/catalog/preview", phone, fmt.Sprintf(`{"market": %q, "extension": "example-ui"}`, market))
	if code != 200 || out["preview_id"] == nil || out["operation_id"] == nil {
		t.Fatalf("synchronous preview: %d %v", code, out)
	}
}

// opCall makes a call that answers 202 with an operation, and is its id.
func opCall(t *testing.T, h http.Handler, method, path, token, body string) string {
	t.Helper()
	code, out := call(t, h, method, path, token, body)
	return opID(t, code, out)
}

// TestMarketClientGitHubProxy: market requests to GitHub's hosts go through hub.json's github.proxy, others as the
// environment says (#3277: a package from GitHub stalled for minutes without it).
func TestMarketClientGitHubProxy(t *testing.T) {
	h, _, _ := marketHub(t)
	h.cfg.GitHub = &github.Config{Egress: connector.Egress{Proxy: "http://127.0.0.1:3128"}}
	h.extensions.markets.client = nil
	tr := h.marketClient().Transport.(*http.Transport)
	for host, want := range map[string]string{"api.github.com": "127.0.0.1:3128", "objects.githubusercontent.com": "127.0.0.1:3128",
		"github.com": "127.0.0.1:3128", "example.com": "", "notgithub.com": ""} {
		req, _ := http.NewRequest("GET", "https://"+host+"/x", nil)
		u, err := tr.Proxy(req)
		got := ""
		if u != nil {
			got = u.Host
		}
		if err != nil || (got != want && !(want == "" && os.Getenv("HTTPS_PROXY") != "")) {
			t.Errorf("%s: %q %v, want %q", host, got, err, want)
		}
	}
}
