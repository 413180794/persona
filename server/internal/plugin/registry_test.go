package plugin

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
)

// memStore is the registry's store in memory.
type memStore struct {
	mu      sync.Mutex
	rows    map[string]Record
	secrets map[string]map[string]string
	audit   []string
}

func newMemStore() *memStore {
	return &memStore{rows: map[string]Record{}, secrets: map[string]map[string]string{}}
}

func (s *memStore) Plugins(context.Context) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Record
	for _, r := range s.rows {
		out = append(out, r)
	}
	return out, nil
}

func (s *memStore) PutPlugin(_ context.Context, r Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[r.ID] = r
	return nil
}

func (s *memStore) DeletePlugin(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rows, id)
	delete(s.secrets, id)
	return nil
}

func (s *memStore) PluginSecrets(_ context.Context, id string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for k, v := range s.secrets[id] {
		out[k] = v
	}
	return out, nil
}

func (s *memStore) PutPluginSecret(_ context.Context, id, name, sealed string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rows[id]; !ok {
		return errors.New("no row: the foreign key")
	}
	if s.secrets[id] == nil {
		s.secrets[id] = map[string]string{}
	}
	s.secrets[id][name] = sealed
	return nil
}

func (s *memStore) DeletePluginSecret(_ context.Context, id, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.secrets[id], name)
	return nil
}

func (s *memStore) AddPluginAudit(_ context.Context, id, action, actor string, detail json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audit = append(s.audit, id+" "+action+" "+actor+" "+string(detail))
	return nil
}

// rot13 stands for secret.key: sealed is not the plain text.
type rot13 struct{}

func (rot13) Seal(p string) (string, error) { return "sealed:" + rot(p), nil }
func (rot13) Open(s string) (string, error) {
	p, ok := strings.CutPrefix(s, "sealed:")
	if !ok {
		return "", errors.New("not sealed")
	}
	return rot(p), nil
}
func rot(s string) string {
	b := []byte(s)
	for i, c := range b {
		switch {
		case c >= 'a' && c <= 'z':
			b[i] = 'a' + (c-'a'+13)%26
		case c >= 'A' && c <= 'Z':
			b[i] = 'A' + (c-'A'+13)%26
		}
	}
	return string(b)
}

var platform = runtime.GOOS + "-" + runtime.GOARCH

// writePackage writes a package directory with the example manifest changed by change and its program.
func writePackage(t *testing.T, dir string, change func(m map[string]any)) {
	t.Helper()
	m := with(t, func(m map[string]any) {
		m["runtime"] = map[string]any{"command": map[string]any{platform: "bin/notes"}}
		m["engines"] = map[string]any{"plugin_protocol": 1}
		if change != nil {
			change(m)
		}
	})
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), m, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "notes"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func newTestRegistry(t *testing.T) (*Registry, *memStore, *[]Change) {
	store := newMemStore()
	r := NewRegistry(t.TempDir(), store, rot13{}, 202611100000)
	var changes []Change
	r.OnChange = func(c Change) { changes = append(changes, c) }
	return r, store, &changes
}

func TestInstallConfigureUpgradeRollbackUninstall(t *testing.T) {
	ctx := context.Background()
	r, store, changes := newTestRegistry(t)
	pkg := t.TempDir()
	writePackage(t, pkg, nil)

	pv, err := r.Preview(ctx, "dir:"+pkg, "")
	if err != nil {
		t.Fatal(err)
	}
	if pv.Upgrade || pv.Manifest.ID != "example-notes" || len(pv.Permissions) != 3 {
		t.Fatalf("%+v", pv)
	}
	if _, err := r.Install(ctx, pv.ID, "nope", "cli"); err == nil {
		t.Fatal("a digest that is not the preview's")
	}
	// A refused install used the preview up.
	pv, _ = r.Preview(ctx, pkg, "")
	p, err := r.Install(ctx, pv.ID, pv.Digest, "cli")
	if err != nil {
		t.Fatal(err)
	}
	if p.Problem != "" || p.Dir != filepath.Join(r.dir, "installed", "example-notes", "1.2.0") || !p.Record.Enabled {
		t.Fatalf("%+v", p)
	}
	if _, err := os.Stat(filepath.Join(p.Dir, "bin", "notes")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r.dir, "staging")); err == nil {
		if entries, _ := os.ReadDir(filepath.Join(r.dir, "staging")); len(entries) != 0 {
			t.Fatal("staging left behind", entries)
		}
	}
	// The original changing later changes nothing installed.
	_ = os.WriteFile(filepath.Join(pkg, "bin", "notes"), []byte("changed"), 0o755)
	if b, _ := os.ReadFile(filepath.Join(p.Dir, "bin", "notes")); string(b) != "#!/bin/sh\n" {
		t.Fatal("a copy, not a link")
	}
	if _, err := r.Preview(ctx, pkg, ""); err == nil {
		t.Fatal("installed already")
	}

	if m := r.Missing(ctx, p); len(m) != 1 || !strings.Contains(m[0], "API Key") {
		t.Fatal("needs its key and nothing else (url has a default):", m)
	}
	if err := r.SetConfig(ctx, "example-notes", json.RawMessage(`{"base_url": "nope"}`), "device:x"); err == nil {
		t.Fatal("held to the schema")
	}
	if err := r.SetConfig(ctx, "example-notes", json.RawMessage(`{"folders": ["a"]}`), "device:x"); err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	_ = json.Unmarshal(r.Config(r.Get("example-notes")), &cfg)
	if cfg["base_url"] != "https://notes.example.com" || cfg["include_shared"] != false {
		t.Fatal("defaults filled in:", cfg)
	}
	if err := r.SetSecret(ctx, "example-notes", "other", "x", "device:x"); err == nil {
		t.Fatal("an undeclared secret")
	}
	if err := r.SetSecret(ctx, "example-notes", "api_key", "", "device:x"); err == nil {
		t.Fatal("an empty secret")
	}
	if err := r.SetSecret(ctx, "example-notes", "api_key", "s3cret", "device:x"); err != nil {
		t.Fatal(err)
	}
	if store.secrets["example-notes"]["api_key"] == "s3cret" {
		t.Fatal("kept sealed")
	}
	if s, _ := r.Secrets(ctx, "example-notes"); s["api_key"] != "s3cret" {
		t.Fatal(s)
	}
	if m := r.Missing(ctx, r.Get("example-notes")); len(m) != 0 {
		t.Fatal(m)
	}
	for _, a := range store.audit {
		if strings.Contains(a, "s3cret") || strings.Contains(a, "\"a\"") {
			t.Fatal("the audit has no values:", a)
		}
	}

	// Upgrade: same permissions, no digest needed.
	pkg2 := t.TempDir()
	writePackage(t, pkg2, func(m map[string]any) { m["version"] = "1.3.0" })
	if _, err := r.Preview(ctx, pkg2, "other"); err == nil {
		t.Fatal("not the package of that plugin")
	}
	pv, err = r.Preview(ctx, pkg2, "example-notes")
	if err != nil {
		t.Fatal(err)
	}
	if !pv.Upgrade || pv.From != "1.2.0" || pv.MorePermissions {
		t.Fatalf("%+v", pv)
	}
	if p, err = r.Upgrade(ctx, "example-notes", pv.ID, "", "cli"); err != nil {
		t.Fatal(err)
	}
	if p.Record.Version != "1.3.0" || p.Record.PreviousVersion != "1.2.0" || string(p.Record.Config) != `{"folders":["a"]}` {
		t.Fatalf("%+v", p.Record)
	}
	if s, _ := r.Secrets(ctx, "example-notes"); s["api_key"] != "s3cret" {
		t.Fatal("secrets stay over an upgrade")
	}

	// Upgrade asking for more: the digest is needed.
	pkg3 := t.TempDir()
	writePackage(t, pkg3, func(m map[string]any) {
		m["version"] = "1.4.0"
		m["permissions"] = map[string]any{"network": []any{"notes.example.com"}, "hub_api": true}
	})
	pv, _ = r.Preview(ctx, pkg3, "example-notes")
	if !pv.MorePermissions {
		t.Fatal("asks for hub_api")
	}
	if _, err := r.Upgrade(ctx, "example-notes", pv.ID, "", "cli"); err == nil {
		t.Fatal("more permissions without consent")
	}
	pv, _ = r.Preview(ctx, pkg3, "example-notes")
	if p, err = r.Upgrade(ctx, "example-notes", pv.ID, pv.Digest, "cli"); err != nil {
		t.Fatal(err)
	}
	if !p.Record.Granted.HubAPI {
		t.Fatal("granted")
	}
	entries, _ := os.ReadDir(filepath.Join(r.dir, "installed", "example-notes"))
	if len(entries) != 2 {
		t.Fatal("the current and the previous version only:", entries)
	}
	pv, _ = r.Preview(ctx, pkg2, "example-notes")
	if pv != nil {
		t.Fatal("not newer")
	}

	p, err = r.Rollback(ctx, "example-notes", "cli")
	if err != nil {
		t.Fatal(err)
	}
	if p.Record.Version != "1.3.0" || p.Record.Granted.HubAPI || p.Record.PreviousVersion != "" {
		t.Fatalf("%+v", p.Record)
	}
	if _, err := r.Rollback(ctx, "example-notes", "cli"); err == nil {
		t.Fatal("only one step back")
	}

	if err := os.MkdirAll(r.DataDir("example-notes"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := r.Uninstall(ctx, "example-notes", false, "cli"); err != nil {
		t.Fatal(err)
	}
	if r.Get("example-notes") != nil || len(store.rows) != 0 || len(store.secrets) != 0 {
		t.Fatal("row and secrets gone")
	}
	for _, d := range []string{filepath.Join(r.dir, "installed", "example-notes"), r.DataDir("example-notes")} {
		if _, err := os.Stat(d); err == nil {
			t.Fatal(d, "left")
		}
	}
	var kinds []string
	for _, c := range *changes {
		kinds = append(kinds, c.Kind)
	}
	if strings.Join(kinds, ",") != "installed,config,secret,upgraded,upgraded,rolled_back,removed" {
		t.Fatal(kinds)
	}
	var actions []string
	for _, a := range store.audit {
		actions = append(actions, strings.Fields(a)[1])
	}
	if strings.Join(actions, ",") != "install,config,secret_set,upgrade,upgrade,rollback,uninstall" {
		t.Fatal(actions)
	}

	// The rows come back after a restart.
	pv, _ = r.Preview(ctx, pkg, "")
	if _, err := r.Install(ctx, pv.ID, pv.Digest, "cli"); err != nil {
		t.Fatal(err)
	}
	r2 := NewRegistry(r.dir, store, rot13{}, 202611100000)
	if err := r2.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if p := r2.Get("example-notes"); p == nil || p.Problem != "" || p.Manifest.Name != "Example Notes" {
		t.Fatalf("%+v", p)
	}
	r3 := NewRegistry(r.dir, store, rot13{}, 202611010000)
	_ = r3.Load(ctx)
	if p := r3.Get("example-notes"); p == nil || p.Problem != "" {
		t.Fatal("no engines.persona: runs anywhere")
	}
}

func TestPreviewRefuses(t *testing.T) {
	ctx := context.Background()
	r, _, _ := newTestRegistry(t)
	r.Reserve("connectors", "github")
	cases := map[string]func(m map[string]any){
		"reserved connector": func(m map[string]any) {
			m["contributes"] = map[string]any{"connectors": []any{map[string]any{"id": "github", "name": "x", "capabilities": []any{"status"}}}}
		},
		"no program for this platform": func(m map[string]any) {
			m["runtime"] = map[string]any{"command": map[string]any{"plan9-mips": "bin/notes"}}
		},
		"program not in the package": func(m map[string]any) { m["runtime"] = map[string]any{"command": "bin/other"} },
		"the hub's binary":           func(m map[string]any) { m["runtime"] = map[string]any{"command": "personad"} },
		"too new": func(m map[string]any) {
			m["engines"] = map[string]any{"persona": ">=202701010000", "plugin_protocol": 1}
		},
	}
	for name, change := range cases {
		pkg := t.TempDir()
		writePackage(t, pkg, change)
		if _, err := r.Preview(ctx, pkg, ""); err == nil {
			t.Errorf("%s: previewed", name)
		}
	}
	// Not executable.
	pkg := t.TempDir()
	writePackage(t, pkg, nil)
	_ = os.Chmod(filepath.Join(pkg, "bin", "notes"), 0o644)
	if _, err := r.Preview(ctx, pkg, ""); err == nil {
		t.Error("not executable")
	}
	// A link out of the package.
	pkg = t.TempDir()
	writePackage(t, pkg, nil)
	_ = os.Symlink("/etc/passwd", filepath.Join(pkg, "bin", "leak"))
	if _, err := r.Preview(ctx, pkg, ""); err == nil {
		t.Error("a link out of the package")
	}
	// Another plugin's connector.
	pkg = t.TempDir()
	writePackage(t, pkg, nil)
	pv, _ := r.Preview(ctx, pkg, "")
	if _, err := r.Install(ctx, pv.ID, pv.Digest, "cli"); err != nil {
		t.Fatal(err)
	}
	pkg = t.TempDir()
	writePackage(t, pkg, func(m map[string]any) { m["id"] = "copycat" })
	if _, err := r.Preview(ctx, pkg, ""); err == nil || !strings.Contains(err.Error(), "example-notes") {
		t.Error("its connector is example-notes':", err)
	}
	none := NewRegistry(t.TempDir(), nil, nil, 1)
	if _, err := none.Preview(ctx, pkg, ""); err == nil {
		t.Error("no database, no installs")
	}
}

func TestBuiltinAndHubJSON(t *testing.T) {
	ctx := context.Background()
	r, store, _ := newTestRegistry(t)
	m, err := Parse(with(t, func(m map[string]any) {
		m["id"] = "telegram"
		m["runtime"] = map[string]any{"command": "personad"}
		m["engines"] = map[string]any{"plugin_protocol": 1}
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.AddBuiltin(m, "/opt/persona", true); err != nil {
		t.Fatal(err)
	}
	voice := &Manifest{ManifestVersion: 1, ID: "voice", Name: "语音", Version: "0.0.0", Runtime: &Runtime{Command: Command{Path: "/usr/bin/x"}, RPC: "none", HTTP: true},
		ActivationEvents: []string{OnStartup}, Contributes: map[string]json.RawMessage{"voice": json.RawMessage(`[{"id":"voice"}]`)}, Permissions: Permissions{HubAPI: true}}
	if err := r.AddHubJSON(voice, "entry", true); err != nil {
		t.Fatal(err)
	}
	if err := r.AddHubJSON(voice, "entry", true); err == nil {
		t.Fatal("twice")
	}
	if err := r.SetEnabled(ctx, "voice", false, "cli"); err == nil {
		t.Fatal("hub.json's is read-only")
	}
	if err := r.Uninstall(ctx, "telegram", false, "cli"); err == nil {
		t.Fatal("a built-in one only switches off")
	}
	if err := r.SetEnabled(ctx, "telegram", false, "cli"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetSecret(ctx, "telegram", "api_key", "k", "cli"); err != nil {
		t.Fatal(err)
	}
	if len(store.rows) != 1 || store.rows["telegram"].Enabled {
		t.Fatal(store.rows)
	}
	r2 := NewRegistry(r.dir, store, rot13{}, 1)
	_ = r2.AddBuiltin(m, "/opt/persona", true)
	_ = r2.Load(ctx)
	if p := r2.Get("telegram"); p.Record.Enabled || p.Dir != "/opt/persona" {
		t.Fatalf("its row's switch: %+v", p.Record)
	}
	ids := []string{}
	for _, p := range r.List() {
		ids = append(ids, p.Record.ID+"/"+p.Record.Source)
	}
	if !slices.Equal(ids, []string{"telegram/builtin", "voice/hub.json"}) {
		t.Fatal(ids)
	}
}

// tarGz is a package archive of name → content (a name ending in / is a directory; "->target" a link).
func tarGz(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		switch {
		case strings.HasSuffix(name, "/"):
			_ = tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeDir, Mode: 0o755})
		case strings.HasPrefix(content, "->"):
			_ = tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeSymlink, Linkname: content[2:]})
		default:
			_ = tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(content))})
			_, _ = tw.Write([]byte(content))
		}
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func TestStageArchives(t *testing.T) {
	ctx := context.Background()
	manifest := string(with(t, func(m map[string]any) {
		m["runtime"] = map[string]any{"command": map[string]any{platform: "bin/notes"}}
		m["engines"] = map[string]any{"plugin_protocol": 1}
	}))
	good := tarGz(t, map[string]string{"notes-1.2.0/": "", "notes-1.2.0/" + ManifestFile: manifest, "notes-1.2.0/bin/notes": "#!/bin/sh\n",
		"notes-1.2.0/bin/run": "->notes"})
	file := filepath.Join(t.TempDir(), "notes.tar.gz")
	_ = os.WriteFile(file, good, 0o644)
	root, err := Stage(ctx, Source{Kind: "file", Path: file}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(root, "bin", "notes")); err != nil || fi.Mode()&0o111 == 0 {
		t.Fatal("the program, executable", err)
	}

	// Made on a Mac: AppleDouble files beside the top-level directory (tar without COPYFILE_DISABLE), and Finder's
	// __MACOSX/ and .DS_Store; none of them is unpacked, and the directory is still the package's root.
	mac := tarGz(t, map[string]string{"._notes": "\x00\x05\x16\x07", "notes/": "", "notes/._" + ManifestFile: "x",
		"notes/" + ManifestFile: manifest, "notes/bin/notes": "#!/bin/sh\n", "notes/bin/._notes": "x", "notes/.DS_Store": "x",
		"__MACOSX/notes/._bin": "x"})
	file = filepath.Join(t.TempDir(), "mac.tar.gz")
	_ = os.WriteFile(file, mac, 0o644)
	root, err = Stage(ctx, Source{Kind: "file", Path: file}, t.TempDir())
	if err != nil {
		t.Fatal("a package made on a Mac:", err)
	}
	var kept []string
	_ = filepath.WalkDir(filepath.Dir(root), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(filepath.Dir(root), p)
			kept = append(kept, filepath.ToSlash(rel))
		}
		return nil
	})
	if filepath.Base(root) != "notes" || strings.Join(kept, " ") != "notes/bin/notes notes/"+ManifestFile {
		t.Fatalf("%s: %v", root, kept)
	}
	macDir := filepath.Join(t.TempDir(), "notes")
	for rel, content := range map[string]string{ManifestFile: manifest, "._" + ManifestFile: "x", ".DS_Store": "x", "__MACOSX/x": "x"} {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(macDir, rel)), 0o755)
		_ = os.WriteFile(filepath.Join(macDir, rel), []byte(content), 0o644)
	}
	root, err = Stage(ctx, Source{Kind: "dir", Path: macDir}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 1 {
		t.Fatalf("a directory made on a Mac keeps %d entries", len(entries))
	}

	for name, files := range map[string]map[string]string{
		"escape":       {"../x": "x"},
		"absolute":     {"/etc/x": "x"},
		"link out":     {ManifestFile: manifest, "bin/x": "->../../etc/passwd"},
		"link abs":     {ManifestFile: manifest, "bin/x": "->/etc/passwd"},
		"no manifest":  {"a/x": "x", "b/y": "y"},
		"nested twice": {"a/b/" + ManifestFile: manifest},
	} {
		f := filepath.Join(t.TempDir(), "p.tar.gz")
		_ = os.WriteFile(f, tarGz(t, files), 0o644)
		if _, err := Stage(ctx, Source{Kind: "file", Path: f}, t.TempDir()); err == nil {
			t.Errorf("%s: staged", name)
		}
	}

	// zip, and an https download checked against its sha256.
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, _ := zw.Create(ManifestFile)
	_, _ = io.WriteString(w, manifest)
	w, _ = zw.Create("__MACOSX/._" + ManifestFile)
	_, _ = io.WriteString(w, "x")
	hd := &zip.FileHeader{Name: "bin/notes"}
	hd.SetMode(0o755)
	w, _ = zw.CreateHeader(hd)
	_, _ = io.WriteString(w, "#!/bin/sh\n")
	_ = zw.Close()
	sum := sha256.Sum256(zbuf.Bytes())
	old := Fetch
	defer func() { Fetch = old }()
	Fetch = func(_ context.Context, u string, w io.Writer) error {
		if u != "https://plugins.example.com/notes.zip" {
			t.Fatal(u)
		}
		_, err := w.Write(zbuf.Bytes())
		return err
	}
	src, err := ParseSource("https://plugins.example.com/notes.zip#sha256=" + hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Stage(ctx, src, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	src.SHA256 = strings.Repeat("0", 64)
	if _, err := Stage(ctx, src, t.TempDir()); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatal("a download that does not hash to its sum:", err)
	}
	for _, bad := range []string{"https://x.example.com/p.zip", "http://x.example.com/p.zip#sha256=" + strings.Repeat("0", 64), "remote:", "remote:tcp=x", ""} {
		if _, err := ParseSource(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
	if s, err := ParseSource("remote:rpc=/tmp/a.sock,http=/tmp/b.sock"); err != nil || s.RPCSocket != "/tmp/a.sock" || s.HTTPSocket != "/tmp/b.sock" {
		t.Fatal(s, err)
	}
}
