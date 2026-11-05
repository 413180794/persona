package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/extension"
	pserve "github.com/nwverse/persona/server/internal/extension/serve"
)

// serveScopedExtensionForTest says on its HTTP side what the hub gave it: the token in its environment and the one in
// initialize, with the scopes.
func serveScopedExtensionForTest() {
	ln, err := net.Listen("unix", os.Getenv("PERSONA_EXTENSION_SOCKET"))
	if err != nil {
		os.Exit(1)
	}
	var mu sync.Mutex
	var init extension.HubInfo
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"env": os.Getenv("PERSONA_HUB_TOKEN"), "env_url": os.Getenv("PERSONA_HUB_URL"),
			"init": init.Token, "url": init.URL, "scopes": init.Scopes})
	}))
	_ = pserve.Serve(context.Background(), os.Stdin, os.Stdout, pserve.Program{ID: "scoped", Version: "1.0.0",
		Init: func(_ context.Context, ip extension.InitializeParams, _ *pserve.Hub) (map[string]any, error) {
			mu.Lock()
			init = ip.Hub
			mu.Unlock()
			return nil, nil
		}})
}

func scopedPackage(t *testing.T, hub string) string {
	dir := t.TempDir()
	self, _ := os.Executable()
	_ = os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "bin", "scoped"), []byte("#!/bin/sh\nJARVIS_TEST_PLUGIN=scoped exec "+self+" \"$@\"\n"), 0o755)
	manifest := fmt.Sprintf(`{"manifest_version": 1, "id": "scoped", "name": "Scoped", "version": "1.0.0",
		"engines": {"plugin_protocol": 1}, "runtime": {"command": {%q: "bin/scoped"}, "http": true},
		"activation_events": ["on_startup"], "contributes": {"api": [{"id": "x", "prefix": "scopedx"}]},
		"permissions": {"hub": %s}}`, runtime.GOOS+"-"+runtime.GOARCH, hub)
	_ = os.WriteFile(filepath.Join(dir, extension.ManifestFile), []byte(manifest), 0o644)
	return dir
}

// An extension granted scopes gets a token of those (the same in its environment and in initialize) that opens their
// routes and no other; it is made anew each start and stops working when the extension is switched off or removed.
func TestExtensionScopedToken(t *testing.T) {
	h, _ := extensionTestHub(t)
	mux := h.routes()
	master := h.cfg.Token

	code, pv := call(t, mux, "POST", "/api/v1/extensions/-/preview", master, `{"source": "dir:`+scopedPackage(t, `["kb.read", "extensions.call:other"]`)+`"}`)
	if code != 200 {
		t.Fatalf("preview: %d %v", code, pv)
	}
	if b, _ := json.Marshal(pv["permissions"]); !strings.Contains(string(b), "读知识库") || strings.Contains(string(b), "完整权限") {
		t.Fatalf("the install screen lists the scopes: %s", b)
	}
	if code, out := call(t, mux, "POST", "/api/v1/extensions/-/install", master,
		fmt.Sprintf(`{"preview_id": %q, "permissions_digest": %q}`, pv["preview_id"], pv["permissions_digest"])); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	given := func() map[string]any {
		var out map[string]any
		waitLong(t, "the extension up with its token", func() bool {
			hp := h.extensions.get("scoped")
			if hp == nil || hp.proc == nil || !hp.proc.Ready() {
				return false
			}
			_, out = call(t, mux, "GET", "/api/v1/extensions/scoped/whoami", master, "")
			return out["init"] != nil && out["init"] != ""
		})
		return out
	}
	got := given()
	tok, _ := got["env"].(string)
	if !strings.HasPrefix(tok, extTokenPrefix) || got["init"] != tok || got["url"] != got["env_url"] || got["url"] == "" {
		t.Fatalf("one token, in the environment and in initialize: %v", got)
	}
	if b, _ := json.Marshal(got["scopes"]); string(b) != `["kb.read","extensions.call:other"]` {
		t.Fatalf("scopes: %s", b)
	}

	if code, out := call(t, mux, "GET", "/api/v1/kb-search?q=x", tok, ""); code == http.StatusForbidden || code == http.StatusUnauthorized {
		t.Fatalf("kb.read opens the knowledge base's search: %d %v", code, out)
	}
	for _, c := range []struct{ method, path, scope string }{
		{"GET", "/api/v1/sessions", "agents.read"},
		{"PUT", "/api/v1/kb/people/x", "kb.write"},
		{"POST", "/api/v1/sessions/m/s/messages", "agents.control"},
		{"GET", "/api/v1/extensions/third/x", "extensions.call:third"},
		{"GET", "/api/v1/extensions", ""},
		{"GET", "/api/v1/ws", ""},
		{"POST", "/api/v1/auth/tokens", ""},
		{"GET", "/api/v1/plugins", ""},
		{"GET", "/api/v1/extensions/other/-/", ""},
		{"GET", "/api/v1/scopedx/y", ""},
	} {
		code, out := call(t, mux, c.method, c.path, tok, "")
		scope, _ := out["scope"].(string)
		if code != http.StatusForbidden || out["code"] != "permission_denied" || scope != c.scope {
			t.Fatalf("%s %s: %d %v, want 403 permission_denied (scope %q)", c.method, c.path, code, out, c.scope)
		}
	}
	if code, out := call(t, mux, "GET", "/api/v1/extensions/other/x", tok, ""); code == http.StatusForbidden {
		t.Fatalf("extensions.call:other opens the other extension's HTTP: %d %v", code, out)
	}

	req := httptest.NewRequest("GET", "/api/v1/kb-search?q=x", nil)
	req.RemoteAddr = "203.0.113.9:4000"
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("an extension's token from the internet: %d %s", w.Code, w.Body)
	}

	// Switched off: its token goes. On again: a new one.
	if code, out := call(t, mux, "PUT", "/api/v1/extensions/scoped/-/enabled", master, `{"enabled": false}`); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	if code, _ := call(t, mux, "GET", "/api/v1/kb-search?q=x", tok, ""); code != http.StatusUnauthorized {
		t.Fatalf("switched off, its token still opens: %d", code)
	}
	if code, out := call(t, mux, "PUT", "/api/v1/extensions/scoped/-/enabled", master, `{"enabled": true}`); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	again := given()
	tok2, _ := again["env"].(string)
	if tok2 == "" || tok2 == tok || again["init"] != tok2 {
		t.Fatalf("a new start, a new token: %v (was %s)", again, tok)
	}
	if code, _ := call(t, mux, "GET", "/api/v1/kb-search?q=x", tok, ""); code != http.StatusUnauthorized {
		t.Fatalf("the old token after a new start: %d", code)
	}

	// Uninstalled: gone too.
	if code, out := call(t, mux, "DELETE", "/api/v1/extensions/scoped/-/", master, ""); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	if code, _ := call(t, mux, "GET", "/api/v1/kb-search?q=x", tok2, ""); code != http.StatusUnauthorized {
		t.Fatalf("uninstalled, its token still opens: %d", code)
	}
}

// What the install screen says of the hub's token, and what an upgrade asking for more must be agreed to.
func TestHubScopesConsent(t *testing.T) {
	scoped := extension.Permissions{Hub: []string{"kb.read"}}
	full := extension.Permissions{HubAPI: true}
	both := extension.Permissions{HubAPI: true, Hub: []string{"kb.read"}}
	for _, c := range []struct {
		name      string
		have, ask extension.Permissions
		covers    bool
	}{
		{"same scope", scoped, scoped, true},
		{"one more scope", scoped, extension.Permissions{Hub: []string{"kb.read", "inbox.read"}}, false},
		{"fewer scopes", extension.Permissions{Hub: []string{"kb.read", "inbox.read"}}, scoped, true},
		{"the full token covers scopes", full, scoped, true},
		{"scopes do not cover the full token", scoped, full, false},
		{"hub_api with scopes is the scopes", scoped, both, true},
		{"from the full token to hub_api with scopes", full, both, true},
	} {
		if got := c.have.Covers(c.ask); got != c.covers {
			t.Errorf("%s: Covers = %v", c.name, got)
		}
	}
	if !full.FullHub() || both.FullHub() || scoped.FullHub() {
		t.Fatal("FullHub")
	}
}

// routePatterns are the patterns the hub's source registers routes with: a literal, or one made of literals and
// constants (written as the constant's name), and the assistant's relayed routes.
func routePatterns(t *testing.T) []string {
	fset := token.NewFileSet()
	files, _ := filepath.Glob("*.go")
	seen := map[string]bool{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "HandleFunc" && sel.Sel.Name != "Handle") {
				return true
			}
			switch call.Args[0].(type) {
			case *ast.BasicLit, *ast.BinaryExpr: // a variable is not a route's (the compatibility paths, a loop's)
				if p, ok := patternOf(call.Args[0]); ok {
					seen[p] = true
				}
			}
			return true
		})
	}
	for _, p := range slices.Concat(assistantwire.Routes, assistantwire.InboxRoutes) { // the manager's, relayed
		seen[p] = true
	}
	return slices.Sorted(func(yield func(string) bool) {
		for p := range seen {
			if !yield(p) {
				return
			}
		}
	})
}

func patternOf(e ast.Expr) (string, bool) {
	switch e := e.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(e.Value)
		return s, err == nil && (strings.HasPrefix(s, "/") || strings.Contains(s, " /") || strings.HasSuffix(s, " "))
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		a, ok1 := patternOf(e.X)
		b, ok2 := patternOf(e.Y)
		return a + b, ok1 && ok2
	case *ast.Ident:
		return "{" + e.Name + "}", true
	case *ast.SelectorExpr:
		if x, ok := e.X.(*ast.Ident); ok {
			return "{" + x.Name + "." + e.Sel.Name + "}", true
		}
	}
	return "", false
}

// Every route of the hub and whether an extension's token opens it, kept in testdata/route_scopes.txt: a new route is
// for full credentials until someone decides otherwise (routeScopes), and the decision shows in the file's diff.
// PERSONA_UPDATE_ROUTE_SCOPES=1 rewrites the file.
func TestRouteScopes(t *testing.T) {
	patterns := routePatterns(t)
	for p := range routeScopes {
		if !slices.Contains(patterns, p) {
			t.Errorf("routeScopes names %q, which the hub does not register", p)
		}
	}
	for p, s := range routeScopes {
		if s == extension.ScopeCallPrefix {
			continue
		}
		if _, ok := extension.ScopeTitle(s); !ok {
			t.Errorf("routeScopes: %q needs %q, which is not a scope", p, s)
		}
	}
	var b strings.Builder
	b.WriteString("# Every route of the hub (as registered) and the scope an extension's token needs for it; - is full credentials only.\n")
	b.WriteString("# Kept by TestRouteScopes (hub/extension_scopes_test.go); PERSONA_UPDATE_ROUTE_SCOPES=1 rewrites it.\n")
	for _, p := range patterns {
		s := routeScopes[p]
		if s == "" {
			s = "-"
		} else if s == extension.ScopeCallPrefix {
			s = extension.ScopeCallPrefix + "{id}"
		}
		fmt.Fprintf(&b, "%-20s %s\n", s, p)
	}
	file := filepath.Join("testdata", "route_scopes.txt")
	if os.Getenv("PERSONA_UPDATE_ROUTE_SCOPES") == "1" {
		_ = os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(file, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != b.String() {
		t.Fatalf("the hub's routes changed: decide whether an extension's token opens the new ones (routeScopes in extension_scopes.go), " +
			"then PERSONA_UPDATE_ROUTE_SCOPES=1 go test ./internal/hub -run TestRouteScopes and check the diff of testdata/route_scopes.txt")
	}
}
