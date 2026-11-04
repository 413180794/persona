package extension

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestVersionRange(t *testing.T) {
	r, err := ParseVersionRange(">=1.2.0 <2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	for v, want := range map[string]bool{"1.2.0": true, "1.9.9": true, "2.0.0": false, "1.1.9": false, "1.2.0-rc.1": false, "nope": false} {
		if r.Allows(v) != want {
			t.Errorf("%s: %v", v, !want)
		}
	}
	for _, bad := range []string{"", "^1.2", "<=1.0.0", ">=1"} {
		if _, err := ParseVersionRange(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
	if !(Dependency{ID: "x"}).Allows("0.0.1") {
		t.Fatal("no range: any")
	}
}

func TestModelManifests(t *testing.T) {
	ok := []func(m map[string]any){
		func(m map[string]any) { m["depends"] = []any{map[string]any{"id": "other", "version": ">=1.0.0"}} },
		func(m map[string]any) {
			m["exports"] = map[string]any{"extension_points": []any{map[string]any{"id": "sources", "version": 1,
				"schema": map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}, "url": map[string]any{"type": "string"}}}}}}
		},
		func(m map[string]any) {
			m["depends"] = []any{map[string]any{"id": "other"}}
			m["contributes"].(map[string]any)["other.sources"] = []any{map[string]any{"id": "a"}}
		},
	}
	for i, change := range ok {
		if _, err := Parse(with(t, change)); err != nil {
			t.Errorf("ok %d: %v", i, err)
		}
	}
	bad := map[string]func(m map[string]any){
		"depends on itself": func(m map[string]any) { m["depends"] = []any{map[string]any{"id": "example-notes"}} },
		"a bad range":       func(m map[string]any) { m["depends"] = []any{map[string]any{"id": "other", "version": "^1"}} },
		"not in depends": func(m map[string]any) {
			m["contributes"].(map[string]any)["other.sources"] = []any{map[string]any{"id": "a"}}
		},
		"a dotted name": func(m map[string]any) { m["contributes"].(map[string]any)["a.b.c"] = []any{map[string]any{"id": "a"}} },
		"an exported schema without id": func(m map[string]any) {
			m["exports"] = map[string]any{"extension_points": []any{map[string]any{"id": "s", "version": 1,
				"schema": map[string]any{"type": "object", "properties": map[string]any{"url": map[string]any{"type": "string"}}}}}}
		},
		"a pack with a runtime": func(m map[string]any) { m["extension_pack"] = []any{map[string]any{"id": "other"}} },
	}
	for name, change := range bad {
		if _, err := Parse(with(t, change)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
	pack := `{"manifest_version": 1, "id": "a-pack", "name": "A pack", "version": "1.0.0",
		"extension_pack": [{"id": "one"}, {"id": "two", "version": ">=1.0.0", "market": "github:example/persona-extensions"}]}`
	m, err := Parse([]byte(pack))
	if err != nil {
		t.Fatal(err)
	}
	if !m.IsPack() || m.Kind() != "extension_pack" {
		t.Fatal(m.Kind())
	}
	if _, err := Parse([]byte(strings.Replace(pack, `{"id": "one"}, `, `{"id": "one"}, {"id": "one"}, `, 1))); err == nil {
		t.Fatal("a member twice")
	}
	if _, err := Parse([]byte(`{"manifest_version": 1, "id": "empty", "name": "x", "version": "1.0.0"}`)); err == nil {
		t.Fatal("neither contributes nor a pack")
	}
}

// installDir installs the package written at dir by change (the example manifest, no program) on r.
func installDir(t *testing.T, r *Registry, change func(m map[string]any)) *Extension {
	t.Helper()
	dir := t.TempDir()
	m := with(t, func(m map[string]any) {
		for _, k := range []string{"secrets", "configuration", "runtime", "engines", "activation_events", "permissions"} {
			delete(m, k)
		}
		provider := map[string]any{"name": "P", "base_url": "https://p.example.com"}
		m["contributes"] = map[string]any{"providers": []any{provider}}
		change(m)
		provider["id"] = m["id"].(string) + "-p"
	})
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), m, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	pv, err := r.Preview(ctx, "dir:"+dir, "")
	if err != nil {
		t.Fatal(err)
	}
	p, err := r.Install(ctx, pv.ID, pv.Digest, "cli")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDependenciesAndExports(t *testing.T) {
	ctx := context.Background()
	r, _, _ := newTestRegistry(t)
	user := installDir(t, r, func(m map[string]any) {
		m["id"] = "search-ui"
		m["depends"] = []any{map[string]any{"id": "search", "version": ">=2.0.0"}}
		m["contributes"].(map[string]any)["search.sources"] = []any{map[string]any{"id": "notes", "title": "笔记"}}
	})
	if got := r.Missing(ctx, user); len(got) != 1 || !strings.Contains(got[0], "先装扩展「search」") {
		t.Fatal(got)
	}
	exporter := func(version string, schemaProps map[string]any) func(m map[string]any) {
		return func(m map[string]any) {
			m["id"], m["version"] = "search", version
			m["exports"] = map[string]any{"extension_points": []any{map[string]any{"id": "sources", "version": 1,
				"schema": map[string]any{"type": "object", "properties": schemaProps}}}}
		}
	}
	installDir(t, r, exporter("1.0.0", map[string]any{"id": map[string]any{"type": "string"}, "title": map[string]any{"type": "string"}}))
	if got := r.Missing(ctx, r.Get("search-ui")); len(got) != 1 || !strings.Contains(got[0], "要 >=2.0.0（现在是 1.0.0）") {
		t.Fatal(got)
	}
	if deps := r.Dependencies(r.Get("search-ui").Full); len(deps) != 1 || deps[0].State != DepOld {
		t.Fatal(deps)
	}
	if err := r.Uninstall(ctx, "search", false, "cli"); err != nil {
		t.Fatal(err)
	}
	installDir(t, r, exporter("2.0.0", map[string]any{"id": map[string]any{"type": "string"}, "title": map[string]any{"type": "string"}}))
	if got := r.Missing(ctx, r.Get("search-ui")); len(got) != 0 {
		t.Fatal("met:", got)
	}
	got := r.ContributionsTo("search")
	if len(got["sources"]) != 1 || got["sources"][0].Extension != "search-ui" {
		t.Fatal(got)
	}
	var item map[string]string
	_ = json.Unmarshal(got["sources"][0].Item, &item)
	if item["title"] != "笔记" {
		t.Fatal(item)
	}
	if ds := r.Dependents("search"); len(ds) != 1 || ds[0].Record.ID != "search-ui" {
		t.Fatal(ds)
	}
	// Off: still a dependent, no longer a contribution; the exporter switched off makes it wait.
	if err := r.SetEnabled(ctx, "search", false, "cli"); err != nil {
		t.Fatal(err)
	}
	if got := r.Missing(ctx, r.Get("search-ui")); len(got) != 1 || !strings.Contains(got[0], "先启用") {
		t.Fatal(got)
	}
	_ = r.SetEnabled(ctx, "search", true, "cli")
	if err := r.SetContribution(ctx, "search-ui", "search.sources", "notes", false, "cli"); err != nil {
		t.Fatal(err)
	}
	if got := r.ContributionsTo("search"); len(got["sources"]) != 0 {
		t.Fatal("switched off:", got)
	}
	// A contribution its exporter's schema does not take.
	other := installDir(t, r, func(m map[string]any) {
		m["id"] = "bad-source"
		m["depends"] = []any{map[string]any{"id": "search"}}
		m["contributes"].(map[string]any)["search.sources"] = []any{map[string]any{"id": "x", "color": "red"}}
	})
	if got := r.Missing(ctx, other); len(got) != 1 || !strings.Contains(got[0], "sources") {
		t.Fatal(got)
	}
	// An exported point it does not have.
	other = installDir(t, r, func(m map[string]any) {
		m["id"] = "lost"
		m["depends"] = []any{map[string]any{"id": "search"}}
		m["contributes"].(map[string]any)["search.nope"] = []any{map[string]any{"id": "x"}}
	})
	if got := r.Missing(ctx, other); len(got) != 1 || !strings.Contains(got[0], "没有扩展点 nope") {
		t.Fatal(got)
	}
	if !slices.Equal(other.Full.ContributesTo(), []string{"search"}) {
		t.Fatal(other.Full.ContributesTo())
	}
	if lines := Explain(other.Full); !slices.ContainsFunc(lines, func(l PermissionLine) bool { return l.Key == "contributes_to" }) {
		t.Fatal(lines)
	}
}
