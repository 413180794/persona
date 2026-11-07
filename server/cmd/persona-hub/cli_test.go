package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The hub's command line, run as persona-hub (this test binary again, never with no command: that runs a hub): the
// version, the usage, and the mistakes, each with its exit code; `persona-hub hub <command>` is the same command.
func TestPersonaHubCommandLine(t *testing.T) {
	if args := os.Getenv("PERSONA_HUB_TEST_ARGS"); args != "" {
		os.Args = append([]string{"persona-hub"}, strings.Split(args, "\x1f")...)
		main()
		os.Exit(0)
	}
	home := t.TempDir()
	run := func(args ...string) (string, int) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestPersonaHubCommandLine$")
		cmd.Env = append(os.Environ(), "PERSONA_HUB_TEST_ARGS="+strings.Join(args, "\x1f"), "HOME="+home,
			"PERSONA_HOME="+filepath.Join(home, ".persona"))
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return string(out), code
	}
	for _, c := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"version", "-v"}, 0, "channel "},
		{[]string{"--help"}, 0, "persona-hub setup"},
		{[]string{"help"}, 0, "persona-hub setup"},
		{[]string{"run", "--listen", ":1"}, 2, "usage: persona-hub [run]"},
		{[]string{"hub", "run", "--listen", ":1"}, 2, "usage: persona-hub [run]"},
		{[]string{"slot", "x"}, 2, "persona-hub slot takes no arguments"},
		{[]string{"update-watch"}, 2, "usage: persona-hub update-watch <pending.json>"},
		{[]string{"bogus"}, 1, `unknown command "bogus"`},
		{[]string{"hub", "bogus"}, 1, `unknown command "bogus"`},
		{[]string{"assistant"}, 1, `unknown command "assistant"`}, // the manager's: persona-manager assistant
		{[]string{"tailcat-key"}, 1, `unknown command "tailcat-key"`},
	} {
		out, code := run(c.args...)
		if code != c.code || !strings.Contains(out, c.want) {
			t.Errorf("%q: exit %d, want %d and %q in %q", c.args, code, c.code, c.want, out)
		}
	}
}
