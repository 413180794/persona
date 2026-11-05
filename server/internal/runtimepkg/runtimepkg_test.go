package runtimepkg

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// The same vector as integrations/runtime-sdk/test/loader.test.js: the loader and jarvisd must hash alike.
func TestHashVector(t *testing.T) {
	got := Hash([]File{{Path: "b/c.js", Data: []byte("y")}, {Path: "a.js", Data: []byte("x")}})
	if want := "d6f3031b914b7e5243281ea484c24fc9ef36488a09bf7fe78df53a1101f06b8f"; got != want {
		t.Fatalf("hash %s, want %s", got, want)
	}
}

func TestValidPath(t *testing.T) {
	for p, ok := range map[string]bool{"dsh/lib/a.js": true, "a.js": true, "": false, "/etc/x": false, "../x.js": false,
		"a/../b.js": false, "a//b.js": false, "a\\b.js": false, "./a.js": false} {
		if ValidPath(p) != ok {
			t.Errorf("ValidPath(%q) = %v", p, !ok)
		}
	}
}

func TestLoadOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(OverrideEnv, dir)
	if p, err := Load("jarvis-dsh-runtime"); p != nil || err != nil {
		t.Fatalf("no package there: %v %v", p, err)
	}
	root := filepath.Join(dir, "jarvis-dsh-runtime")
	for path, data := range map[string]string{"dsh/lib/a.js": "x", "runtime-sdk/index.js": "y"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(path)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, path), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p, err := Load("jarvis-dsh-runtime")
	if err != nil || p == nil {
		t.Fatalf("load: %v %v", p, err)
	}
	if len(p.Files) != 2 || p.Files[0].Path != "dsh/lib/a.js" || p.Hash != Hash(p.Files) {
		t.Fatalf("package %+v", p)
	}
	if _, err := Load("../x"); err == nil {
		t.Fatal("a bad name loaded")
	}
}

// The generator copies what the patterns say, and fails when one matches nothing.
func TestGenerate(t *testing.T) {
	src, out := t.TempDir(), t.TempDir()
	cmd := map[string]string{"dsh/package.json": "{}", "dsh/lib/a.js": "a", "dsh/lib/b.txt": "no", "runtime-sdk/package.json": "{}",
		"runtime-sdk/index.js": "i", "runtime-sdk/lib/r.js": "r", "dsh/test/t.js": "no",
		"pi/package.json": "{}", "pi/index.js": "x", "pi/companion.mjs": "c", "pi/lib/pi.js": "p", "pi/e2e/run.mjs": "no",
		"paseo/package.json": "{}", "paseo/bridge.mjs": "b", "paseo/lib/bridge.js": "l", "paseo/test/bridge.test.js": "no",
		"agent-sdk/package.json": "{}", "agent-sdk/dist/bridge.mjs": "b", "agent-sdk/src/bridge.mjs": "no", "agent-sdk/build.mjs": "no"}
	for path, data := range cmd {
		_ = os.MkdirAll(filepath.Join(src, filepath.Dir(path)), 0o755)
		if err := os.WriteFile(filepath.Join(src, path), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := Generate(src, out)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, f := range files["jarvis-dsh-runtime"] {
		got = append(got, f.Path)
	}
	want := []string{"dsh/package.json", "dsh/lib/a.js", "runtime-sdk/package.json", "runtime-sdk/index.js", "runtime-sdk/lib/r.js"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("copied %v, want %v", got, want)
	}
	if _, err := os.Stat(filepath.Join(out, "jarvis-dsh-runtime", "dsh", "test")); err == nil {
		t.Fatal("tests were copied")
	}
	got = got[:0]
	for _, f := range files["jarvis-pi-runtime"] {
		got = append(got, f.Path)
	}
	want = []string{"pi/package.json", "pi/index.js", "pi/companion.mjs", "pi/lib/pi.js", "runtime-sdk/package.json", "runtime-sdk/index.js", "runtime-sdk/lib/r.js"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pi copied %v, want %v", got, want)
	}
	got = got[:0]
	for _, f := range files["jarvis-paseo-runtime"] {
		got = append(got, f.Path)
	}
	want = []string{"paseo/package.json", "paseo/bridge.mjs", "paseo/lib/bridge.js", "runtime-sdk/package.json", "runtime-sdk/index.js", "runtime-sdk/lib/r.js"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paseo copied %v, want %v", got, want)
	}
	got = got[:0]
	for _, f := range files["jarvis-agent-sdk-runtime"] {
		got = append(got, f.Path)
	}
	if want = []string{"agent-sdk/package.json", "agent-sdk/dist/bridge.mjs"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("agent-sdk copied %v, want %v", got, want)
	}
	_ = os.Remove(filepath.Join(src, "runtime-sdk", "index.js"))
	if _, err := Generate(src, out); err == nil || !strings.Contains(err.Error(), "matches no file") {
		t.Fatalf("a missing file: %v", err)
	}
}

// The loader's list (integrations/runtime-sdk/lib/package.js) is this one: otherwise every hello looks like another
// version.
func TestPackagesMatchTheLoader(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "integrations", "runtime-sdk", "lib", "package.js"))
	if err != nil {
		t.Skipf("no integrations checkout beside server/: %v", err)
	}
	block := regexp.MustCompile(`(?s)export const PACKAGES = \{(.*?)\n\}`).FindSubmatch(src)
	if block == nil {
		t.Fatal("PACKAGES not found in package.js")
	}
	js := map[string][]string{}
	for _, m := range regexp.MustCompile(`'([^']+)':\s*\[([^\]]*)\]`).FindAllSubmatch(block[1], -1) {
		var list []string
		for _, q := range regexp.MustCompile(`'([^']+)'`).FindAllSubmatch(m[2], -1) {
			list = append(list, string(q[1]))
		}
		js[string(m[1])] = list
	}
	if !reflect.DeepEqual(js, Packages) {
		t.Fatalf("package.js PACKAGES %v, runtimepkg.Packages %v", js, Packages)
	}
}

// The Agent SDK bridge as committed (integrations/agent-sdk/dist, the SDK bundled in): it generates, loads, and stays
// under MaxSize, the bound every package is offered and fetched within.
func TestAgentSDKPackage(t *testing.T) {
	src := filepath.Join("..", "..", "..", "integrations")
	if _, err := os.Stat(filepath.Join(src, "agent-sdk", "dist", "bridge.mjs")); err != nil {
		t.Skipf("no integrations checkout beside server/: %v", err)
	}
	out := t.TempDir()
	if _, err := Generate(src, out); err != nil {
		t.Fatal(err)
	}
	t.Setenv(OverrideEnv, out)
	p, err := Load("jarvis-agent-sdk-runtime")
	if err != nil || p == nil {
		t.Fatalf("load: %v %v", p, err)
	}
	size := 0
	for _, f := range p.Files {
		size += len(f.Data)
	}
	if len(p.Files) != 2 || size > MaxSize || size < 100<<10 {
		t.Fatalf("%d files, %d bytes (MaxSize %d)", len(p.Files), size, MaxSize)
	}
}
