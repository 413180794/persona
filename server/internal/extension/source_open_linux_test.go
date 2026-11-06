//go:build linux

package extension

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestSourceOpenRejectsLinksAndSpecialFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.mjs"), []byte("export default 1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("main.mjs", filepath.Join(root, "link.mjs")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, filepath.Join(root, "dir")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(root, "fifo.mjs"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"link.mjs", "dir/main.mjs", "fifo.mjs", "../main.mjs"} {
		if f, err := openSourceFile(root, rel); err == nil {
			_ = f.Close()
			t.Fatal("descriptor walk accepted special/link path")
		}
	}
	link := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	b := []byte("export default 1")
	h := sha256.Sum256(b)
	if _, err := CheckSourceBundleIntegrity(link, &SourceRuntime{Bundle: "main.mjs", SHA256: hex.EncodeToString(h[:])}); err != nil {
		t.Fatal("root symlink regressed", err)
	}
}
