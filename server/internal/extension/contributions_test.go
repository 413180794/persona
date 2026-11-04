package extension

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestContributionSwitches(t *testing.T) {
	ctx := context.Background()
	r, store, changes := newTestRegistry(t)
	pkg := t.TempDir()
	writePackage(t, pkg, func(m map[string]any) {
		c := m["contributes"].(map[string]any)
		c["providers"] = []any{
			map[string]any{"id": "notes-api", "name": "Notes API", "base_url": "https://api.notes.example.com"},
			map[string]any{"id": "notes-mini", "name": "Notes Mini", "base_url": "https://mini.notes.example.com"},
		}
		c["ui"] = []any{map[string]any{"id": "usage", "title": "用量", "placement": "extension_page"}}
	})
	pv, err := r.Preview(ctx, "dir:"+pkg, "")
	if err != nil {
		t.Fatal(err)
	}
	p, err := r.Install(ctx, pv.ID, pv.Digest, "cli")
	if err != nil {
		t.Fatal(err)
	}
	*changes = nil

	if err := r.SetContribution(ctx, "example-notes", "providers", "nope", false, "device:x"); err == nil {
		t.Fatal("no such contribution")
	}
	if err := r.SetContribution(ctx, "example-notes", "connectors", "examplenotes", false, "device:x"); !errors.Is(err, ErrSwitchedElsewhere) {
		t.Fatal("a connector's switch is the connector's:", err)
	}
	if err := r.SetContribution(ctx, "example-notes", "providers", "notes-mini", false, "device:x"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetContribution(ctx, "example-notes", "ui", "usage", false, "device:x"); err != nil {
		t.Fatal(err)
	}
	p = r.Get("example-notes")
	if ids := p.Manifest.Items("providers"); !slices.Equal(ids, []string{"notes-api"}) {
		t.Fatal("what takes effect:", ids)
	}
	if p.Manifest.Items("ui") != nil || p.Manifest.Contributes["ui"] != nil {
		t.Fatal("the only ui is off: the point is gone from what takes effect")
	}
	if ids := p.Manifest.Items("connectors"); !slices.Equal(ids, []string{"examplenotes"}) {
		t.Fatal("the connector stays:", ids)
	}
	if ids := p.Full.Items("providers"); len(ids) != 2 || p.Full.Items("ui") == nil {
		t.Fatal("the full manifest keeps all:", ids)
	}
	if got := store.rows["example-notes"].DisabledContributions; !slices.Equal(got, []string{"providers/notes-mini", "ui/usage"}) {
		t.Fatal("kept:", got)
	}
	if len(*changes) != 2 || (*changes)[0].Kind != "contributions" {
		t.Fatalf("%+v", *changes)
	}
	// The same again changes nothing.
	if err := r.SetContribution(ctx, "example-notes", "ui", "usage", false, "device:x"); err != nil || len(*changes) != 2 {
		t.Fatal(err, *changes)
	}

	// Read back from the store: the switches hold.
	r2 := NewRegistry(r.dir, store, rot13{}, 202611100000)
	if err := r2.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if ids := r2.Get("example-notes").Manifest.Items("providers"); !slices.Equal(ids, []string{"notes-api"}) {
		t.Fatal("after a restart:", ids)
	}

	// Another extension cannot take the id of a contribution switched off.
	other := t.TempDir()
	writePackage(t, other, func(m map[string]any) {
		m["id"] = "other"
		m["contributes"] = map[string]any{"ui": []any{map[string]any{"id": "usage", "title": "x", "placement": "extension_page"}}}
	})
	if _, err := r.Preview(ctx, "dir:"+other, ""); err == nil {
		t.Fatal("ui usage is still example-notes's")
	}

	// Back on.
	if err := r.SetContribution(ctx, "example-notes", "ui", "usage", true, "device:x"); err != nil {
		t.Fatal(err)
	}
	if r.Get("example-notes").Manifest.Items("ui") == nil {
		t.Fatal("on again")
	}
	if title := ContributionTitle(r.Get("example-notes").Full, "providers", "notes-mini"); title != "Notes Mini" {
		t.Fatal(title)
	}
}

func TestWithoutKeepsTheManifest(t *testing.T) {
	full, err := Parse(with(t, func(map[string]any) {}))
	if err != nil {
		t.Fatal(err)
	}
	if full.Without(nil) != full || full.Without([]string{"providers/nope"}) != full {
		t.Fatal("nothing taken out: the same manifest")
	}
	if full.Without([]string{"connectors/examplenotes"}) != full {
		t.Fatal("a connector is not taken out here")
	}
}
