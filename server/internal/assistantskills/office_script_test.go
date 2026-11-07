package assistantskills

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The office skill's script runs office with whichever Persona program the computer has: personad first, else
// persona-node, the device's own (a device installed with install.sh --node has no personad).
func TestOfficeScriptFindsThePersonaProgram(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a POSIX sh script")
	}
	script, err := filepath.Abs(filepath.Join("builtin", "persona-office", "scripts", "office.sh"))
	if err != nil {
		t.Fatal(err)
	}
	run := func(home string) (string, int) {
		cmd := exec.Command("sh", script, "check", "x.docx")
		cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin", "PERSONA_BIN="}
		out, _ := cmd.CombinedOutput()
		return string(out), cmd.ProcessState.ExitCode()
	}
	home := t.TempDir()
	if out, code := run(home); code != 3 || !strings.Contains(out, "no personad or persona-node") {
		t.Fatalf("none: %d %q", code, out)
	}
	bin := filepath.Join(home, ".persona", "bin")
	_ = os.MkdirAll(bin, 0o755)
	for _, name := range []string{"persona-node", "personad"} {
		_ = os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\necho \""+name+" $*\"\n"), 0o755)
		want := name + " office check x.docx" // personad wins once it is there too
		if out, code := run(home); code != 0 || strings.TrimSpace(out) != want {
			t.Errorf("with %s: %d %q, want %q", name, code, out, want)
		}
	}
}
