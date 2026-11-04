package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/connector"
)

// personad plugin on the hub's machine: install (showing what it may do), list, configure, a secret from stdin,
// uninstall; all over loopback with hub.json's token.
func TestPluginCommand(t *testing.T) {
	h, store := pluginTestHub(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h.routes()}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	home := t.TempDir()
	t.Setenv("PERSONA_HOME", home)
	t.Setenv("JARVIS_HOME", "")
	t.Setenv("PERSONA_LISTEN", "")
	t.Setenv("JARVIS_LISTEN", "")
	b, _ := json.Marshal(map[string]any{"listen": ln.Addr().String(), "token": h.cfg.Token})
	if err := os.WriteFile(filepath.Join(home, "hub.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(stdin string, args ...string) string {
		t.Helper()
		var out, errOut bytes.Buffer
		if err := PluginCommand(context.Background(), args, strings.NewReader(stdin), &out, &errOut); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out.String())
		}
		return out.String()
	}
	pkg := notesPackage(t, "1.0.0")

	var out bytes.Buffer
	if err := PluginCommand(context.Background(), []string{"install", pkg}, strings.NewReader("n\n"), &out, &out); err == nil {
		t.Fatal("said no")
	}
	if !strings.Contains(out.String(), "在 hub 机器上运行程序") || !strings.Contains(out.String(), "notes.example.com") {
		t.Fatal("what it may do is shown first:\n" + out.String())
	}
	if got := run("y\n", "install", pkg); !strings.Contains(got, "needs_setup") || !strings.Contains(got, "API Key") {
		t.Fatal(got)
	}
	run("", "config", "example-notes", `folders=["a","b"]`)
	run("s3cret\n", "secret", "example-notes", "api_key")
	if store.secrets["example-notes"]["api_key"] == "" {
		t.Fatal("not kept")
	}
	waitLong(t, "its connector", func() bool {
		c := h.connectors.Get("examplenotes")
		return c != nil && connector.StatusOf(context.Background(), c).State == connector.StateOK
	})
	if got := run("", "list"); !strings.Contains(got, "example-notes") || !strings.Contains(got, "running") {
		t.Fatal(got)
	}
	if got := run("", "show", "example-notes"); strings.Contains(got, "s3cret") || !strings.Contains(got, `"set": true`) {
		t.Fatal(got)
	}
	run("", "disable", "example-notes")
	run("", "enable", "example-notes")
	run("", "uninstall", "example-notes")
	if got := run("", "list"); strings.Contains(got, "example-notes") || !strings.Contains(got, cuaPluginID) {
		t.Fatal("gone; the built-in ones stay:", got)
	}
}
