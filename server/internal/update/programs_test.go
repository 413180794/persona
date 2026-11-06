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

// A program released with personad (personad-wechat-<os>-<arch>.gz) is listed under its name, not taken for a
// personad platform ("wechat-linux-amd64").
func TestBuildManifestPrograms(t *testing.T) {
	dir := t.TempDir()
	writeGz(t, filepath.Join(dir, "personad-linux-amd64.gz"), []byte("personad"))
	writeGz(t, filepath.Join(dir, "personad-wechat-linux-amd64.gz"), []byte("personad-wechat"))
	writeGz(t, filepath.Join(dir, "personad-wechat-darwin-arm64.gz"), []byte("personad-wechat for a Mac"))
	m, err := BuildManifest(dir, Manifest{Version: "v1", Build: 202611030000})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Files) != 1 || m.Files["linux-amd64"].Name != "personad-linux-amd64.gz" {
		t.Fatalf("files %+v", m.Files)
	}
	tg := m.Programs["personad-wechat"]
	if len(tg) != 2 || tg["linux-amd64"].Name != "personad-wechat-linux-amd64.gz" || tg["darwin-arm64"].BinarySHA256 == "" {
		t.Fatalf("programs %+v", m.Programs)
	}
	// Only programs, no personad: not a release.
	only := t.TempDir()
	writeGz(t, filepath.Join(only, "personad-wechat-linux-amd64.gz"), []byte("x"))
	if _, err := BuildManifest(only, Manifest{Version: "v1", Build: 1}); err == nil {
		t.Fatal("a release without personad")
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
	target := write("personad", "old personad")
	write("personad-old-program", "old program")
	newHub, newProg := write("new-personad", "new personad"), write("new-program", "new program")

	prev, err := Swap(target, newHub)
	if err != nil {
		t.Fatal(err)
	}
	p := Pending{Target: target, Prev: prev}
	had, err := SwapProgram(filepath.Join(dir, "personad-old-program"), newProg)
	if err != nil || had.Prev == "" {
		t.Fatalf("an existing program: %+v %v", had, err)
	}
	fresh, err := SwapProgram(filepath.Join(dir, "personad-new-program"), newProg)
	if err != nil || fresh.Prev != "" {
		t.Fatalf("a new program: %+v %v", fresh, err)
	}
	p.Programs = []Swapped{had, fresh}
	if read("personad") != "new personad" || read("personad-old-program") != "new program" || read("personad-new-program") != "new program" {
		t.Fatal("not swapped")
	}
	if err := RestoreAll(p); err != nil {
		t.Fatal(err)
	}
	if read("personad") != "old personad" || read("personad-old-program") != "old program" || read("personad-new-program") != "<none>" {
		t.Fatalf("went back to %q %q %q", read("personad"), read("personad-old-program"), read("personad-new-program"))
	}
}
