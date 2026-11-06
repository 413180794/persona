package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const modulePath = "github.com/nwverse/persona/server/"

// The device's program links the node and its clients, never the hub, the manager or the database, on any platform it
// ships for (the self-contained packaging plan, step 4).
func TestPersonaNodeDoesNotLinkTheHubOrTheManager(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go tool:", err)
	}
	// The hub's and the manager's own packages, and their sources and channels (they run in the manager).
	forbidden := []string{"internal/hub", "internal/hubedge", "internal/assistant", "internal/manager", "internal/devices",
		"internal/extmgr", "internal/db", "internal/relay", "internal/voice", "internal/mail", "internal/lark",
		"internal/feishubot", "internal/wechat", "internal/apns", "internal/bus", "internal/workspaceboard",
		"internal/usagesource", "internal/admin"}
	// No database driver either, wherever it comes from.
	drivers := []string{"github.com/jackc/pgx", "github.com/lib/pq", "database/sql/driver"}
	for _, target := range []string{"linux/amd64", "linux/arm64", "darwin/arm64", "darwin/amd64", "windows/amd64"} {
		goos, goarch, _ := strings.Cut(target, "/")
		cmd := exec.Command(goBin, "list", "-deps", "-f", "{{.ImportPath}}", ".")
		cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list -deps (%s): %v", target, err)
		}
		for _, dep := range strings.Fields(string(out)) {
			for _, d := range drivers {
				if dep == d || strings.HasPrefix(dep, d+"/") {
					t.Errorf("persona-node (%s) links the database driver %s", target, dep)
				}
			}
			rel, ok := strings.CutPrefix(dep, modulePath)
			if !ok {
				continue
			}
			for _, f := range forbidden {
				if rel == f || strings.HasPrefix(rel, f+"/") {
					t.Errorf("persona-node (%s) depends on %s: the device's program reaches the manager only over the node "+
						"protocol; move what it needs into internal/node, internal/nodecmd or a contract package", target, dep)
				}
			}
		}
	}
}

// main in a child process (this test binary, re-executed): what persona-node answers and its exit codes.
func TestPersonaNodeCommandLine(t *testing.T) {
	if args := os.Getenv("PERSONA_NODE_TEST_ARGS"); args != "" {
		os.Args = append([]string{"persona-node"}, strings.Split(args, "\x1f")...)
		main()
		os.Exit(0)
	}
	home := t.TempDir()
	run := func(args ...string) (string, int) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestPersonaNodeCommandLine$")
		cmd.Env = append(os.Environ(), "PERSONA_NODE_TEST_ARGS="+strings.Join(args, "\x1f"), "HOME="+home,
			"PERSONA_HOME="+filepath.Join(home, ".persona"))
		out, _ := cmd.CombinedOutput()
		return string(out), cmd.ProcessState.ExitCode()
	}
	if out, code := run("version"); code != 0 || !strings.HasPrefix(out, "dev") {
		t.Fatalf("version: %d %q", code, out)
	}
	if out, code := run("version", "-v"); code != 0 || !strings.Contains(out, "channel dev, build 0") {
		t.Fatalf("version -v: %d %q", code, out)
	}
	if out, code := run("node", "-h"); code != 0 || !strings.Contains(out, "persona-node node --attach") {
		t.Fatalf("node -h: %d %q", code, out)
	}
	for _, args := range [][]string{{"hub"}, {"manager", "run"}, {"assistant"}, {"extension", "list"}} {
		if out, code := run(args...); code != 2 || !strings.Contains(out, "usage:") {
			t.Errorf("%v is not the device's: %d %q", args, code, out)
		}
	}
	if out, code := run("sysmon", "x"); code != 2 || !strings.Contains(out, "persona-node sysmon") {
		t.Fatalf("sysmon x: %d %q", code, out)
	}
	if out, code := run("hook"); code != 0 || out != "" { // empty stdin: nothing to say, as personad hook
		t.Fatalf("hook: %d %q", code, out)
	}
	if out, code := run("node-update-check"); code != 0 || !strings.Contains(out, `"version":"dev"`) {
		t.Fatalf("node-update-check: %d %q", code, out)
	}
}
