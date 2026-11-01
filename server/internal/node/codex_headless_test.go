package node

import (
	"path/filepath"
	"testing"
)

func TestCodexNonDefaultHeadlessSourceIsNotAnAgent(t *testing.T) {
	n := remoteTestNode(t)
	home := t.TempDir()
	id := n.registerCodex(home, remoteRaw, "")
	if id == remoteRaw {
		t.Fatal("test must use a non-default home")
	}
	n.state.mu.Lock()
	n.state.Sessions[remoteRaw] = sessionState{Cwd: filepath.Join(n.home, ".jarvis", "assistant")}
	n.state.mu.Unlock()
	if listed := n.codexSessions(map[string]bool{}); len(listed) != 0 {
		t.Fatalf("headless conversation duplicated into agents: %d", len(listed))
	}
}
