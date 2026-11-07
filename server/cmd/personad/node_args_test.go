package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNodeArgsEndToEnd runs main in a child process (this test binary, re-executed): -h prints the node usage and
// exits 0; extra arguments after reset-uid or tailcat addr are refused with exit 2 and change nothing.
func TestNodeArgsEndToEnd(t *testing.T) {
	if args := os.Getenv("PERSONAD_TEST_MAIN_ARGS"); args != "" {
		os.Args = append([]string{"personad"}, strings.Split(args, "\x1f")...)
		main()
		os.Exit(0)
	}
	home := t.TempDir()
	run := func(args ...string) (string, string, int) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestNodeArgsEndToEnd$")
		cmd.Env = append(os.Environ(), "PERSONAD_TEST_MAIN_ARGS="+strings.Join(args, "\x1f"), "HOME="+home,
			"PERSONA_HOME="+filepath.Join(home, ".persona"))
		var stdout, stderr strings.Builder
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return stdout.String(), stderr.String(), code
	}
	for _, h := range []string{"-h", "--help"} {
		out, errOut, code := run("node", h)
		if code != 0 || !strings.Contains(out, "personad node reset-uid") || strings.Contains(errOut, "不认识") {
			t.Errorf("node %s: exit %d, stdout %q, stderr %q", h, code, out, errOut)
		}
	}
	for _, args := range [][]string{{"node", "reset-uid", "--dry-run"}, {"node", "tailcat", "addr", "x"},
		{"node", "tailcat", "init", "--hub", "k", "extra"}, {"node", "-h", "x"}, {"node", "tailcat"}, {"node", "tailcat", "adr"}} {
		out, errOut, code := run(args...)
		if code != 2 || out != "" || !strings.Contains(errOut, "personad node --stdio") {
			t.Errorf("%q: exit %d, stdout %q, stderr %q", args, code, out, errOut)
		}
	}
	// Scripts tell personad from persona-node by node-update-check's program before they replace or run one.
	if out, _, code := run("node-update-check"); code != 0 || !strings.Contains(out, `"program":"personad"`) {
		t.Errorf("node-update-check: exit %d, stdout %q", code, out)
	}
	entries, _ := os.ReadDir(home)
	for _, e := range entries {
		if e.Name() != ".cache" && e.Name() != ".config" {
			t.Errorf("a refused command wrote %s in HOME", e.Name())
		}
	}
}
