package hub

import (
	"os"
	"path/filepath"
	"testing"
)

// The server's binary is the one running, unless that is persona-hub (the hub alone): then the personad beside it,
// and an error when there is none (never persona-hub itself, which runs no built-in manager and is no update target).
func TestServerBinary(t *testing.T) {
	dir := t.TempDir()
	touch := func(name string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	personad, hub := touch("personad"), touch("persona-hub")
	for _, c := range []struct{ running, want string }{
		{personad, personad},
		{hub, personad},
		{touch("personad-linux-amd64"), filepath.Join(dir, "personad-linux-amd64")}, // the Docker image's
		{touch("hub.test"), filepath.Join(dir, "hub.test")},
	} {
		if got, err := serverBinaryOf(c.running); err != nil || got != c.want {
			t.Errorf("%s: %q %v, want %q", filepath.Base(c.running), got, err, c.want)
		}
	}
	if got, err := serverBinaryOf(touch("persona-hub.exe")); err == nil {
		t.Errorf("persona-hub.exe without personad.exe beside it: %q", got)
	}
	touch("personad.exe")
	if got, err := serverBinaryOf(filepath.Join(dir, "persona-hub.exe")); err != nil || got != filepath.Join(dir, "personad.exe") {
		t.Errorf("persona-hub.exe: %q %v", got, err)
	}
	alone := filepath.Join(t.TempDir(), "persona-hub")
	if err := os.WriteFile(alone, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := serverBinaryOf(alone); err == nil {
		t.Errorf("persona-hub without personad beside it: %q", got)
	}
}
