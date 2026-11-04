package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/plugin"
	pserve "github.com/nwverse/persona/server/internal/plugin/serve"
)

// serveAPIPluginForTest serves part of the app's API (/api/v1/examplefeed/…, and /api/v1/lark/… taken over from the
// hub) on its socket, and pushes two events once initialized: one of a type it declared, one it did not.
func serveAPIPluginForTest() {
	ln, err := net.Listen("unix", os.Getenv("PERSONA_PLUGIN_SOCKET"))
	if err != nil {
		os.Exit(1)
	}
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/cached") {
			w.Header().Set("Cache-Control", "private, max-age=3600")
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"path": r.URL.Path, "raw": r.URL.EscapedPath(), "device": r.Header.Get("X-Persona-Device"),
			"auth": r.Header.Get("Authorization"), "forged": r.Header.Get("X-Persona-Forged")})
	}))
	_ = pserve.Serve(context.Background(), os.Stdin, os.Stdout, pserve.Program{ID: "example-feed", Version: "1.0.0",
		Init: func(_ context.Context, _ plugin.InitializeParams, h *pserve.Hub) (map[string]any, error) {
			go func() {
				time.Sleep(300 * time.Millisecond)
				_ = h.Notify("api.broadcast", map[string]any{"type": "examplefeed", "n": 1})
				_ = h.Notify("api.broadcast", map[string]any{"type": "sessions", "n": 2})
			}()
			return nil, nil
		}})
}

func apiPackage(t *testing.T) string {
	dir := t.TempDir()
	self, _ := os.Executable()
	_ = os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, "skills", "example-feed"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "bin", "feed"), []byte("#!/bin/sh\nJARVIS_TEST_PLUGIN=api exec "+self+" \"$@\"\n"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "skills", "example-feed", "SKILL.md"), []byte("---\nname: example-feed\ndescription: Read the example feed.\n---\nUse it.\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "assistant.md"), []byte("## Example feed\n\nThe example feed is read with its skill.\n"), 0o644)
	manifest := fmt.Sprintf(`{"manifest_version": 1, "id": "example-feed", "name": "Example feed", "version": "1.0.0",
		"engines": {"plugin_protocol": 1}, "runtime": {"command": {%q: "bin/feed"}, "http": true},
		"activation_events": ["on_startup"],
		"contributes": {
			"api": [{"id": "feed", "prefix": "examplefeed", "features": ["example_feed"], "events": ["examplefeed"]},
			        {"id": "lark", "prefix": "lark"}, {"id": "mine", "prefix": "machines"}],
			"assistant": [{"id": "feed", "skills": ["skills/example-feed"], "instructions": "assistant.md"}]}}`, runtime.GOOS+"-"+runtime.GOARCH)
	_ = os.WriteFile(filepath.Join(dir, plugin.ManifestFile), []byte(manifest), 0o644)
	return dir
}

// A plugin's part of the app's API: relayed with who is asking (never the credential), its features added to the
// hub's, its declared events pushed to the apps; a prefix being moved out of the hub is the plugin's, the hub's own
// are not; its skills and instructions reach the assistant.
func TestPluginAPI(t *testing.T) {
	h, _ := pluginTestHub(t)
	mux := h.routes()
	master := h.cfg.Token
	phone := &client{out: make(chan []byte, 16), subs: map[[2]string]bool{}, role: roleFull}
	h.mu.Lock()
	h.clients[phone] = struct{}{}
	h.mu.Unlock()

	_, pv := call(t, mux, "POST", "/api/v1/plugins/-/preview", master, `{"source": "dir:`+apiPackage(t)+`"}`)
	if code, out := call(t, mux, "POST", "/api/v1/plugins/-/install", master,
		fmt.Sprintf(`{"preview_id": %q, "permissions_digest": %q}`, pv["preview_id"], pv["permissions_digest"])); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	waitLong(t, "the plugin up", func() bool {
		hp := h.plugins.get("example-feed")
		return hp != nil && hp.proc != nil && hp.proc.Ready()
	})

	req, _ := http.NewRequest("GET", "/api/v1/examplefeed/items?x=1", nil)
	req.Header.Set("X-Persona-Forged", "nope")
	req.Header.Set("X-Persona-Device", "someone-else")
	code, out := callReq(t, mux, req, master)
	if code != 200 || out["path"] != "/api/v1/examplefeed/items" || out["device"] != "" || out["auth"] != "" {
		t.Fatalf("relayed: %d %v", code, out)
	}
	if out["forged"] != "" {
		t.Fatal("the caller's own X-Persona headers are dropped")
	}
	if _, out := call(t, mux, "GET", "/api/v1/lark/chats", master, ""); out["path"] != "/api/v1/lark/chats" {
		t.Fatalf("a prefix being moved out of the hub is the plugin's: %v", out)
	}
	if _, out := call(t, mux, "GET", "/api/v1/lark/raw/mail/threads/a%2Fb", master, ""); out["raw"] != "/api/v1/lark/raw/mail/threads/a%2Fb" {
		t.Fatalf("an escaped / reaches the plugin escaped: %v", out)
	}
	// The plugin's own Cache-Control reaches the app alone; without one the answer is no-store, as the hub's own are.
	for path, want := range map[string]string{"/api/v1/lark/emojis/cached": "private, max-age=3600", "/api/v1/lark/chats": "no-store",
		"/api/v1/plugins/example-feed/cached": "private, max-age=3600"} {
		req, _ := http.NewRequest("GET", path, nil)
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Authorization", "Bearer "+master)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if got := w.Header().Values("Cache-Control"); w.Code != 200 || len(got) != 1 || got[0] != want {
			t.Fatalf("%s: %d Cache-Control %q, want %q", path, w.Code, got, want)
		}
	}
	if _, out := call(t, mux, "GET", "/api/v1/machines", master, ""); out["path"] != nil || out["machines"] == nil {
		t.Fatalf("the hub's own API is not a plugin's: %v", out)
	}
	if _, out := call(t, mux, "GET", "/api/v1/machines/nosuch/zzz", master, ""); out["path"] != nil {
		t.Fatalf("nor any path under the hub's own prefix: %v", out)
	}

	if !slices.Contains(h.features(), "example_feed") {
		t.Fatal("the plugin's feature is the hub's")
	}
	select {
	case b := <-phone.out:
		var ev map[string]any
		_ = json.Unmarshal(b, &ev)
		if ev["type"] != "examplefeed" || ev["n"] != float64(1) {
			t.Fatalf("%s", b)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the plugin's event did not reach the app")
	}
	select {
	case b := <-phone.out:
		t.Fatalf("an event of a type it did not declare went out: %s", b)
	case <-time.After(700 * time.Millisecond):
	}

	if md := h.plugins.pluginInstructions(); !strings.Contains(md, "The example feed is read with its skill.") {
		t.Fatalf("instructions: %q", md)
	}
	first := h.plugins.pluginSkillsFor("mac")
	if len(first) != 1 || first[0].Name != "example-feed" || string(first[0].Files["SKILL.md"]) == "" {
		t.Fatalf("%+v", first)
	}
	if again := h.plugins.pluginSkillsFor("mac"); again[0].Files != nil || again[0].Digest != first[0].Digest {
		t.Fatal("the files go once")
	}
	if other := h.plugins.pluginSkillsFor("other"); other[0].Files == nil {
		t.Fatal("each machine gets them once")
	}

	// Switched off: its API, features and skills go.
	call(t, mux, "PUT", "/api/v1/plugins/example-feed/-/enabled", master, `{"enabled": false}`)
	if _, out := call(t, mux, "GET", "/api/v1/examplefeed/items", master, ""); out["path"] != nil {
		t.Fatalf("still relayed: %v", out)
	}
	if slices.Contains(h.features(), "example_feed") || len(h.plugins.pluginSkills()) != 0 {
		t.Fatal("switched off, it adds nothing")
	}
}

func callReq(t *testing.T, h http.Handler, req *http.Request, token string) (int, map[string]any) {
	t.Helper()
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

// A plugin connector's kinds reach the models in its own words.
func TestPluginKindDescriptions(t *testing.T) {
	pluginKinds.Store("examplenotes/shared", "Example Notes 里有人共享了一篇笔记给用户")
	defer pluginKinds.Delete("examplenotes/shared")
	it := &InboxItem{Source: "examplenotes", Kind: "shared", Context: "（正文）", Connector: &InboxConnector{}}
	if got := triageInput(it); !strings.HasPrefix(got, "类型：Example Notes 里有人共享了一篇笔记给用户\n") {
		t.Fatalf("%q", got)
	}
}
