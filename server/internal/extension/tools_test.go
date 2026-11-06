package extension

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// tgz is a .tar.gz of files (name → content; a name ending in * is executable, without the star).
func tgz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		mode := int64(0o644)
		if n, ok := strings.CutSuffix(name, "*"); ok {
			name, mode = n, 0o755
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

// fakeDownloads serves Fetch from files by URL and counts the requests.
type fakeDownloads struct {
	mu    sync.Mutex
	files map[string][]byte
	got   []string
}

func (f *fakeDownloads) install(t *testing.T) {
	old := Fetch
	t.Cleanup(func() { Fetch = old })
	Fetch = func(_ context.Context, u string, w io.Writer) error {
		f.mu.Lock()
		f.got = append(f.got, u)
		b, ok := f.files[u]
		f.mu.Unlock()
		if !ok {
			return errors.New("HTTP 404")
		}
		_, err := w.Write(b)
		return err
	}
}

func (f *fakeDownloads) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.got)
}

const (
	nodeURL = "https://dl.example.com/fakenode-1.0.0.tar.gz"
	npmReg  = "https://npm.example.com"
)

// toolFixtures: an archive tool "fakenode" (prints v1.0.0) and an npm tool "hello-cli" that runs fakenode, so it
// only works with fakenode on PATH.
func toolFixtures(t *testing.T) (*fakeDownloads, map[string]any, map[string]any) {
	node := tgz(t, map[string]string{"fakenode-1.0.0/bin/fakenode*": "#!/bin/sh\necho v1.0.0\n"})
	npm := tgz(t, map[string]string{
		"package/package.json":  `{"name": "@example/hello-cli", "version": "2.3.4", "bin": {"hello-cli": "dist/index.js"}}`,
		"package/dist/index.js": "#!/bin/sh\nfakenode >/dev/null || exit 3\necho hello-cli 2.3.4\n", // not executable in the tarball
	})
	nsum := sha256.Sum256(node)
	isum := sha512.Sum512(npm)
	f := &fakeDownloads{files: map[string][]byte{
		nodeURL: node,
		npmReg + "/@example/hello-cli/-/hello-cli-2.3.4.tgz": npm,
	}}
	nodeSpec := map[string]any{"name": "fakenode", "version": "1.0.0",
		"check":   map[string]any{"min_version": "1.0.0"},
		"archive": map[string]any{platform: map[string]any{"url": nodeURL, "sha256": hex.EncodeToString(nsum[:]), "path": "fakenode-1.0.0/bin/fakenode"}}}
	npmSpec := map[string]any{"name": "hello-cli", "version": "2.3.4", "requires": []any{"fakenode"},
		"check": map[string]any{"args": []any{"--version"}},
		"npm": map[string]any{"package": "@example/hello-cli", "version": "2.3.4", "registry": npmReg,
			"integrity": "sha512-" + base64.StdEncoding.EncodeToString(isum[:])}}
	return f, nodeSpec, npmSpec
}

func noMachineTools(t *testing.T) {
	old := LookTool
	t.Cleanup(func() { LookTool = old })
	LookTool = func(string, string) string { return "" }
}

func installWithTools(t *testing.T, r *Registry, change func(m map[string]any)) (*Extension, []string) {
	t.Helper()
	ctx := context.Background()
	pkg := t.TempDir()
	writePackage(t, pkg, change)
	pv, err := r.Preview(ctx, "dir:"+pkg, "")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var steps []string
	ctx = WithProgress(ctx, func(step string, ev StepEvent) {
		if step == StepTools {
			mu.Lock()
			steps = append(steps, fmt.Sprintf("%s:%s:%s", ev.State, ev.Tool, ev.ToolState))
			mu.Unlock()
		}
	})
	p, err := r.Install(ctx, pv.ID, pv.Digest, "cli")
	if err != nil {
		t.Fatal(err)
	}
	return p, steps
}

func TestToolsInstallOnPATH(t *testing.T) {
	noMachineTools(t)
	f, nodeSpec, npmSpec := toolFixtures(t)
	f.install(t)
	r, _, _ := newTestRegistry(t)
	p, steps := installWithTools(t, r, func(m map[string]any) {
		m["tools"] = []any{npmSpec, nodeSpec} // hello-cli first: fakenode still installs before it
	})
	if f.count() != 2 {
		t.Fatal("downloads:", f.got)
	}
	tools := r.Tools(p.Record.ID, p.Record.Version)
	if len(tools) != 2 || tools[0].Name != "fakenode" || tools[1].Name != "hello-cli" || tools[0].Reused {
		t.Fatalf("%+v", tools)
	}
	if steps[len(steps)-1] != "done::" || !strings.Contains(strings.Join(steps, " "), "running:hello-cli:done") {
		t.Fatal(steps)
	}
	path := r.ToolPath(p.Record.ID, p.Record.Version, "/usr/bin:/bin")
	if !strings.HasPrefix(path, tools[1].Bin+string(os.PathListSeparator)+tools[0].Bin) || !strings.HasSuffix(path, ":/usr/bin:/bin") {
		t.Fatal(path)
	}
	cmd := exec.Command("/bin/sh", "-c", "hello-cli")
	cmd.Env = []string{"PATH=" + path}
	if out, err := cmd.CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "hello-cli 2.3.4" {
		t.Fatalf("%v %s", err, out)
	}
	// The permission line lists them.
	var line *PermissionLine
	for _, l := range Explain(p.Full) {
		if l.Key == "tools" {
			line = &l
		}
	}
	if line == nil || !strings.Contains(line.Detail, "hello-cli 2.3.4（npm：npm.example.com）") || !strings.Contains(line.Detail, "dl.example.com") {
		t.Fatal(line)
	}
	if err := r.Uninstall(context.Background(), p.Record.ID, false, "cli"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.ToolsDir(p.Record.ID)); !os.IsNotExist(err) {
		t.Fatal("tools left after uninstall", err)
	}
}

func TestToolsReuseMachineCopy(t *testing.T) {
	f, nodeSpec, _ := toolFixtures(t)
	f.install(t)
	t.Setenv("HOME", filepath.Join(t.TempDir(), "home")) // the copy below is not in it
	mine := filepath.Join(t.TempDir(), "fakenode")
	if err := os.WriteFile(mine, []byte("#!/bin/sh\necho fakenode v1.4.2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := LookTool
	t.Cleanup(func() { LookTool = old })
	LookTool = func(name, _ string) string {
		if name == "fakenode" {
			return mine
		}
		return ""
	}
	r, _, _ := newTestRegistry(t)
	p, _ := installWithTools(t, r, func(m map[string]any) { m["tools"] = []any{nodeSpec} })
	tools := r.Tools(p.Record.ID, p.Record.Version)
	if f.count() != 0 || len(tools) != 1 || !tools[0].Reused {
		t.Fatalf("%v %+v", f.got, tools)
	}
	if real, _ := filepath.EvalSymlinks(mine); tools[0].Path != real {
		t.Fatal(tools[0].Path)
	}
	// Too old: installed instead.
	r2, _, _ := newTestRegistry(t)
	nodeSpec["check"] = map[string]any{"min_version": "2.0.0"}
	p, _ = installWithTools(t, r2, func(m map[string]any) { m["tools"] = []any{nodeSpec} })
	if tools := r2.Tools(p.Record.ID, p.Record.Version); f.count() != 1 || tools[0].Reused {
		t.Fatalf("%+v", tools)
	}
}

func TestToolsChecksumFailsTheInstall(t *testing.T) {
	noMachineTools(t)
	f, nodeSpec, _ := toolFixtures(t)
	f.install(t)
	f.files[nodeURL] = []byte("not the archive")
	mirror := "https://mirror.example.com/fakenode-1.0.0.tar.gz"
	r, _, _ := newTestRegistry(t)
	ctx := context.Background()
	pkg := t.TempDir()
	writePackage(t, pkg, func(m map[string]any) { m["tools"] = []any{nodeSpec} })
	pv, err := r.Preview(ctx, "dir:"+pkg, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Install(ctx, pv.ID, pv.Digest, "cli")
	if !errors.Is(err, ErrTools) || !strings.Contains(err.Error(), "校验值") {
		t.Fatal(err)
	}
	if r.Get("example-notes") != nil {
		t.Fatal("installed anyway")
	}
	if _, err := os.Stat(r.ToolsDir("example-notes")); !os.IsNotExist(err) {
		t.Fatal("left tools behind", err)
	}
	// A mirror with the right file does it.
	good, _, _ := toolFixtures(t)
	f.files[mirror] = good.files[nodeURL]
	arch := nodeSpec["archive"].(map[string]any)[platform].(map[string]any)
	arch["mirrors"] = []any{mirror}
	pkg = t.TempDir()
	writePackage(t, pkg, func(m map[string]any) { m["tools"] = []any{nodeSpec} })
	pv, err = r.Preview(ctx, "dir:"+pkg, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Install(ctx, pv.ID, pv.Digest, "cli"); err != nil {
		t.Fatal(err)
	}
}

func TestToolsKeptAcrossUpgradeAndRollback(t *testing.T) {
	noMachineTools(t)
	f, nodeSpec, _ := toolFixtures(t)
	f.install(t)
	r, _, _ := newTestRegistry(t)
	ctx := context.Background()
	p, _ := installWithTools(t, r, func(m map[string]any) { m["tools"] = []any{nodeSpec} })
	upgrade := func(version string) {
		t.Helper()
		pkg := t.TempDir()
		writePackage(t, pkg, func(m map[string]any) { m["version"] = version; m["tools"] = []any{nodeSpec} })
		pv, err := r.Preview(ctx, "dir:"+pkg, p.Record.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.Upgrade(ctx, p.Record.ID, pv.ID, pv.Digest, "cli"); err != nil {
			t.Fatal(err)
		}
	}
	upgrade("1.3.0")
	if f.count() != 1 {
		t.Fatal("downloaded again for the same tool:", f.got)
	}
	if len(r.Tools(p.Record.ID, "1.2.0")) != 1 || len(r.Tools(p.Record.ID, "1.3.0")) != 1 {
		t.Fatal("both versions keep theirs")
	}
	upgrade("1.4.0")
	if r.Tools(p.Record.ID, "1.2.0") != nil || len(r.Tools(p.Record.ID, "1.4.0")) != 1 {
		t.Fatal("pruned with its package")
	}
	if _, err := r.Rollback(ctx, p.Record.ID, "cli"); err != nil {
		t.Fatal(err)
	}
	tools := r.Tools(p.Record.ID, "1.3.0")
	if len(tools) != 1 {
		t.Fatal("rolled back to a version without its tools")
	}
	if _, err := os.Stat(tools[0].Path); err != nil {
		t.Fatal(err)
	}
}

func TestToolsManifestRules(t *testing.T) {
	_, nodeSpec, npmSpec := toolFixtures(t)
	cases := map[string]func(m map[string]any){
		"exactly one of archive and npm": func(m map[string]any) {
			both := map[string]any{}
			for k, v := range nodeSpec {
				both[k] = v
			}
			both["npm"] = npmSpec["npm"]
			m["tools"] = []any{both}
		},
		"an https URL": func(m map[string]any) {
			m["tools"] = []any{map[string]any{"name": "x", "version": "1", "archive": map[string]any{platform: map[string]any{"url": "http://x.example.com/x", "sha256": strings.Repeat("a", 64)}}}}
		},
		"sha256: 64": func(m map[string]any) {
			m["tools"] = []any{map[string]any{"name": "x", "version": "1", "archive": map[string]any{platform: map[string]any{"url": "https://x.example.com/x", "sha256": "abc"}}}}
		},
		"which file of the archive": func(m map[string]any) {
			m["tools"] = []any{map[string]any{"name": "x", "version": "1", "archive": map[string]any{platform: map[string]any{"url": "https://x.example.com/x.tgz", "sha256": strings.Repeat("a", 64)}}}}
		},
		"requires \"nope\"": func(m map[string]any) {
			m["tools"] = []any{map[string]any{"name": "x", "version": "1", "requires": []any{"nope"}, "npm": npmSpec["npm"]}}
		},
		"go round in a circle": func(m map[string]any) {
			a := map[string]any{"name": "a", "version": "1", "requires": []any{"b"}, "npm": npmSpec["npm"]}
			b := map[string]any{"name": "b", "version": "1", "requires": []any{"a"}, "npm": npmSpec["npm"]}
			m["tools"] = []any{a, b}
		},
		"integrity": func(m map[string]any) {
			m["tools"] = []any{map[string]any{"name": "x", "version": "1", "npm": map[string]any{"package": "x", "version": "1.0.0", "integrity": "md5-abc"}}}
		},
		"auth.page \"login\"": func(m map[string]any) {
			m["auth"] = map[string]any{"title": "登录", "page": "login"}
		},
		"auth.title": func(m map[string]any) { m["auth"] = map[string]any{"title": ""} },
	}
	for want, change := range cases {
		m := with(t, func(m map[string]any) {
			m["runtime"] = map[string]any{"command": map[string]any{platform: "bin/notes"}}
			m["engines"] = map[string]any{"plugin_protocol": 1}
			change(m)
		})
		if _, err := Parse(m); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", want, err)
		}
	}
	m := with(t, func(m map[string]any) {
		m["runtime"] = map[string]any{"command": map[string]any{platform: "bin/notes"}}
		m["engines"] = map[string]any{"plugin_protocol": 1}
		m["tools"] = []any{nodeSpec, npmSpec}
		m["auth"] = map[string]any{"title": "登录", "description": "扫码"}
	})
	parsed, err := Parse(m)
	if err != nil {
		t.Fatal(err)
	}
	// Without tools the digest is what it was; with them, a new tool or source asks again, a new version does not.
	plain := *parsed
	plain.Tools = nil
	if Digest(&plain) == Digest(parsed) {
		t.Fatal("tools are agreed to")
	}
	newer := *parsed
	newer.Tools = append([]ToolSpec(nil), parsed.Tools...)
	newer.Tools[1].Version, newer.Tools[1].NPM = "2.4.0", &ToolNPM{Package: "@example/hello-cli", Version: "2.4.0", Integrity: parsed.Tools[1].NPM.Integrity, Registry: npmReg}
	if Digest(&newer) != Digest(parsed) || len(Added(parsed, &newer)) != 0 {
		t.Fatal("a newer version from the same place is not asked about")
	}
	if added := Added(&plain, parsed); len(added) != 1 || added[0].Key != "tools" {
		t.Fatal(added)
	}
}

func TestToolsInstallUnderSymlinkDir(t *testing.T) {
	noMachineTools(t)
	f, nodeSpec, _ := toolFixtures(t)
	f.install(t)
	realDir := t.TempDir()
	linkDir := filepath.Join(t.TempDir(), "symlink-registry")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Fatal(err)
	}
	store := newMemStore()
	r := NewRegistry(linkDir, store, rot13{}, 202611100000)
	p, _ := installWithTools(t, r, func(m map[string]any) { m["tools"] = []any{nodeSpec} })
	tools := r.Tools(p.Record.ID, p.Record.Version)
	if len(tools) != 1 || tools[0].Name != "fakenode" || tools[0].Reused {
		t.Fatalf("%+v", tools)
	}
	if _, err := os.Stat(tools[0].Path); err != nil {
		t.Fatal(err)
	}
}
