package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/update"
)

func TestCommandLine(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		terminal bool
		code     int
		done     bool
		stdout   string
		stderr   string
	}{
		{"hub pipe", nil, false, 0, false, "", ""},
		{"by hand in a terminal", nil, true, 2, true, "", "usage: personad-wechat"},
		{"version", []string{"version"}, true, 0, true, "dev", ""},
		{"version -v", []string{"version", "-v"}, false, 0, true, "channel ", ""},
		{"--help", []string{"--help"}, true, 0, true, "personad-wechat version -v", ""},
		{"-h", []string{"-h"}, false, 0, true, "usage: personad-wechat", ""},
		{"unknown", []string{"serve"}, false, 2, true, "", `unknown argument "serve"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code, done := commandLine("personad-wechat", c.args, c.terminal, &stdout, &stderr)
			if code != c.code || done != c.done {
				t.Fatalf("got (%d, %v), want (%d, %v)", code, done, c.code, c.done)
			}
			if !strings.Contains(stdout.String(), c.stdout) || (c.stdout == "" && stdout.Len() > 0) {
				t.Errorf("stdout = %q, want %q", stdout.String(), c.stdout)
			}
			if !strings.Contains(stderr.String(), c.stderr) || (c.stderr == "" && stderr.Len() > 0) {
				t.Errorf("stderr = %q, want %q", stderr.String(), c.stderr)
			}
		})
	}
}

// The hub's build check (`version -v`) reads this exact output.
func TestVersionOutput(t *testing.T) {
	var stdout bytes.Buffer
	commandLine("personad-wechat", []string{"version", "-v"}, false, &stdout, &bytes.Buffer{})
	b := update.Make(version, channel, build, updateURL)
	want := fmt.Sprintf("%s\nchannel %s, build %d\n", version, b.Channel, b.Number)
	if stdout.String() != want {
		t.Fatalf("got %q, want %q", stdout.String(), want)
	}
}

// The compat name prints its own name, not the other one.
func TestCommandLineUsesItsName(t *testing.T) {
	var stdout bytes.Buffer
	commandLine("jarvisd-wechat", []string{"--help"}, false, &stdout, &bytes.Buffer{})
	if !strings.Contains(stdout.String(), "jarvisd-wechat version -v") || strings.Contains(stdout.String(), "personad-wechat") {
		t.Fatalf("usage = %q", stdout.String())
	}
}
