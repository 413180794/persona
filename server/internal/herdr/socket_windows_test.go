package herdr

import (
	"testing"
)

// herdr on Windows serves its API on the named pipe named after the socket path (interprocess's GenericNamespaced).
func TestPipeName(t *testing.T) {
	for in, want := range map[string]string{
		`C:\Users\me\AppData\Roaming\herdr\herdr.sock`: `\\.\pipe\C:\Users\me\AppData\Roaming\herdr\herdr.sock`,
		`\\.\pipe\custom`: `\\.\pipe\custom`,
		`//./pipe/custom`: `//./pipe/custom`,
	} {
		if got := pipeName(in); got != want {
			t.Errorf("pipeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSocketPathWindows(t *testing.T) {
	t.Setenv("HERDR_SOCKET_PATH", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("APPDATA", `C:\Users\me\AppData\Roaming`)
	if got, want := SocketPath(), `C:\Users\me\AppData\Roaming\herdr\herdr.sock`; got != want {
		t.Fatalf("SocketPath() = %q, want %q", got, want)
	}
	t.Setenv("XDG_CONFIG_HOME", `D:\cfg`)
	if got, want := SocketPath(), `D:\cfg\herdr\herdr.sock`; got != want {
		t.Fatalf("with XDG_CONFIG_HOME: %q, want %q", got, want)
	}
}
