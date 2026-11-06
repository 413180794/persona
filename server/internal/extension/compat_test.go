package extension

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The names before #1844 still work (docs/extensions.md "Naming and compatibility"): the manifest's old file name and
// engines.plugin_protocol, initialize's "plugin", the index's old file name and "plugins".

func TestEnginesBothNames(t *testing.T) {
	for _, engines := range []string{`{"extension_protocol": 1}`, `{"plugin_protocol": 1}`, `{"extension_protocol": 1, "plugin_protocol": 1}`} {
		m, err := Parse([]byte(strings.Replace(exampleManifest, `"plugin_protocol": 1`, strings.Trim(engines, "{}"), 1)))
		if err != nil || m.Engines.ExtensionProtocol != 1 {
			t.Fatalf("%s: %v", engines, err)
		}
		// Written out under the old name: a hub rolled back to a build before #1844 reads the manifests it stored.
		if b, _ := json.Marshal(m); !strings.Contains(string(b), `"plugin_protocol":1`) || strings.Contains(string(b), "extension_protocol") {
			t.Fatalf("%s", b)
		}
	}
	if _, err := Parse([]byte(strings.Replace(exampleManifest, `"plugin_protocol": 1`, `"extension_protocol": 1, "plugin_protocol": 2`, 1))); err == nil {
		t.Fatal("two protocols that differ")
	}
	if _, err := Parse([]byte(strings.Replace(exampleManifest, `"plugin_protocol": 1`, `"plugin_protocol": 1, "extension_protocl": 1`, 1))); err == nil {
		t.Fatal("engines is read strictly")
	}
}

func TestManifestFileNames(t *testing.T) {
	dir := t.TempDir()
	if ManifestPath(dir) != filepath.Join(dir, ManifestFile) || hasManifest(dir) {
		t.Fatal("none: the new name")
	}
	_ = os.WriteFile(filepath.Join(dir, LegacyManifestFile), []byte("{}"), 0o644)
	if ManifestPath(dir) != filepath.Join(dir, LegacyManifestFile) || !hasManifest(dir) {
		t.Fatal("only the old name: that one")
	}
	_ = os.WriteFile(filepath.Join(dir, ManifestFile), []byte("{}"), 0o644)
	if ManifestPath(dir) != filepath.Join(dir, ManifestFile) {
		t.Fatal("both: the new name")
	}
	if root, err := packageRoot(filepath.Dir(dir)); err != nil || root != dir {
		t.Fatalf("the only top-level directory: %q %v", root, err)
	}
}

func TestInitializeBothKeys(t *testing.T) {
	b, _ := json.Marshal(InitializeParams{Protocol: 1, Extension: ExtensionInfo{ID: "x", Version: "1.0.0"}})
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(b, &raw)
	if string(raw["extension"]) != string(raw["plugin"]) || !strings.Contains(string(raw["plugin"]), `"id":"x"`) {
		t.Fatalf("%s", b)
	}
	for _, in := range []string{`{"protocol": 1, "plugin": {"id": "x", "version": "1.0.0"}}`, `{"protocol": 1, "extension": {"id": "x", "version": "1.0.0"}}`} {
		var p InitializeParams
		if err := json.Unmarshal([]byte(in), &p); err != nil || p.Extension.ID != "x" || p.Protocol != 1 {
			t.Fatalf("%s: %+v %v", in, p, err)
		}
	}
}

func TestIndexBothNames(t *testing.T) {
	var m map[string]any
	_ = json.Unmarshal([]byte(exampleManifest), &m)
	entries := []any{map[string]any{"manifest": m, "packages": []any{map[string]any{"url": "x.tar.gz", "sha256": strings.Repeat("a", 64)}}}}
	index := func(key string) []byte {
		b, _ := json.Marshal(map[string]any{"market_version": 1, "name": "Example", key: entries})
		return b
	}
	for _, key := range []string{"extensions", "plugins"} {
		ix, _, err := ParseIndex(index(key))
		if err != nil || len(ix.Extensions) != 1 {
			t.Fatalf("%s: %v", key, err)
		}
	}

	// A site or a repository that has only the old file is read from it.
	for _, files := range []map[string]bool{{IndexFile: true}, {LegacyIndexFile: true}} {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			name := filepath.Base(r.URL.Path)
			if strings.HasPrefix(r.URL.Path, "/repos/") {
				name = strings.TrimPrefix(r.URL.Path, "/repos/example/market/contents/")
			}
			if !files[name] {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(index("extensions"))
		}))
		for _, spec := range []string{srv.URL + "/market/", "github:example/market"} {
			s, err := ParseMarket(spec)
			if err != nil {
				t.Fatal(err)
			}
			mk := &Market{Spec: s, Client: srv.Client(), GitHubAPI: srv.URL}
			if ix, _, err := mk.Fetch(context.Background()); err != nil || len(ix.Extensions) != 1 {
				t.Fatalf("%v %s: %v", files, spec, err)
			}
		}
		srv.Close()
	}
}
