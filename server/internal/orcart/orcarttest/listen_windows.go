package orcarttest

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/tailscale/go-winio"
)

// TempDir is a fresh directory, removed when the test ends.
func TempDir(t testing.TB) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "orca")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// Endpoint is the transport a fake in dir serves on, as Orca on Windows does: a named pipe, named after dir so that
// fakes running at once do not share one.
func Endpoint(dir string) (kind, socket string) {
	return "named-pipe", `\\.\pipe\orcatest-` + filepath.Base(dir)
}

// Listen serves on a transport Endpoint gave.
func Listen(kind, socket string) (net.Listener, error) {
	if kind == "unix" {
		_ = os.Remove(socket)
		return net.Listen("unix", socket)
	}
	return winio.ListenPipe(socket, nil)
}
