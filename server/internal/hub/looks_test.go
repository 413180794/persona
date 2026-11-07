package hub

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/compat"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extwire"
)

// copyDir copies the example package so a test may change it.
func copyDir(t *testing.T, from string) string {
	t.Helper()
	to := t.TempDir()
	err := filepath.Walk(from, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		if fi.IsDir() {
			return os.MkdirAll(filepath.Join(to, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return to
}

func exampleLooksDir(t *testing.T) string {
	return copyDir(t, filepath.Join("..", "..", "..", "schema", "extension", "examples", "example-looks"))
}

func lookByID(t *testing.T, out map[string]any, id string) map[string]any {
	t.Helper()
	for _, l := range out["looks"].([]any) {
		if m := l.(map[string]any); m["id"] == compat.LegacyLook(id) {
			return m
		}
	}
	return nil
}

// lookReport is the example extension's looks as the manager reports them (extwire.Report.Looks), SVGs cleaned.
func lookReport(t *testing.T, dir string, ids ...string) []extwire.LookPackage {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, extension.ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	m, err := extension.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	var out []extwire.LookPackage
	for _, l := range m.Looks() {
		if len(ids) > 0 && !slices.Contains(ids, l.ID) {
			continue
		}
		files := map[string][]byte{}
		for _, f := range l.Files() {
			b, err := extension.ReadLookFile(dir, f)
			if err == nil && strings.HasSuffix(f, ".svg") {
				b, err = extension.CleanSVG(b)
			}
			if err != nil {
				t.Fatal(err)
			}
			files[f] = b
		}
		item, _ := json.Marshal(l)
		out = append(out, extwire.LookPackage{Extension: m.ID, ExtensionName: m.Name, Version: m.Version, Item: item, Files: files})
	}
	return out
}

// reportLooks is the manager's report of the extensions' looks as the hub takes it (extensions.report).
func reportLooks(h *Hub, looks []extwire.LookPackage) {
	r := extwire.Report{Looks: looks}
	var was []extwire.LookPackage
	if old := h.ext.report.Swap(&r); old != nil {
		was = old.Looks
	}
	h.looksReported(was, looks)
}

// The extensions' looks the manager reports: listed with the built-in ones, served as persona packages (only the
// files they name), chosen for every device, the default again when no longer reported (switched off), for good when
// removed (extmgr TestExtensionLooksReported has the manager's side).
func TestExtensionLooks(t *testing.T) {
	h := newTestHub(t)
	h.store = &settingsStore{vals: map[string][]byte{}}
	mux := h.routes()
	master := h.cfg.Token
	app := appClient(h)

	_, out := call(t, mux, "GET", "/api/v1/looks", master, "")
	if len(out["looks"].([]any)) != 1 || lookByID(t, out, "persona")["source"] != "builtin" || lookByID(t, out, "persona")["app_icon"] != "persona" {
		t.Fatalf("before: the built-in look only: %v", out)
	}
	if lookByID(t, out, "persona")["name"] != "团子" {
		t.Fatalf("Mochi must retain its identity: %v", out)
	}
	if cur := out["current"].(map[string]any); cur["id"] != nil || cur["effective"] != "persona" {
		t.Fatalf("never chosen: %v", cur)
	}

	dir := exampleLooksDir(t)
	both := lookReport(t, dir)
	reportLooks(h, both)
	if n := broadcasts(app, "personas"); n == 0 {
		t.Fatal("reported: the apps are not told the list changed")
	}

	_, out = call(t, mux, "GET", "/api/v1/looks", master, "")
	aurora := lookByID(t, out, "aurora")
	if aurora == nil || aurora["source"] != "extension" || aurora["extension"] != "example-looks" || aurora["app_icon"] != "persona" ||
		aurora["names"].(map[string]any)["en"] != "Aurora" || aurora["rev"] == "" {
		t.Fatalf("%v", aurora)
	}
	if p := lookByID(t, out, "pebble"); p == nil || p["app_icon"] != "persona" {
		t.Fatalf("%v", p)
	}
	var m struct {
		Format int    `json:"format"`
		ID     string `json:"id"`
		Avatar struct {
			SVG  string            `json:"svg"`
			Dark map[string]any    `json:"dark"`
			Size map[string]string `json:"sizes"`
		} `json:"avatar"`
		Dot struct {
			States map[string]struct{ File string } `json:"states"`
		} `json:"dot"`
		Source map[string]string `json:"source"`
	}
	raw, _ := json.Marshal(lookByID(t, out, "pebble")["manifest"])
	if err := json.Unmarshal(raw, &m); err != nil || m.Format != 1 || m.Dot.States["idle"].File != "looks/pebble/avatar-256.png" || m.Source["extension"] != "example-looks" {
		t.Fatalf("pebble without a dot gets its avatar as idle: %s", raw)
	}

	// The old list has them too: apps that know only packages show them.
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ids := []string{}
	for _, p := range getPersonas(t, srv, master) {
		ids = append(ids, p.ID)
	}
	if strings.Join(ids, ",") != "aurora,pebble" {
		t.Fatalf("%v", ids)
	}
	code, body := getPersonaFile(t, srv, master, "aurora/looks/aurora/avatar.svg")
	if code != 200 || strings.Contains(body, "script") || !strings.Contains(body, "<svg") {
		t.Fatalf("%d %s", code, body)
	}
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/personas/aurora/looks/aurora/avatar.svg?rev="+aurora["rev"].(string), nil)
	req.Header.Set("Authorization", "Bearer "+master)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "default-src 'none'") || !strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
		t.Fatalf("%v", resp.Header)
	}
	if code, _ := getPersonaFile(t, srv, master, "aurora/persona-extension.json"); code != 404 {
		t.Fatalf("the manifest: %d", code)
	}
	if code, _ := getPersonaFile(t, srv, master, "aurora/persona.json"); code != 200 {
		t.Fatalf("its made persona.json: %d", code)
	}

	// Chosen: stored, every app told.
	if code, out := call(t, mux, "PUT", "/api/v1/looks/current", master, `{"id": "nope"}`); code != 404 {
		t.Fatalf("%d %v", code, out)
	}
	code, cur := call(t, mux, "PUT", "/api/v1/looks/current", master, `{"id": "aurora"}`)
	if code != 200 || cur["id"] != "aurora" || cur["effective"] != "aurora" || cur["updated_at"] == nil {
		t.Fatalf("%d %v", code, cur)
	}
	if n := broadcasts(app, "look"); n != 1 {
		t.Fatalf("%d look broadcasts", n)
	}
	// A device moving its old choice up does not overwrite one already on the hub.
	if _, cur := call(t, mux, "PUT", "/api/v1/looks/current", master, `{"id": "persona", "if_unset": true}`); cur["id"] != "aurora" {
		t.Fatalf("%v", cur)
	}
	if n := broadcasts(app, "look"); n != 0 {
		t.Fatal("nothing changed: no broadcast")
	}

	// Switched off: the choice stays, the default shows; on again, back.
	reportLooks(h, lookReport(t, dir, "pebble"))
	_, out = call(t, mux, "GET", "/api/v1/looks", master, "")
	if c := out["current"].(map[string]any); c["id"] != "aurora" || c["effective"] != "persona" || lookByID(t, out, "aurora") != nil {
		t.Fatalf("%v", out)
	}
	reportLooks(h, both)
	_, out = call(t, mux, "GET", "/api/v1/looks", master, "")
	if c := out["current"].(map[string]any); c["effective"] != "aurora" {
		t.Fatalf("%v", c)
	}

	// Removed: back to the default for good.
	_ = broadcasts(app, "look")
	reportLooks(h, nil)
	h.looksGone(context.Background(), []string{"aurora", "pebble"}) // extensions.looks_gone
	_, out = call(t, mux, "GET", "/api/v1/looks", master, "")
	if c := out["current"].(map[string]any); c["id"] != "persona" || c["effective"] != "persona" || len(out["looks"].([]any)) != 1 {
		t.Fatalf("%v", out)
	}
	if n := broadcasts(app, "look"); n != 1 {
		t.Fatalf("%d look broadcasts after the removal", n)
	}
}
