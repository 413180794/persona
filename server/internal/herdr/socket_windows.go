package herdr

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/tailscale/go-winio"
)

// platformConfigDir is herdr's config directory when $XDG_CONFIG_HOME is not set: %APPDATA%\herdr, else
// %USERPROFILE%\AppData\Roaming\herdr (herdr's own platform_config_dir).
func platformConfigDir() string {
	if d := os.Getenv("APPDATA"); d != "" {
		return filepath.Join(d, "herdr")
	}
	if d := os.Getenv("USERPROFILE"); d != "" {
		return filepath.Join(d, "AppData", "Roaming", "herdr")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "AppData", "Roaming", "herdr")
}

// pipeName is the named pipe herdr serves its API on for a socket path: herdr on Windows takes the path as a name in
// the pipe namespace (interprocess's GenericNamespaced), so C:\Users\me\AppData\Roaming\herdr\herdr.sock is
// \\.\pipe\C:\Users\me\AppData\Roaming\herdr\herdr.sock. A path that already is a pipe name is used as it is.
func pipeName(path string) string {
	if strings.HasPrefix(path, `\\.\pipe\`) || strings.HasPrefix(path, `//./pipe/`) {
		return path
	}
	return `\\.\pipe\` + path
}

// dialSocket connects to herdr's named pipe.
func dialSocket(ctx context.Context, path string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, pipeName(path))
}

// socketExists asks for the pipe without opening it (opening one takes the server's only free instance for a moment).
func socketExists(path string) bool {
	_, err := os.Stat(pipeName(path))
	return err == nil
}

// listenSocket listens where herdr would serve its API: the named pipe for path.
func listenSocket(path string) (net.Listener, error) { return winio.ListenPipe(pipeName(path), nil) }
