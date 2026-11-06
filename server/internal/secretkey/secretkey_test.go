package secretkey

import (
	"os"
	"path/filepath"
	"testing"
)

// A key file is made once; another Box on it (a restart, the other process) opens what the first sealed.
func TestSealOpenAcrossBoxes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")
	sealed, err := New(path).Seal("sk-test")
	if err != nil || sealed == "" || sealed == "sk-test" {
		t.Fatalf("sealed %q %v", sealed, err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 || fi.Size() != 32 {
		t.Fatalf("key file %v %v", fi, err)
	}
	if plain, err := New(path).Open(sealed); err != nil || plain != "sk-test" {
		t.Fatalf("opened %q %v", plain, err)
	}
	if plain, err := New("").Open(sealed); err == nil {
		t.Fatalf("another key opened it: %q", plain)
	}
	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(path).Seal("x"); err == nil {
		t.Fatal("a short key file was used")
	}
}

// A key in a directory not made yet (a manager's own, before anything else wrote there) is made with it.
func TestKeyInANewDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "managers", "assistant", "secret.key")
	if _, err := New(path).Seal("x"); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Dir(path)); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("directory %v %v", fi, err)
	}
}
