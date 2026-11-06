package identity

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSocketDirectorySupportsTheUnixPathLimit(t *testing.T) {
	cleanRootEnv(t)
	limit := 103
	if runtime.GOOS == "linux" {
		limit = 107
	} else if runtime.GOOS != "darwin" {
		t.Skip("Unix socket path limit is platform-specific")
	}
	base, err := os.MkdirTemp(os.TempDir(), "pc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	count := limit - len(filepath.Join(base, ".persona", "node-attach.sock")) - 1
	if count < 1 {
		t.Skip("temporary directory is already longer than the Unix socket limit")
	}
	home := filepath.Join(base, strings.Repeat("h", count))
	if _, err := PrepareHome(home); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(SocketDir(home), "node-attach.sock")
	if len(path) != limit {
		t.Fatalf("Unix path length changed: %d, want %d", len(path), limit)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal("valid Unix socket path was rejected:", err)
	}
	listener.Close()
}
