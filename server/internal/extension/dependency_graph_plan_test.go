package extension

import (
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestDependencyGraphChecksWholeCandidatePlan(t *testing.T) {
	r, store, _ := newTestRegistry(t)
	var candidates []*Manifest
	for _, pair := range [][2]string{{"a", "b"}, {"b", "c"}, {"c", "a"}} {
		pv, err := r.Preview(t.Context(), dependencyPackage(t, pair[0], "1.0.0", []Dependency{{ID: pair[1]}}, nil), "")
		if err != nil {
			t.Fatal("individual preview should permit an absent dependency:", err)
		}
		candidates = append(candidates, pv.Manifest)
	}
	if err := r.ValidateDependencyGraph(candidates...); err == nil || !strings.Contains(err.Error(), "环") {
		t.Errorf("joint candidate plan not rejected: %v", err)
	}
	if len(store.rows) != 0 || len(r.List()) != 0 {
		t.Fatal("validating a plan wrote records")
	}
	last := *candidates[2]
	last.Depends = nil
	if err := r.ValidateDependencyGraph(candidates[0], candidates[1], &last); err != nil {
		t.Fatal("acyclic plan refused:", err)
	}
}

func TestDependencyGraphConcurrentCommitsCannotCloseCycle(t *testing.T) {
	r, store, _ := newTestRegistry(t)
	var previews []*Preview
	for _, pair := range [][2]string{{"a", "b"}, {"b", "a"}} {
		pv, err := r.Preview(t.Context(), dependencyPackage(t, pair[0], "1.0.0", []Dependency{{ID: pair[1]}}, nil), "")
		if err != nil {
			t.Fatal(err)
		}
		previews = append(previews, pv)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, pv := range previews {
		wg.Add(1)
		go func(i int, pv *Preview) {
			defer wg.Done()
			<-start
			_, errs[i] = r.Install(t.Context(), pv.ID, pv.Digest, "test")
		}(i, pv)
	}
	close(start)
	wg.Wait()
	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		} else if !strings.Contains(err.Error(), "环") {
			t.Fatal(err)
		}
	}
	if succeeded != 1 || len(store.rows) != 1 || len(r.List()) != 1 {
		t.Fatalf("cycle published concurrently: successes=%d rows=%d", succeeded, len(store.rows))
	}
}

func TestDependencyGraphRollbackCannotIntroduceCycle(t *testing.T) {
	r, _, _ := newTestRegistry(t)
	installDependency(t, r, "a", Dependency{ID: "b"})
	installDependency(t, r, "b")
	for _, candidate := range []struct {
		id   string
		deps []Dependency
	}{{"a", nil}, {"b", []Dependency{{ID: "a"}}}} {
		pv, err := r.Preview(t.Context(), dependencyPackage(t, candidate.id, "2.0.0", candidate.deps, nil), candidate.id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = r.Upgrade(t.Context(), candidate.id, pv.ID, pv.Digest, "test"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Rollback(t.Context(), "a", "test"); err == nil {
		t.Error("rollback restored a manifest that now closes a cycle")
	}
	if r.Get("a").Record.Version != "2.0.0" {
		t.Error("failed rollback changed the current version")
	}
}

func TestDependencyGraphUnreadableSecretIsNotReady(t *testing.T) {
	r, store, _ := newTestRegistry(t)
	dir := dependencyPackage(t, "base", "1.0.0", nil, func(m map[string]any) {
		m["secrets"] = []any{map[string]any{"name": "token", "title": "Test key", "required": true}}
	})
	pv, err := r.Preview(t.Context(), dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Install(t.Context(), pv.ID, pv.Digest, "test"); err != nil {
		t.Fatal(err)
	}
	installDependency(t, r, "reader", Dependency{ID: "base"})
	if err = store.PutExtensionSecret(t.Context(), "base", "token", "deliberately-invalid-ciphertext"); err != nil {
		t.Fatal(err)
	}
	if got := r.Missing(t.Context(), r.Get("base")); len(got) == 0 {
		t.Error("ciphertext presence was treated as a usable secret")
	}
	if got := r.Dependencies(r.Get("reader").Full); len(got) != 1 || got[0].State != DepNotReady {
		t.Errorf("unreadable secret dependency: %v", got)
	}
	if err = store.PutExtensionSecret(t.Context(), "base", "token", "sealed:"); err != nil {
		t.Fatal(err)
	}
	if got := r.Missing(t.Context(), r.Get("base")); len(got) == 0 {
		t.Error("empty required secret considered ready")
	}
	if err = r.SetSecret(t.Context(), "base", "token", "synthetic-test-value", "test"); err != nil {
		t.Fatal(err)
	}
	if got := r.Missing(t.Context(), r.Get("reader")); len(got) != 0 {
		t.Fatal("corrected secret did not recover:", got)
	}
}

func TestDependencyGraphOrdersCompleteAffectedGraph(t *testing.T) {
	r, _, _ := newTestRegistry(t)
	installDependency(t, r, "ancestor", Dependency{ID: "middle"})
	installDependency(t, r, "sibling", Dependency{ID: "middle"})
	installDependency(t, r, "middle", Dependency{ID: "base"})
	installDependency(t, r, "unrelated")
	installDependency(t, r, "base")
	ids := func(entries []*Extension) []string {
		var out []string
		for _, p := range entries {
			out = append(out, p.Record.ID)
		}
		return out
	}
	want := []string{"base", "middle", "ancestor", "sibling"}
	if got := ids(r.DependencyOrder("base")); !slices.Equal(got, want) {
		t.Fatalf("affected order %v, want %v", got, want)
	}
	if err := r.Uninstall(t.Context(), "base", true, "test"); err != nil {
		t.Fatal(err)
	}
	if got := ids(r.DependencyOrder("base")); !slices.Equal(got, want[1:]) {
		t.Fatalf("removal order %v", got)
	}
}

func TestDependencyGraphUnreadyExporterHasNoSnapshot(t *testing.T) {
	r, _, _ := newTestRegistry(t)
	exporter := installDir(t, r, func(m map[string]any) {
		m["id"] = "search"
		m["exports"] = map[string]any{"extension_points": []any{map[string]any{"id": "sources", "version": 1}}}
	})
	installDir(t, r, func(m map[string]any) {
		m["id"] = "reader"
		m["depends"] = []Dependency{{ID: "search"}}
		m["contributes"].(map[string]any)["search.sources"] = []any{map[string]any{"id": "one"}}
	})
	before := r.ContributionsTo("search")
	if len(before["sources"]) != 1 {
		t.Fatal(before)
	}
	r.mu.Lock()
	exporter.Problem = "synthetic unavailable package"
	r.mu.Unlock()
	if got := r.ContributionsTo("search"); len(got) != 0 {
		t.Fatal("unready exporter received contributions:", got)
	}
	if len(before["sources"]) != 1 {
		t.Fatal("previous snapshot was mutated")
	}
	r.mu.Lock()
	exporter.Problem = ""
	r.mu.Unlock()
	if got := r.ContributionsTo("search"); len(got["sources"]) != 1 {
		t.Fatal("exporter did not recover:", got)
	}
}
