//go:build !windows

package herdr

import (
	"net"
	"path/filepath"
	"testing"
)

func TestSocketPathUnix(t *testing.T) {
	t.Setenv("HERDR_SOCKET_PATH", "")
	t.Setenv("HERDR_SESSION", "")
	t.Setenv("PERSONA_HOME", "") // a custom data directory has a herdr session of its own (session.go)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/me")
	if got, want := SocketPath(), "/home/me/.config/herdr/herdr.sock"; got != want {
		t.Fatalf("SocketPath() = %q, want %q", got, want)
	}
	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	if got, want := SocketPath(), "/cfg/herdr/herdr.sock"; got != want {
		t.Fatalf("with XDG_CONFIG_HOME: %q, want %q", got, want)
	}
	t.Setenv("HERDR_SOCKET_PATH", "/run/h.sock")
	if got := SocketPath(); got != "/run/h.sock" {
		t.Fatalf("with HERDR_SOCKET_PATH: %q", got)
	}
}

func TestSocketUp(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.sock")
	t.Setenv("HERDR_SOCKET_PATH", p)
	if SocketUp() {
		t.Fatal("up before anything listens")
	}
	ln, err := net.Listen("unix", p)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if !SocketUp() {
		t.Fatal("not up while listening")
	}
}
