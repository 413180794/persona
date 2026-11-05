package hub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		if m := l.(map[string]any); m["id"] == id {
			return m
		}
	}
	return nil
}

// The example extension's looks: listed with the built-in ones, served as persona packages (only the files they name,
// SVGs cleaned), chosen for every device, the default again when switched off or removed.
func TestExtensionLooks(t *testing.T) {
	h, _ := extensionTestHub(t)
	mux := h.routes()
	master := h.cfg.Token
	app := appClient(h)

	_, out := call(t, mux, "GET", "/api/v1/looks", master, "")
	if len(out["looks"].([]any)) != 3 || lookByID(t, out, "persona")["source"] != "builtin" || lookByID(t, out, "jarvis")["app_icon"] != "jarvis" {
		t.Fatalf("before: the built-in looks only: %v", out)
	}
	if cur := out["current"].(map[string]any); cur["id"] != nil || cur["effective"] != "persona" {
		t.Fatalf("never chosen: %v", cur)
	}

	dir := exampleLooksDir(t)
	_ = os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("not a look's"), 0o644)
	// An SVG with a script in it installs: the hub serves it cleaned.
	svg, _ := os.ReadFile(filepath.Join(dir, "looks", "aurora", "avatar.svg"))
	_ = os.WriteFile(filepath.Join(dir, "looks", "aurora", "avatar.svg"),
		[]byte(strings.Replace(string(svg), "<title>", `<script>alert(1)</script><title>`, 1)), 0o644)
	installForTest(t, h, dir)
	if n := broadcasts(app, "personas"); n == 0 {
		t.Fatal("installed: the apps are not told the list changed")
	}

	_, out = call(t, mux, "GET", "/api/v1/looks", master, "")
	aurora := lookByID(t, out, "aurora")
	if aurora == nil || aurora["source"] != "extension" || aurora["extension"] != "example-looks" || aurora["app_icon"] != "persona" ||
		aurora["names"].(map[string]any)["en"] != "Aurora" || aurora["rev"] == "" {
		t.Fatalf("%v", aurora)
	}
	if p := lookByID(t, out, "pebble"); p == nil || p["app_icon"] != "jarvis-circuit" {
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
	if code, _ := getPersonaFile(t, srv, master, "aurora/secret.txt"); code != 404 {
		t.Fatalf("a file the look does not name: %d", code)
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
	if _, cur := call(t, mux, "PUT", "/api/v1/looks/current", master, `{"id": "jarvis", "if_unset": true}`); cur["id"] != "aurora" {
		t.Fatalf("%v", cur)
	}
	if n := broadcasts(app, "look"); n != 0 {
		t.Fatal("nothing changed: no broadcast")
	}

	// Switched off: the choice stays, the default shows; on again, back.
	if code, out := call(t, mux, "PUT", "/api/v1/extensions/example-looks/-/contributions/looks/aurora", master, `{"enabled": false}`); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	_, out = call(t, mux, "GET", "/api/v1/looks", master, "")
	if c := out["current"].(map[string]any); c["id"] != "aurora" || c["effective"] != "persona" || lookByID(t, out, "aurora") != nil {
		t.Fatalf("%v", out)
	}
	_, _ = call(t, mux, "PUT", "/api/v1/extensions/example-looks/-/contributions/looks/aurora", master, `{"enabled": true}`)
	_, out = call(t, mux, "GET", "/api/v1/looks", master, "")
	if c := out["current"].(map[string]any); c["effective"] != "aurora" {
		t.Fatalf("%v", c)
	}

	// Removed: back to the default for good.
	_ = broadcasts(app, "look")
	if code, out := call(t, mux, "DELETE", "/api/v1/extensions/example-looks/-/", master, ""); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	_, out = call(t, mux, "GET", "/api/v1/looks", master, "")
	if c := out["current"].(map[string]any); c["id"] != "persona" || c["effective"] != "persona" || len(out["looks"].([]any)) != 3 {
		t.Fatalf("%v", out)
	}
	if n := broadcasts(app, "look"); n != 1 {
		t.Fatalf("%d look broadcasts after the removal", n)
	}
}

// An extension's look may not take a built-in look's id, nor one built into personad; one with a bad picture does not
// install.
func TestExtensionLooksRefused(t *testing.T) {
	h, _ := extensionTestHub(t)
	mux := h.routes()
	for name, edit := range map[string]func(dir string){
		"a built-in id": func(dir string) {
			b, _ := os.ReadFile(filepath.Join(dir, "persona-extension.json"))
			_ = os.WriteFile(filepath.Join(dir, "persona-extension.json"), []byte(strings.Replace(string(b), `"id": "pebble"`, `"id": "jarvis"`, 1)), 0o644)
		},
		"a picture that is not a PNG": func(dir string) {
			_ = os.WriteFile(filepath.Join(dir, "looks", "pebble", "avatar-256.png"), []byte("gif"), 0o644)
		},
		"a missing picture": func(dir string) { _ = os.Remove(filepath.Join(dir, "looks", "aurora", "working.png")) },
	} {
		dir := exampleLooksDir(t)
		edit(dir)
		code, out := call(t, mux, "POST", "/api/v1/extensions/-/preview", h.cfg.Token, `{"source": "dir:`+dir+`"}`)
		if code == 200 {
			code, out = call(t, mux, "POST", "/api/v1/extensions/-/install", h.cfg.Token,
				`{"preview_id": "`+out["preview_id"].(string)+`", "permissions_digest": "`+out["permissions_digest"].(string)+`"}`)
		}
		if code == 200 {
			t.Errorf("%s: installed (%v)", name, out)
		}
	}
}
