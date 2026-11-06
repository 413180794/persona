// Package plugins holds the plugins built and bundled with personad (docs/extensions.md, the extensions' sources):
// each is a plugin like any other, written against the Go SDK (sdk/go) and its own packages only.
package plugins_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

const modulePath = "github.com/nwverse/persona/server/"

// A plugin and its program depend on the SDK, the standard library, third-party modules and its own packages: no
// server/internal package, no other plugin. What it needs of the hub goes through the plugin protocol.
func TestPluginsDependOnlyOnTheSDK(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go tool:", err)
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	own := map[string]string{} // package to check → the plugin it may use
	for _, e := range entries {
		if e.IsDir() {
			own["plugins/"+e.Name()+"/..."] = "plugins/" + e.Name()
		}
	}
	// The commands that run a plugin (they import one): each may depend on its plugin, nothing else of the server's.
	out, err := exec.Command(goBin, "list", "-f", "{{.ImportPath}} {{join .Imports \" \"}}", "../cmd/...").Output()
	if err != nil {
		t.Fatalf("go list ../cmd/...: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		for _, imp := range fields[1:] {
			if rel, ok := strings.CutPrefix(imp, modulePath+"plugins/"); ok {
				own[strings.TrimPrefix(fields[0], modulePath)] = "plugins/" + strings.SplitN(rel, "/", 2)[0]
			}
		}
	}
	if _, ok := own["cmd/personad-wechat"]; !ok || len(own) < 2 {
		t.Fatalf("plugins and their programs not found: %v", own)
	}
	for pkg, plugin := range own {
		out, err := exec.Command(goBin, "list", "-deps", "-test", "-f", "{{.ImportPath}}", "../"+pkg).Output()
		if err != nil {
			msg := ""
			if ee, ok := err.(*exec.ExitError); ok {
				msg = string(ee.Stderr)
			}
			t.Fatalf("go list %s: %v %s", pkg, err, msg)
		}
		for _, dep := range strings.Fields(string(out)) {
			rel, ok := strings.CutPrefix(strings.TrimSuffix(dep, ".test"), modulePath)
			if !ok || rel == plugin || strings.HasPrefix(rel, plugin+"/") || rel == pkg || rel == "plugins" {
				continue
			}
			t.Errorf("%s depends on %s: a plugin uses only the SDK (sdk/go) and its own packages", pkg, rel)
		}
	}
}
