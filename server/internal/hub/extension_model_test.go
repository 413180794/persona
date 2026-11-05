package hub

import (
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/extension"
)

// declarativePackage writes a package with only manifest (no program).
func declarativePackage(t *testing.T, manifest string) string {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, extension.ManifestFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// From a market: an extension's missing dependency installs with it (one screen, every digest), switching the
// dependency off asks first and takes the dependent along, and a pack installs and leaves with its members.
func TestExtensionDependenciesAndPacks(t *testing.T) {
	h, phone, _ := marketHub(t)
	mux := h.routes()
	base := declarativePackage(t, `{"manifest_version": 1, "id": "base", "name": "Base", "version": "1.2.0",
		"contributes": {"providers": [{"id": "base-p", "name": "Base P", "base_url": "https://base.example.com"}]}}`)
	addon := declarativePackage(t, `{"manifest_version": 1, "id": "addon", "name": "Addon", "version": "1.0.0",
		"depends": [{"id": "base", "version": ">=1.0.0"}],
		"contributes": {"providers": [{"id": "addon-p", "name": "Addon P", "base_url": "https://addon.example.com"}]}}`)
	pack := declarativePackage(t, `{"manifest_version": 1, "id": "both", "name": "Both", "version": "1.0.0",
		"extension_pack": [{"id": "base"}, {"id": "addon"}]}`)
	pkgs := map[string][]byte{"base.tar.gz": tarDir(t, base), "addon.tar.gz": tarDir(t, addon), "both.tar.gz": tarDir(t, pack)}
	gh := &fakeGitHub{release: "v1", files: map[string][]byte{}, assets: pkgs}
	srv := httptest.NewTLSServer(gh)
	defer srv.Close()
	gh.storage = srv.URL
	h.extensions.markets.client, h.extensions.markets.githubAPI = srv.Client(), srv.URL
	gh.files[extension.IndexFile] = marketIndex(t, "Example",
		marketEntry(t, base, "release:v1/base.tar.gz", pkgs["base.tar.gz"]),
		marketEntry(t, addon, "release:v1/addon.tar.gz", pkgs["addon.tar.gz"]),
		marketEntry(t, pack, "release:v1/both.tar.gz", pkgs["both.tar.gz"]))
	code, mk := call(t, mux, "POST", "/api/v1/extensions/-/markets", phone, `{"spec": "github:example/plugins"}`)
	if code != 200 || mk["extensions"] != float64(3) {
		t.Fatalf("%d %v", code, mk)
	}
	preview := func(id string) map[string]any {
		t.Helper()
		code, pv := call(t, mux, "POST", "/api/v1/extensions/-/catalog/preview", phone, fmt.Sprintf(`{"market": %q, "extension": %q}`, mk["id"], id))
		if code != 200 {
			t.Fatalf("preview %s: %d %v", id, code, pv)
		}
		return pv
	}
	bundleOf := func(pv map[string]any) (ids []string, refs string) {
		var parts []string
		for _, b := range pv["bundle"].([]any) {
			b := b.(map[string]any)
			ids = append(ids, b["manifest"].(map[string]any)["id"].(string))
			parts = append(parts, fmt.Sprintf(`{"preview_id": %q, "permissions_digest": %q}`, b["preview_id"], b["permissions_digest"]))
		}
		return ids, "[" + strings.Join(parts, ",") + "]"
	}
	install := func(pv map[string]any, bundle string) (int, map[string]any) {
		return call(t, mux, "POST", "/api/v1/extensions/-/install", phone,
			fmt.Sprintf(`{"preview_id": %q, "permissions_digest": %q, "bundle": %s}`, pv["preview_id"], pv["permissions_digest"], bundle))
	}

	pv := preview("addon")
	deps := pv["depends"].([]any)
	if len(deps) != 1 || deps[0].(map[string]any)["state"] != "missing" {
		t.Fatalf("%v", pv["depends"])
	}
	ids, refs := bundleOf(pv)
	if len(ids) != 1 || ids[0] != "base" {
		t.Fatal(ids)
	}
	if code, out := install(pv, "[]"); code != 400 || h.extensions.reg.Get("base") != nil {
		t.Fatalf("without its bundle nothing installs: %d %v", code, out)
	}
	if code, out := install(pv, refs); code != 200 || out["status"] != "running" {
		t.Fatalf("%d %v", code, out)
	} else if ds, ok := out["depends"].([]any); !ok || len(ds) != 1 || ds[0].(map[string]any)["state"] != "ok" {
		t.Fatalf("installed detail includes dependencies: %v", out)
	}
	if h.extensions.reg.Get("base") == nil || extension.FromMarket(h.extensions.reg.Get("base").Record.Origin) != mk["id"] {
		t.Fatal("the dependency came from the market")
	}

	// Switching base off: asked, then both go off.
	code, out := call(t, mux, "PUT", "/api/v1/extensions/base/-/enabled", phone, `{"enabled": false}`)
	if code != 409 || out["code"] != "has_dependents" || !strings.Contains(fmt.Sprint(out["dependents"]), "addon") {
		t.Fatalf("%d %v", code, out)
	}
	if code, _ := call(t, mux, "PUT", "/api/v1/extensions/base/-/enabled", phone, `{"enabled": false, "cascade": true}`); code != 200 {
		t.Fatal(code)
	}
	if h.extensions.reg.Get("addon").Record.Enabled || h.extensions.reg.Get("base").Record.Enabled {
		t.Fatal("both off")
	}
	_, _ = call(t, mux, "PUT", "/api/v1/extensions/base/-/enabled", phone, `{"enabled": true}`)
	_, _ = call(t, mux, "PUT", "/api/v1/extensions/addon/-/enabled", phone, `{"enabled": true}`)
	if code, out := call(t, mux, "DELETE", "/api/v1/extensions/base/-/", phone, ""); code != 409 || out["code"] != "has_dependents" {
		t.Fatalf("removing what another needs asks first: %d %v", code, out)
	}
	for _, id := range []string{"addon", "base"} {
		if code, out := call(t, mux, "DELETE", "/api/v1/extensions/"+id+"/-/", phone, ""); code != 200 {
			t.Fatalf("%s: %d %v", id, code, out)
		}
	}

	// The pack: its members in its bundle, in order, each once.
	pv = preview("both")
	ids, refs = bundleOf(pv)
	if strings.Join(ids, ",") != "base,addon" {
		t.Fatal(ids)
	}
	if code, out := install(pv, refs); code != 200 || out["kind"] != "extension_pack" {
		t.Fatalf("%d %v", code, out)
	}
	for _, id := range []string{"base", "addon", "both"} {
		if p := h.extensions.reg.Get(id); p == nil || !p.Record.Enabled {
			t.Fatalf("%s installed and on", id)
		}
	}
	outside := declarativePackage(t, `{"manifest_version":1,"id":"outside","name":"Outside","version":"1.0.0","depends":[{"id":"addon"}],"contributes":{"providers":[{"id":"outside-p","name":"Outside P","base_url":"https://outside.example.com"}]}}`)
	op, err := h.extensions.reg.Preview(t.Context(), "dir:"+outside, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.extensions.reg.Install(t.Context(), op.ID, op.Digest, "test"); err != nil {
		t.Fatal(err)
	}
	code, out = call(t, mux, "DELETE", "/api/v1/extensions/both/-/?members=1", phone, "")
	if code != 409 || out["code"] != "has_dependents" || h.extensions.reg.Get("both") == nil || h.extensions.reg.Get("base") == nil {
		t.Fatalf("pack member's external dependents require confirmation before any removal: %d %v", code, out)
	}
	code, out = call(t, mux, "DELETE", "/api/v1/extensions/both/-/?members=1&cascade=1", phone, "")
	if code != 200 || len(out["removed_members"].([]any)) != 2 {
		t.Fatalf("%d %v", code, out)
	}
	if h.extensions.reg.Get("outside").Record.Enabled {
		t.Fatal("external dependent is stopped, not removed")
	}
	if h.extensions.reg.Get("base") != nil || h.extensions.reg.Get("addon") != nil {
		t.Fatal("the members left with the pack")
	}
}
