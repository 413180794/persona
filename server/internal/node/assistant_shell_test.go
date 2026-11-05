package node

import (
	"bytes"
	"io"
	"log"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The shell the assistant's launcher runs in is the login shell's, never a bare bash on Windows: there it is WSL, and a
// claude handed to WSL exited at once (#1994).
func TestInteractiveShellIsTheLoginShell(t *testing.T) {
	sh, err := interactiveShell()
	if runtime.GOOS != "windows" {
		if err != nil || (sh != "zsh" && sh != "bash") {
			t.Fatalf("%q %v", sh, err)
		}
		if _, zerr := exec.LookPath("zsh"); zerr == nil && sh != "zsh" {
			t.Fatalf("%q with zsh installed", sh)
		}
		return
	}
	if loginShellProgram() == "" { // Git Bash not found
		if err == nil || !strings.Contains(err.Error(), "Git Bash") {
			t.Fatalf("no Git Bash: %q %v", sh, err)
		}
		return
	}
	if err != nil || !strings.EqualFold(filepath.Base(sh), "bash.exe") || strings.Contains(strings.ToLower(sh), `\system32\`) {
		t.Fatalf("%q %v", sh, err)
	}
}

func TestAnAssistantThatDoesNotStartSaysWhy(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	const id = "0c000000-0000-4000-8000-000000000021"
	lines := "Welcome\nopen terminal failed: not a terminal\n"
	watchAssistantStderr(logger, &assistantProc{id: id, launcher: "claude", born: time.Now()}, io.NopCloser(strings.NewReader(lines)))
	if out := buf.String(); !strings.Contains(out, "assistant 0c000000: claude exited") || !strings.Contains(out, "Welcome | open terminal failed: not a terminal") {
		t.Fatalf("log: %q", out)
	}

	// One that ran for a while and ended (stopped, idle) says nothing but its error lines.
	buf.Reset()
	watchAssistantStderr(logger, &assistantProc{id: id, launcher: "claude", born: time.Now().Add(-time.Hour)}, io.NopCloser(strings.NewReader("bye\nAPI Error: overloaded\n")))
	if out := buf.String(); out != "assistant 0c000000: API Error: overloaded\n" {
		t.Fatalf("log: %q", out)
	}

	// And one that exits early without a word adds nothing.
	buf.Reset()
	watchAssistantStderr(logger, &assistantProc{id: id, launcher: "claude", born: time.Now()}, io.NopCloser(strings.NewReader("")))
	if buf.Len() != 0 {
		t.Fatalf("log: %q", buf.String())
	}
}
