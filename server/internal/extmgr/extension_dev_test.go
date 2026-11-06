package extmgr

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
)

// `personad extension dev`'s side in the hub: an extension run from its own directory, in memory only, its log and
// the hub's calls streamed, restarted on demand, taken away; only from this machine.
func TestDevExtension(t *testing.T) {
	h, store := extensionTestHub(t)
	mux := h.routes()
	master := h.cfg.Token
	dir := notesPackage(t, "1.0.0")
	body := `{"dir": "` + dir + `", "secrets": {"api_key": "k3y"}}`

	if code, out := callFrom(t, mux, "POST", "/api/v1/extensions/-/dev", master, body, "[fd00::10]:1234"); code != 403 || out["code"] != "this_machine_only" {
		t.Fatalf("from another machine: %d %v", code, out)
	}
	if code, out := call(t, mux, "POST", "/api/v1/extensions/-/dev", master, `{"dir": "relative/path"}`); code != 400 {
		t.Fatalf("a relative dir: %d %v", code, out)
	}
	code, out := call(t, mux, "POST", "/api/v1/extensions/-/dev", master, body)
	if code != 200 || out["source"] != "dev" || out["enabled"] != true {
		t.Fatalf("dev: %d %v", code, out)
	}
	waitLong(t, "the dev extension's connector signed in with the secret it was given", func() bool {
		c := h.connectors.Get("examplenotes")
		return c != nil && connector.StatusOf(context.Background(), c).State == connector.StateOK
	})
	if len(store.rows) != 0 || len(store.secrets) != 0 {
		t.Fatalf("a dev extension is not kept: %v %v", store.rows, store.secrets)
	}
	if code, _ := call(t, mux, "PUT", "/api/v1/extensions/example-notes/-/enabled", master, `{"enabled": false}`); code == 200 {
		t.Fatal("a dev extension's switch is its command's, not the app's")
	}

	// The log stream hears the hub's calls to it.
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/v1/extensions/example-notes/-/dev/logs", nil)
	req.Header.Set("Authorization", "Bearer "+master)
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("logs: %v %v", err, res)
	}
	defer res.Body.Close()
	lines := bufio.NewScanner(res.Body)
	lines.Scan() // "connected"
	go func() {
		_ = h.extensions.get("example-notes").proc.Call(ctx, "connector.status", map[string]string{"contribution": "examplenotes"}, nil)
	}()
	heard := false
	for !heard && lines.Scan() {
		heard = strings.HasPrefix(lines.Text(), "→ connector.")
	}
	if !heard {
		t.Fatalf("the stream did not hear a call: %v", lines.Err())
	}

	if code, out := call(t, mux, "POST", "/api/v1/extensions/example-notes/-/dev/restart", master, ``); code != 200 {
		t.Fatalf("restart: %d %v", code, out)
	}
	if code, out := call(t, mux, "DELETE", "/api/v1/extensions/example-notes/-/dev", master, ``); code != 200 {
		t.Fatalf("remove: %d %v", code, out)
	}
	if h.extensions.reg.Get("example-notes") != nil {
		t.Fatal("still there")
	}
	waitLong(t, "its connector left", func() bool { return h.connectors.Get("examplenotes") == nil })
	if code, _ := call(t, mux, "POST", "/api/v1/extensions/example-notes/-/dev/restart", master, ``); code != 404 {
		t.Fatal("restarted one that is gone")
	}
}
