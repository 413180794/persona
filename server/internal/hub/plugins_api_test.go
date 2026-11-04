package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/connector/serve"
	"github.com/nwverse/persona/server/internal/plugin"
	"github.com/nwverse/persona/server/internal/proto"
)

// notesConnector is the connector an installed test plugin contributes: signed in with the API key the user filled
// in (a declared secret), its folders from the plugin's configuration.
type notesConnector struct {
	key     string
	folders []string
}

func (notesConnector) ID() string   { return "examplenotes" }
func (notesConnector) Name() string { return "Example Notes" }
func (notesConnector) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapStatus, connector.CapTools}, Hosts: []string{"notes.example.com"}}
}
func (n notesConnector) Status(context.Context) connector.Status {
	if n.key == "" {
		return connector.Status{State: connector.StateNotConfigured}
	}
	return connector.Status{State: connector.StateOK, Account: "key " + n.key[:2] + "…"}
}
func (n notesConnector) Tools() []proto.Tool { return []proto.Tool{{Name: "examplenotes_search"}} }
func (n notesConnector) Call(_ context.Context, name string, _ json.RawMessage) (string, error) {
	return fmt.Sprintf("%s in %s", name, strings.Join(n.folders, ",")), nil
}

func serveConnectorPluginForTest() {
	_ = serve.MainWith(context.Background(), os.Stdin, os.Stdout, func(i serve.Init, _ connector.Secrets) (connector.Connector, error) {
		var cfg struct {
			Folders []string `json:"folders"`
		}
		_ = json.Unmarshal(i.Config, &cfg)
		return notesConnector{key: i.Declared["api_key"], folders: cfg.Folders}, nil
	})
}

// pluginStore is a hub store that keeps plugins too (the database's tables, in memory).
type pluginStore struct {
	*keepStore
	mu      sync.Mutex
	rows    map[string]plugin.Record
	secrets map[string]map[string]string
	audit   []string
}

func (s *pluginStore) Plugins(context.Context) ([]plugin.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []plugin.Record
	for _, r := range s.rows {
		out = append(out, r)
	}
	return out, nil
}
func (s *pluginStore) PutPlugin(_ context.Context, r plugin.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[r.ID] = r
	return nil
}
func (s *pluginStore) DeletePlugin(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rows, id)
	delete(s.secrets, id)
	return nil
}
func (s *pluginStore) PluginSecrets(_ context.Context, id string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for k, v := range s.secrets[id] {
		out[k] = v
	}
	return out, nil
}
func (s *pluginStore) PutPluginSecret(_ context.Context, id, name, sealed string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.secrets[id] == nil {
		s.secrets[id] = map[string]string{}
	}
	s.secrets[id][name] = sealed
	return nil
}
func (s *pluginStore) DeletePluginSecret(_ context.Context, id, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.secrets[id], name)
	return nil
}
func (s *pluginStore) AddPluginAudit(_ context.Context, id, action, actor string, _ json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audit = append(s.audit, id+" "+action+" "+actor)
	return nil
}

// notesPackage writes a plugin package whose program is this test binary serving notesConnector.
func notesPackage(t *testing.T, version string) string {
	dir := t.TempDir()
	self, _ := os.Executable()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "notes"), []byte("#!/bin/sh\nJARVIS_TEST_PLUGIN=connector exec "+self+" \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`{"manifest_version": 1, "id": "example-notes", "name": "Example Notes", "version": %q,
		"engines": {"plugin_protocol": 1},
		"runtime": {"command": {%q: "bin/notes"}},
		"contributes": {"connectors": [{"id": "examplenotes", "name": "Example Notes", "capabilities": ["status", "tools"], "hosts": ["notes.example.com"]}]},
		"configuration": {"type": "object", "properties": {"folders": {"type": "array", "title": "文件夹", "items": {"type": "string"}}}},
		"secrets": [{"name": "api_key", "title": "API Key", "required": true}],
		"permissions": {"network": ["notes.example.com"]}}`, version, runtime.GOOS+"-"+runtime.GOARCH)
	if err := os.WriteFile(filepath.Join(dir, plugin.ManifestFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func pluginTestHub(t *testing.T) (*Hub, *pluginStore) {
	h := newTestHub(t)
	store := &pluginStore{keepStore: newKeepStore(), rows: map[string]plugin.Record{}, secrets: map[string]map[string]string{}}
	h.store = store
	h.connectors = connector.NewRegistry(nil)
	h.plugins = newPlugins(h.cfg.dir, nil, h.logf)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		for _, p := range h.plugins.reg.List() {
			if hp := h.plugins.get(p.Record.ID); hp != nil && hp.proc != nil {
				hp.proc.Stop()
			}
		}
	})
	h.startConnectors(ctx)
	h.startPlugins(ctx)
	return h, store
}

func waitLong(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// Installed from a directory on this machine, set up from the app, its connector running in the plugin's program;
// switched off and on, upgraded, removed with what the hub kept for it.
func TestInstalledConnectorPlugin(t *testing.T) {
	h, store := pluginTestHub(t)
	mux := h.routes()
	master := h.cfg.Token
	pkg := notesPackage(t, "1.0.0")

	code, out := call(t, mux, "POST", "/api/v1/plugins/-/preview", "", `{"source": "dir:`+pkg+`"}`)
	if code == 200 {
		t.Fatal("previewed without the master token")
	}
	code, pv := call(t, mux, "POST", "/api/v1/plugins/-/preview", master, `{"source": "dir:`+pkg+`"}`)
	if code != 200 || pv["permissions_digest"] == "" || len(pv["permissions"].([]any)) != 3 {
		t.Fatalf("%d %v", code, pv)
	}
	code, out = call(t, mux, "POST", "/api/v1/plugins/-/install", master, fmt.Sprintf(`{"preview_id": %q, "permissions_digest": %q}`, pv["preview_id"], pv["permissions_digest"]))
	if code != 200 || out["status"] != pluginNeedsSetup || out["source"] != "installed" {
		t.Fatalf("installed: %d %v", code, out)
	}
	if h.connectors.Get("examplenotes") != nil {
		t.Fatal("no connector before its key is filled in")
	}

	code, out = call(t, mux, "PUT", "/api/v1/plugins/example-notes/-/config", master, `{"config": {"folders": "work"}}`)
	if code != 400 || out["field"] != "folders" {
		t.Fatalf("held to the schema: %d %v", code, out)
	}
	if code, out = call(t, mux, "PUT", "/api/v1/plugins/example-notes/-/config", master, `{"config": {"folders": ["work"]}}`); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	code, out = call(t, mux, "PUT", "/api/v1/plugins/example-notes/-/secrets/api_key", master, `{"value": "k3y-from-the-app"}`)
	if code != 200 || out["secrets"].([]any)[0].(map[string]any)["set"] != true {
		t.Fatalf("%d %v", code, out)
	}
	if b, _ := json.Marshal(out); strings.Contains(string(b), "k3y-from-the-app") {
		t.Fatal("a secret is never sent back")
	}
	for _, sealed := range store.secrets["example-notes"] {
		if strings.Contains(sealed, "k3y") {
			t.Fatal("kept sealed")
		}
	}

	waitLong(t, "the connector signed in with the key", func() bool {
		c := h.connectors.Get("examplenotes")
		return c != nil && connector.StatusOf(context.Background(), c).State == connector.StateOK
	})
	if st := connector.StatusOf(context.Background(), h.connectors.Get("examplenotes")); st.Account != "key k3…" {
		t.Fatal(st)
	}
	res := h.callConnector(context.Background(), "", "examplenotes_search", json.RawMessage(`{}`))
	if res.IsError || len(res.Content) != 1 || !strings.Contains(res.Content[0].Text, "examplenotes_search in work") {
		t.Fatalf("%+v", res)
	}
	_, list := call(t, mux, "GET", "/api/v1/plugins", master, "")
	var notes map[string]any
	for _, p := range list["plugins"].([]any) {
		if p.(map[string]any)["id"] == "example-notes" {
			notes = p.(map[string]any)
		}
	}
	if notes["status"] != "running" || notes["kind"] != "connectors" {
		t.Fatalf("%v", list["plugins"])
	}

	if code, _ := call(t, mux, "GET", "/api/v1/plugins/example-notes/-/anything", master, ""); code != 404 {
		t.Fatal("the hub's segment is never relayed:", code)
	}

	// Switched off: its connector goes; on again: it comes back.
	if code, out = call(t, mux, "PUT", "/api/v1/plugins/example-notes/-/enabled", master, `{"enabled": false}`); code != 200 || out["status"] != "disabled" {
		t.Fatalf("%d %v", code, out)
	}
	if h.connectors.Get("examplenotes") != nil {
		t.Fatal("still registered")
	}
	call(t, mux, "PUT", "/api/v1/plugins/example-notes/-/enabled", master, `{"enabled": true}`)
	waitLong(t, "the connector back", func() bool {
		c := h.connectors.Get("examplenotes")
		return c != nil && connector.StatusOf(context.Background(), c).State == connector.StateOK
	})

	// The user's connector switch holds the plugin's process: off, it stops.
	if err := h.setConnectorEnabled(context.Background(), "examplenotes", false); err != nil {
		t.Fatal(err)
	}
	waitLong(t, "the program to stop", func() bool {
		st, _ := h.plugins.get("example-notes").proc.Status()
		return st == "stopped"
	})
	if err := h.setConnectorEnabled(context.Background(), "examplenotes", true); err != nil {
		t.Fatal(err)
	}

	// Upgrade (same permissions): the new version runs.
	pkg2 := notesPackage(t, "1.1.0")
	_, pv = call(t, mux, "POST", "/api/v1/plugins/-/preview", master, `{"source": "dir:`+pkg2+`", "upgrade": "example-notes"}`)
	code, out = call(t, mux, "POST", "/api/v1/plugins/example-notes/-/upgrade", master, fmt.Sprintf(`{"preview_id": %q}`, pv["preview_id"]))
	if code != 200 || out["version"] != "1.1.0" || out["previous_version"] != "1.0.0" {
		t.Fatalf("%d %v", code, out)
	}
	waitLong(t, "the new version's connector", func() bool {
		c := h.connectors.Get("examplenotes")
		return c != nil && connector.StatusOf(context.Background(), c).State == connector.StateOK
	})

	// Removed: the plugin, its connector and what the hub kept for it go.
	_ = store.PutConnectorSecret(context.Background(), "examplenotes", "sealed session")
	if code, out = call(t, mux, "DELETE", "/api/v1/plugins/example-notes/-/", master, ""); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	if h.connectors.Get("examplenotes") != nil || h.plugins.reg.Get("example-notes") != nil || len(store.rows) != 0 {
		t.Fatal("still there")
	}
	if _, ok, _ := store.ConnectorSecret(context.Background(), "examplenotes"); ok {
		t.Fatal("its connector's secret stays")
	}
	var cs connectorSettings
	_, _ = store.Setting(context.Background(), connectorSettingsKey, &cs)
	if _, ok := cs.Switch["examplenotes"]; ok {
		t.Fatal("its connector's switch stays")
	}
	actions := strings.Join(store.audit, "\n")
	for _, a := range []string{"install personad plugin", "config hub token", "secret_set hub token", "disabled hub token", "upgrade personad plugin", "uninstall personad plugin"} {
		if !strings.Contains(actions, a) {
			t.Errorf("audit has no %q:\n%s", a, actions)
		}
	}
}

// A phone (a device token) sets plugins up but installs nothing; an answer-only device does neither.
func TestPluginAPIRights(t *testing.T) {
	h, _ := pluginTestHub(t)
	mux := h.routes()
	for _, path := range []string{"/api/v1/plugins/-/preview", "/api/v1/plugins/-/install", "/api/v1/plugins/x/-/upgrade", "/api/v1/plugins/x/-/rollback"} {
		req := `{}`
		code, out := callFrom(t, mux, "POST", path, h.cfg.Token, req, "[fd00::10]:1234")
		if code != 403 || out["code"] != "this_machine_only" {
			t.Errorf("%s from another machine: %d %v", path, code, out)
		}
	}
}

// callFrom is call from the address from.
func callFrom(t *testing.T, h http.Handler, method, path, token, body, from string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = from
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}
