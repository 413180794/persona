//go:build !windows

package orcarttest

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

// TempDir is a fresh directory under /tmp (unix socket paths are short), removed when the test ends.
func TempDir(t testing.TB) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "orca")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// Endpoint is the transport a fake in dir serves on, as Orca on this platform does: a unix socket.
func Endpoint(dir string) (kind, socket string) { return "unix", filepath.Join(dir, "o.sock") }

// Listen serves on a transport Endpoint gave.
func Listen(_, socket string) (net.Listener, error) {
	_ = os.Remove(socket)
	return net.Listen("unix", socket)
}
