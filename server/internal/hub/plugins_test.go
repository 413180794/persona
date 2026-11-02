package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/chyroc/jarvis/server/internal/db/dbtest"
)

// TestMain doubles as a plugin: re-run with JARVIS_TEST_PLUGIN=1 the test binary serves on JARVIS_PLUGIN_SOCKET.
// Otherwise the tests run on a database of their own (dbtest, #583).
func TestMain(m *testing.M) {
	if os.Getenv("JARVIS_TEST_PLUGIN") == "1" {
		servePluginForTest()
		return
	}
	os.Exit(dbtest.Main(m))
}

func servePluginForTest() {
	ln, err := net.Listen("unix", os.Getenv("JARVIS_PLUGIN_SOCKET"))
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /echo", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"path": r.URL.Path, "query": r.URL.RawQuery, "auth": r.Header.Get("Authorization"),
			"hub": os.Getenv("JARVIS_HUB_URL"), "id": os.Getenv("JARVIS_PLUGIN_ID"), "extra": os.Getenv("EXTRA"),
			"config": os.Getenv("JARVIS_PLUGIN_CONFIG"),
		})
	})
	mux.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		for {
			typ, b, err := c.Read(r.Context())
			if err != nil {
				return
			}
			if err := c.Write(r.Context(), typ, b); err != nil {
				return
			}
		}
	})
	_ = http.Serve(ln, mux)
}

func TestPluginRelay(t *testing.T) {
	dir, err := os.MkdirTemp("", "jvp") // short: a unix socket path must stay under ~100 bytes
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	self, _ := os.Executable()
	h := &Hub{cfg: &Config{Listen: ":18999", Token: "secret"}, logger: log.New(io.Discard, "", 0)}
	h.plugins = newPlugins(dir, []PluginConfig{
		{ID: "voice", Kind: "voice", Command: self, Env: map[string]string{"JARVIS_TEST_PLUGIN": "1", "EXTRA": "x"},
			Config: json.RawMessage(`{"providers":[]}`)},
		{ID: "off", Kind: "voice", Command: self, Disabled: true},
		{ID: "Bad ID", Command: self},
	}, h.logf)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.startPlugins(ctx)
	api := http.NewServeMux()
	h.pluginRoutes(api)
	srv := httptest.NewServer(api)
	defer srv.Close()

	deadline := time.Now().Add(10 * time.Second)
	for {
		var got struct{ Plugins []PluginView }
		res, err := http.Get(srv.URL + "/api/v1/plugins")
		if err != nil {
			t.Fatal(err)
		}
		_ = json.NewDecoder(res.Body).Decode(&got)
		res.Body.Close()
		if len(got.Plugins) != 2 || got.Plugins[0].ID != "off" || got.Plugins[0].Status != "disabled" {
			t.Fatalf("%+v", got.Plugins)
		}
		if got.Plugins[1].Status == "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("never running: %+v", got.Plugins[1])
		}
		time.Sleep(100 * time.Millisecond)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/plugins/voice/echo?token=secret&a=1", nil)
	req.Header.Set("Authorization", "Bearer secret")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var echo map[string]string
	_ = json.NewDecoder(res.Body).Decode(&echo)
	res.Body.Close()
	want := map[string]string{"path": "/echo", "query": "a=1", "auth": "", "hub": "http://127.0.0.1:18999", "id": "voice", "extra": "x", "config": `{"providers":[]}`}
	if fmt.Sprint(echo) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", echo, want)
	}

	for _, path := range []string{"/api/v1/plugins/off/echo", "/api/v1/plugins/nope/echo"} {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: %d", path, res.StatusCode)
		}
	}

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/plugins/voice/ws"
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	for _, m := range []struct {
		typ  websocket.MessageType
		data []byte
	}{{websocket.MessageBinary, []byte{0, 1, 2, 255}}, {websocket.MessageText, []byte(`{"type":"start"}`)}} {
		if err := c.Write(ctx, m.typ, m.data); err != nil {
			t.Fatal(err)
		}
		typ, b, err := c.Read(ctx)
		if err != nil || typ != m.typ || string(b) != string(m.data) {
			t.Fatalf("echo %v %q %v", typ, b, err)
		}
	}

	if _, err := os.Stat(filepath.Join(dir, "plugins", "voice.sock")); err != nil {
		t.Fatal(err)
	}
}

func TestHubLocalURL(t *testing.T) {
	for in, want := range map[string]string{":18660": "http://127.0.0.1:18660", "0.0.0.0:80": "http://127.0.0.1:80",
		"hub.example.com:9": "http://hub.example.com:9", "bad": "http://127.0.0.1:18660"} {
		if got := hubLocalURL(in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}

func TestPluginElsewhere(t *testing.T) {
	dir, err := os.MkdirTemp("", "jvp")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "far.sock")
	h := &Hub{cfg: &Config{Listen: ":18999", Token: "secret"}, logger: log.New(io.Discard, "", 0)}
	h.plugins = newPlugins(dir, []PluginConfig{{ID: "far", Kind: "voice", Socket: sock}}, h.logf)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.startPlugins(ctx)
	status := func() string {
		for i := 0; i < 100; i++ {
			if s := h.plugins.list()[0].Status; s != "starting" {
				return s
			}
			time.Sleep(20 * time.Millisecond)
		}
		return "starting"
	}
	if s := status(); s != "error" {
		t.Fatalf("no socket yet: %s", s)
	}
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	p := h.plugins.byID["far"]
	if !p.healthy(ctx) {
		t.Fatal("socket answers")
	}
}
