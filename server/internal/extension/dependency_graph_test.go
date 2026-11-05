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

func dependencyPackage(t *testing.T, id, version string, deps []Dependency, edit func(map[string]any)) string {
	t.Helper()
	m := map[string]any{"manifest_version": 1, "id": id, "name": id, "version": version,
		"contributes": map[string]any{"providers": []any{map[string]any{"id": id + "-provider", "name": id, "base_url": "https://provider.example.com"}}}}
	if len(deps) > 0 {
		m["depends"] = deps
	}
	if edit != nil {
		edit(m)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = os.WriteFile(filepath.Join(dir, ManifestFile), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func installDependency(t *testing.T, r *Registry, id string, deps ...Dependency) *Extension {
	t.Helper()
	pv, err := r.Preview(t.Context(), dependencyPackage(t, id, "1.0.0", deps, nil), "")
	if err != nil {
		t.Fatal(err)
	}
	p, err := r.Install(t.Context(), pv.ID, pv.Digest, "test")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDependencyGraphRejectsNewCycles(t *testing.T) {
	for _, ids := range [][]string{{"a", "b"}, {"a", "b", "c"}, {"a", "b", "c", "d", "e"}} {
		t.Run(strings.Join(ids, "-"), func(t *testing.T) {
			r, store, _ := newTestRegistry(t)
			for i, id := range ids[:len(ids)-1] {
				installDependency(t, r, id, Dependency{ID: ids[i+1]})
			}
			if err := r.SetEnabled(t.Context(), ids[0], false, "test"); err != nil {
				t.Fatal(err)
			}
			last := ids[len(ids)-1]
			pv, err := r.Preview(t.Context(), dependencyPackage(t, last, "1.0.0", []Dependency{{ID: ids[0]}}, nil), "")
			if err == nil {
				r.Discard(pv.ID)
				t.Error("preview accepted a cross-extension cycle, including a disabled node")
			}
			if r.Get(last) != nil || len(store.rows) != len(ids)-1 {
				t.Fatal("cycle check changed installed records")
			}
		})
	}
}

func TestDependencyGraphRejectsUpgradeCycle(t *testing.T) {
	r, _, _ := newTestRegistry(t)
	installDependency(t, r, "b")
	installDependency(t, r, "a", Dependency{ID: "b"})
	pv, err := r.Preview(t.Context(), dependencyPackage(t, "b", "2.0.0", []Dependency{{ID: "a"}}, nil), "b")
	if err == nil {
		r.Discard(pv.ID)
		t.Error("candidate upgrade introduced a cycle")
	}
	if r.Get("b").Record.Version != "1.0.0" {
		t.Fatal("preview upgraded an existing extension")
	}
}

func TestDependencyGraphRechecksAtCommit(t *testing.T) {
	t.Run("install", func(t *testing.T) {
		r, store, _ := newTestRegistry(t)
		pv, err := r.Preview(t.Context(), dependencyPackage(t, "a", "1.0.0", []Dependency{{ID: "b"}}, nil), "")
		if err != nil {
			t.Fatal(err)
		}
		installDependency(t, r, "b", Dependency{ID: "a"})
		if _, err = r.Install(t.Context(), pv.ID, pv.Digest, "test"); err == nil {
			t.Error("install did not recheck the graph changed since preview")
		}
		if r.Get("a") != nil || len(store.rows) != 1 {
			t.Error("failed graph commit changed records")
		}
	})
	t.Run("upgrade", func(t *testing.T) {
		r, _, _ := newTestRegistry(t)
		installDependency(t, r, "a")
		installDependency(t, r, "b")
		pv, err := r.Preview(t.Context(), dependencyPackage(t, "b", "2.0.0", []Dependency{{ID: "a"}}, nil), "b")
		if err != nil {
			t.Fatal(err)
		}
		ap, err := r.Preview(t.Context(), dependencyPackage(t, "a", "2.0.0", []Dependency{{ID: "b"}}, nil), "a")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = r.Upgrade(t.Context(), "a", ap.ID, ap.Digest, "test"); err != nil {
			t.Fatal(err)
		}
		if _, err = r.Upgrade(t.Context(), "b", pv.ID, pv.Digest, "test"); err == nil {
			t.Error("upgrade did not recheck the candidate graph")
		}
		if r.Get("b").Record.Version != "1.0.0" {
			t.Error("rejected upgrade replaced the old version")
		}
	})
}

func TestDependencyGraphLoadedCycleIsKeptAndRepairable(t *testing.T) {
	r, store, _ := newTestRegistry(t)
	installDependency(t, r, "a", Dependency{ID: "b"})
	dir := dependencyPackage(t, "b", "1.0.0", []Dependency{{ID: "a"}}, nil)
	raw, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	to := r.installedDir("b", "1.0.0")
	if err = os.MkdirAll(to, 0o755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(to, ManifestFile), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	row := Record{ID: "b", Version: "1.0.0", Source: SourceInstalled, Manifest: raw, Enabled: true, Config: json.RawMessage(`{}`)}
	if err = store.PutExtension(t.Context(), row); err != nil {
		t.Fatal(err)
	}
	if err = r.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		missing := r.Missing(t.Context(), r.Get(id))
		if len(missing) == 0 || !strings.Contains(strings.Join(missing, " "), "环") {
			t.Errorf("loaded cycle %s considered ready: %v", id, missing)
		}
	}
	if len(store.rows) != 2 {
		t.Fatal("loading removed cyclic records")
	}
	installDependency(t, r, "unrelated")
	pv, err := r.Preview(t.Context(), dependencyPackage(t, "b", "2.0.0", nil, nil), "b")
	if err != nil {
		t.Fatal("repair refused:", err)
	}
	if _, err = r.Upgrade(t.Context(), "b", pv.ID, pv.Digest, "test"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if got := r.Missing(t.Context(), r.Get(id)); len(got) != 0 {
			t.Errorf("repair left %s blocked: %v", id, got)
		}
	}
}

func TestDependencyGraphReadinessAndExportSnapshots(t *testing.T) {
	ctx := context.Background()
	r, _, _ := newTestRegistry(t)
	ex := installDir(t, r, func(m map[string]any) {
		m["id"] = "search"
		m["exports"] = map[string]any{"extension_points": []any{map[string]any{"id": "sources", "version": 1}}}
	})
	installDependency(t, r, "middle", Dependency{ID: "base", Version: ">=2.0.0"}, Dependency{ID: "branch"})
	user := installDir(t, r, func(m map[string]any) {
		m["id"] = "reader"
		m["depends"] = []Dependency{{ID: "middle"}, {ID: ex.Record.ID}}
		m["contributes"].(map[string]any)["search.sources"] = []any{map[string]any{"id": "notes"}}
	})
	assertBlocked := func(reason string) {
		t.Helper()
		missing := r.Missing(ctx, r.Get(user.Record.ID))
		if len(missing) == 0 {
			t.Errorf("%s: ancestor considered ready", reason)
		}
		deps := r.Dependencies(user.Full)
		if len(deps) != 2 || deps[0].State != "not_ready" {
			t.Errorf("%s: direct dependency not marked not_ready: %v", reason, deps)
		}
		if got := r.ContributionsTo("search"); len(got["sources"]) != 0 {
			t.Errorf("%s: unready contribution exported: %v", reason, got)
		}
	}
	assertBlocked("missing base and branch")
	installDependency(t, r, "base")
	assertBlocked("base too old")
	installDependency(t, r, "branch")
	pkg := dependencyPackage(t, "base", "2.0.0", nil, func(m map[string]any) {
		m["secrets"] = []any{map[string]any{"name": "token", "title": "Test token", "required": true}}
		m["configuration"] = map[string]any{"type": "object", "properties": map[string]any{"endpoint": map[string]any{"type": "string"}}, "required": []string{"endpoint"}}
	})
	pv, err := r.Preview(ctx, pkg, "base")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Upgrade(ctx, "base", pv.ID, pv.Digest, "test"); err != nil {
		t.Fatal(err)
	}
	assertBlocked("base needs configuration and a secret")
	if err = r.SetConfig(ctx, "base", json.RawMessage(`{"endpoint":"https://api.example.com"}`), "test"); err != nil {
		t.Fatal(err)
	}
	assertBlocked("base still needs its secret")
	if err = r.SetSecret(ctx, "base", "token", "synthetic-test-value", "test"); err != nil {
		t.Fatal(err)
	}
	if got := r.Missing(ctx, r.Get("reader")); len(got) != 0 {
		t.Fatal("complete graph did not recover:", got)
	}
	snapshot := r.ContributionsTo("search")
	if len(snapshot["sources"]) != 1 {
		t.Fatal("ready contribution missing:", snapshot)
	}
	if err = r.SetEnabled(ctx, "base", false, "test"); err != nil {
		t.Fatal(err)
	}
	assertBlocked("base disabled")
	if len(snapshot["sources"]) != 1 || !json.Valid(snapshot["sources"][0].Item) {
		t.Fatal("old exporter snapshot mutated")
	}
	if err = r.SetEnabled(ctx, "base", true, "test"); err != nil {
		t.Fatal(err)
	}
	if err = r.DeleteSecret(ctx, "base", "token", "test"); err != nil {
		t.Fatal(err)
	}
	assertBlocked("secret removed")
	if err = r.SetSecret(ctx, "base", "token", "synthetic-test-value", "test"); err != nil {
		t.Fatal(err)
	}
	if got := r.ContributionsTo("search"); len(got["sources"]) != 1 {
		t.Fatal("export did not recover:", got)
	}
	if err = r.SetEnabled(ctx, "reader", false, "test"); err != nil {
		t.Fatal(err)
	}
	if got := r.ContributionsTo("search"); len(got["sources"]) != 0 {
		t.Fatal("disabled contributor leaked:", got)
	}
	if r.Get("reader").Record.Enabled {
		t.Fatal("dependency recovery implicitly enabled the contributor")
	}
	if !slices.Equal(user.Full.ContributesTo(), []string{"search"}) {
		t.Fatal("full manifest changed")
	}
}
