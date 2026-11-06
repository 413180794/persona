package assistant

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The key is made once, private to the user, and the same at the next start.
func TestHandleKey(t *testing.T) {
	dir := t.TempDir()
	a, err := handleKey(dir)
	if err != nil || len(a) != 32 {
		t.Fatalf("%d %v", len(a), err)
	}
	if st, err := os.Stat(filepath.Join(dir, "handles.key")); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", st.Mode(), err)
	}
	if b, err := handleKey(dir); err != nil || !bytes.Equal(a, b) {
		t.Fatal("another key at the next start")
	}
	if err := os.WriteFile(filepath.Join(dir, "handles.key"), []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := handleKey(dir); err == nil {
		t.Fatal("a short key was taken")
	}
}

// The key in a directory not made yet (the manager's own) is made with it.
func TestHandleKeyInANewDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "managers", "assistant")
	if k, err := handleKey(dir); err != nil || len(k) != 32 {
		t.Fatalf("%d %v", len(k), err)
	}
}
