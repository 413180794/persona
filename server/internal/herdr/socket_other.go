//go:build !windows

package herdr

import (
	"context"
	"net"
	"os"
	"path/filepath"
)

// platformConfigDir is herdr's config directory when $XDG_CONFIG_HOME is not set: ~/.config/herdr.
func platformConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "herdr")
}

// dialSocket connects to herdr's unix socket.
func dialSocket(ctx context.Context, path string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "unix", path)
}

func socketExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// listenSocket listens where a herdr would serve its API: a unix socket at path.
func listenSocket(path string) (net.Listener, error) { return net.Listen("unix", path) }
