package extension

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// A computer gets the package with its own program only, the same bytes every time (its digest tells it has it),
// and unpacks it as installing does.
func TestNodePackage(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{"node/helper-linux": "linux", "node/helper-mac": "mac", "bin/hub": "hub", "data/words.txt": "words"} {
		_ = os.MkdirAll(filepath.Join(root, filepath.Dir(name)), 0o755)
		_ = os.WriteFile(filepath.Join(root, name), []byte(body), 0o755)
	}
	_ = os.Symlink("words.txt", filepath.Join(root, "data", "link"))
	m, err := Parse(withNode(t, func(n, f map[string]any) {
		n["command"] = map[string]any{"darwin-arm64": "node/helper-mac", "linux-amd64": "node/helper-linux"}
		f["items"].(map[string]any)["x-os"] = []any{"darwin", "linux"}
	}))
	if err != nil {
		t.Fatal(err)
	}
	m.Runtime.Command = Command{ByPlatform: map[string]string{"linux-amd64": "bin/hub"}}
	var a, b bytes.Buffer
	if err := NodePackage(m, root, "linux-amd64", &a); err != nil {
		t.Fatal(err)
	}
	if err := NodePackage(m, root, "linux-amd64", &b); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("the same package, other bytes")
	}
	file := filepath.Join(t.TempDir(), "p.tgz")
	digest, size, err := NodePackageFile(m, root, "linux-amd64", file)
	if err != nil || size != int64(a.Len()) || len(digest) != 64 {
		t.Fatal(digest, size, err)
	}
	to := t.TempDir()
	if err := UnpackNodePackage(file, filepath.Join(to, "x")); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{"node/helper-linux": true, "node/helper-mac": false, "bin/hub": false, "data/words.txt": true, "data/link": true} {
		if _, err := os.Lstat(filepath.Join(to, "x", name)); (err == nil) != want {
			t.Errorf("%s in the package: %v", name, err == nil)
		}
	}
	if fi, _ := os.Stat(filepath.Join(to, "x", "node", "helper-linux")); fi == nil || fi.Mode()&0o111 == 0 {
		t.Fatal("the program is not executable")
	}
	if err := NodePackage(m, root, "windows-amd64", &a); err == nil {
		t.Fatal("a platform it has no program for")
	}
	if !m.NodeRuns("Linux", "AMD64") || m.NodeRuns("windows", "amd64") {
		t.Fatal("NodeRuns")
	}
	_ = os.Symlink("../../outside", filepath.Join(root, "data", "out"))
	if err := NodePackage(m, root, "linux-amd64", &a); err == nil {
		t.Fatal("a link out of the package")
	}
}
