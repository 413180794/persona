package extension

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A package uploaded in the browser previews like a file on the hub's machine, its origin the file's name.
func TestPreviewUpload(t *testing.T) {
	ctx := context.Background()
	r, store, _ := newTestRegistry(t)
	manifest := string(with(t, func(m map[string]any) {
		m["runtime"] = map[string]any{"command": map[string]any{platform: "bin/notes"}}
		m["engines"] = map[string]any{"plugin_protocol": 1}
	}))
	pkg := tarGz(t, map[string]string{"notes/": "", "notes/" + ManifestFile: manifest, "notes/bin/notes": "#!/bin/sh\n"})

	if _, err := r.PreviewUpload(ctx, "notes.exe", bytes.NewReader(pkg), ""); err == nil {
		t.Fatal("not a package's name")
	}
	pv, err := r.PreviewUpload(ctx, `C:\downloads\notes-1.2.0.tar.gz`, bytes.NewReader(pkg), "")
	if err != nil {
		t.Fatal(err)
	}
	if pv.Manifest.ID != "example-notes" || pv.source.String() != "upload:notes-1.2.0.tar.gz" {
		t.Fatalf("%+v %s", pv.Manifest, pv.source)
	}
	if _, err := os.Stat(filepath.Join(pv.staging, "upload")); !os.IsNotExist(err) {
		t.Fatal("the upload is kept after unpacking:", err)
	}
	p, err := r.Install(ctx, pv.ID, pv.Digest, "admin UI")
	if err != nil {
		t.Fatal(err)
	}
	if p.Record.Origin != "upload:notes-1.2.0.tar.gz" || store.rows["example-notes"].Origin != p.Record.Origin {
		t.Fatal(p.Record.Origin)
	}
	if _, err := os.Stat(filepath.Join(p.Dir, "bin", "notes")); err != nil {
		t.Fatal(err)
	}

	// Not a package at all; a package without a manifest.
	if _, err := r.PreviewUpload(ctx, "x.zip", strings.NewReader("not a zip"), ""); err == nil {
		t.Fatal("garbage unpacked")
	}
	if _, err := r.PreviewUpload(ctx, "x.tar.gz", bytes.NewReader(tarGz(t, map[string]string{"README": "hi"})), ""); err == nil {
		t.Fatal("no manifest")
	}
	entries, _ := os.ReadDir(filepath.Join(r.dir, "staging"))
	if len(entries) != 0 {
		t.Fatalf("failed uploads leave %d staging directories", len(entries))
	}
}

func TestUploadName(t *testing.T) {
	for in, want := range map[string]string{"a.tgz": "a.tgz", "../../etc/a.ZIP": "a.ZIP", "dir/b.tar.gz": "b.tar.gz", "x\n.zip": "x.zip"} {
		if got, err := UploadName(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, in := range []string{"", "a.tar", "a.gz", "a.zip.exe", ".."} {
		if _, err := UploadName(in); err == nil {
			t.Errorf("%q passed", in)
		}
	}
}

// An upgrade's preview names only what the new version adds.
func TestAddedPermissions(t *testing.T) {
	old, err := Parse([]byte(exampleManifest))
	if err != nil {
		t.Fatal(err)
	}
	old.Runtime = nil // a declaration-only plugin before
	m, err := Parse(with(t, func(m map[string]any) {
		m["permissions"] = map[string]any{"network": []string{"notes.example.com", "files.example.com"}, "hub_api": true, "launcher_keys": true}
		m["secrets"] = []map[string]any{{"name": "api_key", "title": "API Key"}, {"name": "webhook", "title": "Webhook 密钥"}}
	}))
	if err != nil {
		t.Fatal(err)
	}
	got := Added(old, m)
	var keys []string
	for _, l := range got {
		keys = append(keys, l.Key+"="+l.Detail)
	}
	s := strings.Join(keys, "|")
	if !strings.HasPrefix(s, "runs_code=") || !strings.Contains(s, "network=files.example.com|") || !strings.Contains(s, "hub_api=") || !strings.Contains(s, "launcher_keys=") || !strings.HasSuffix(s, "secrets=Webhook 密钥") {
		t.Fatal(s)
	}
	if Added(m, m) != nil {
		t.Fatal("the same version adds nothing")
	}
}

// A channel's messages reach the assistant as the owner's own, so an upgrade adding one (or adding an extension it
// contributes to) asks for consent as the install screen does, not only the first install.
func TestUpgradeAddingChannelNeedsConsent(t *testing.T) {
	old, err := Parse([]byte(exampleManifest))
	if err != nil {
		t.Fatal(err)
	}
	conn := []any{map[string]any{"id": "examplenotes", "name": "Example Notes", "capabilities": []any{"status", "login"}}}
	ch := map[string]any{"id": "examplechat", "name": "Example Chat", "kind": "im", "connector": "examplenotes", "bind": "qr",
		"capabilities": map[string]any{"images_in": true, "format": "plain", "max_text": 4000, "asks": "text", "push": true, "typing": true}}
	m, err := Parse(with(t, func(m map[string]any) {
		m["version"] = "9.0.0"
		m["depends"] = []any{map[string]any{"id": "search"}}
		m["contributes"].(map[string]any)["connectors"] = conn
		m["contributes"].(map[string]any)["channel"] = []any{ch}
		m["contributes"].(map[string]any)["search.sources"] = []any{map[string]any{"id": "notes"}}
	}))
	if err != nil {
		t.Fatal(err)
	}
	cur := &Extension{Record: Record{Granted: old.Permissions}, Manifest: old, Full: old}
	if !MorePermissions(cur, m) {
		t.Fatal("a new channel went in without consent")
	}
	var keys []string
	for _, l := range Added(old, m) {
		keys = append(keys, l.Key+"="+l.Detail)
	}
	s := strings.Join(keys, "|")
	if !strings.Contains(s, "channel=") || !strings.Contains(s, "Example Chat") || !strings.Contains(s, "contributes_to=") || !strings.Contains(s, "search") {
		t.Fatal(s)
	}
	// Already granted: the same channel and contributions again add nothing.
	cur = &Extension{Record: Record{Granted: m.Permissions}, Manifest: m, Full: m}
	if MorePermissions(cur, m) || Added(m, m) != nil {
		t.Fatal("the same channel asked again")
	}
	// Only the contribution to another extension is new.
	noChan, err := Parse(with(t, func(m map[string]any) {
		m["contributes"].(map[string]any)["connectors"] = conn
		m["contributes"].(map[string]any)["channel"] = []any{ch}
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !MorePermissions(&Extension{Record: Record{Granted: noChan.Permissions}, Manifest: noChan, Full: noChan}, m) {
		t.Fatal("a new contributes_to went in without consent")
	}
}
