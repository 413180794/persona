//go:build !windows

package node

import (
	"errors"
	"os"
	"syscall"
	"testing"
	"time"
)

// The claude that wrote sessions/<pid>.json exited and its pid went to another user's process (here init, pid 1):
// that is not a live claude, so a pane whose agent herdr no longer reports is not kept as the session's (#2141; it
// was kept because kill(pid, 0) gives EPERM for another user's process and that counted as alive).
func TestHerdrCoreExitedAgentWhosePidAnotherUserHasIsNotKept(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root may signal every process: no other user's process to tell apart")
	}
	if err := syscall.Kill(1, 0); !errors.Is(err, syscall.EPERM) {
		t.Skipf("init is not another user's process here: %v", err)
	}
	n, _, hc, home := hcNode(t)
	writeTranscript(t, home, hcCwd, sidA, time.Now().Add(-time.Minute), `{"type":"custom-title","customTitle":"x"}`)
	writeClaudePID(t, home, 1, sidA)
	hc.setTerminals(hcTerminal("t1", "claude-code", "idle", 1, "claude", hcCwd, "", "", ""))
	if m := byID(listSessions(t, n)); m[sidA].PaneID != "herdr-core:t1" {
		t.Fatalf("not listed: %v", m)
	}
	exited := hcTerminal("t1", "claude-code", "done", 1, "claude", hcCwd, "", "", "")
	exited["foreground"] = map[string]any{"pid": 4241, "name": "zsh", "argv": []string{"-zsh"}, "cwd": hcCwd}
	hc.setTerminals(exited)
	if m := byID(listSessions(t, n)); m[sidA].PaneID != "" {
		t.Fatalf("an exited agent whose pid another user's process has is still listed live: %+v", m[sidA])
	}
}
