package update

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func writeGz(t *testing.T, path string, content []byte) {
	t.Helper()
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	zw.Write(content)
	zw.Close()
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A program released with jarvisd (jarvisd-wechat-<os>-<arch>.gz) is listed under its name, not taken for a
// jarvisd platform ("wechat-linux-amd64").
func TestBuildManifestPrograms(t *testing.T) {
	dir := t.TempDir()
	writeGz(t, filepath.Join(dir, "jarvisd-linux-amd64.gz"), []byte("jarvisd"))
	writeGz(t, filepath.Join(dir, "jarvisd-wechat-linux-amd64.gz"), []byte("jarvisd-wechat"))
	writeGz(t, filepath.Join(dir, "jarvisd-wechat-darwin-arm64.gz"), []byte("jarvisd-wechat for a Mac"))
	m, err := BuildManifest(dir, Manifest{Version: "v1", Build: 202611030000})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Files) != 1 || m.Files["linux-amd64"].Name != "jarvisd-linux-amd64.gz" {
		t.Fatalf("files %+v", m.Files)
	}
	tg := m.Programs["jarvisd-wechat"]
	if len(tg) != 2 || tg["linux-amd64"].Name != "jarvisd-wechat-linux-amd64.gz" || tg["darwin-arm64"].BinarySHA256 == "" {
		t.Fatalf("programs %+v", m.Programs)
	}
	// Only programs, no jarvisd: not a release.
	only := t.TempDir()
	writeGz(t, filepath.Join(only, "jarvisd-wechat-linux-amd64.gz"), []byte("x"))
	if _, err := BuildManifest(only, Manifest{Version: "v1", Build: 1}); err == nil {
		t.Fatal("a release without jarvisd")
	}
}

// A program swapped with the hub goes back with it: the old build when there was one, removed when it is new.
func TestSwapProgramAndRestoreAll(t *testing.T) {
	dir := t.TempDir()
	write := func(name, s string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(s), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return "<none>"
		}
		return string(b)
	}
	target := write("jarvisd", "old jarvisd")
	write("jarvisd-old-program", "old program")
	newHub, newProg := write("new-jarvisd", "new jarvisd"), write("new-program", "new program")

	prev, err := Swap(target, newHub)
	if err != nil {
		t.Fatal(err)
	}
	p := Pending{Target: target, Prev: prev}
	had, err := SwapProgram(filepath.Join(dir, "jarvisd-old-program"), newProg)
	if err != nil || had.Prev == "" {
		t.Fatalf("an existing program: %+v %v", had, err)
	}
	fresh, err := SwapProgram(filepath.Join(dir, "jarvisd-new-program"), newProg)
	if err != nil || fresh.Prev != "" {
		t.Fatalf("a new program: %+v %v", fresh, err)
	}
	p.Programs = []Swapped{had, fresh}
	if read("jarvisd") != "new jarvisd" || read("jarvisd-old-program") != "new program" || read("jarvisd-new-program") != "new program" {
		t.Fatal("not swapped")
	}
	if err := RestoreAll(p); err != nil {
		t.Fatal(err)
	}
	if read("jarvisd") != "old jarvisd" || read("jarvisd-old-program") != "old program" || read("jarvisd-new-program") != "<none>" {
		t.Fatalf("went back to %q %q %q", read("jarvisd"), read("jarvisd-old-program"), read("jarvisd-new-program"))
	}
}
