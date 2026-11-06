package node

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/host"
)

// A directory is only a fallback signal: a real host pane can run from the same directory as a headless assistant.
func TestAssistantClassificationRespectsHostPane(t *testing.T) {
	for _, tc := range []struct {
		name       string
		pane       string
		process    bool
		transcript bool
		want       bool
	}{
		{name: "live pane with assistant state", pane: "w1:p1"},
		{name: "live pane with assistant transcript", pane: "w1:p1", transcript: true},
		{name: "headless state", want: true},
		{name: "headless transcript", transcript: true, want: true},
		{name: "empty pane does not override state", want: true},
		{name: "owned headless process wins over stale pane", pane: "w1:p1", process: true, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, home := testNode(t)
			id := "11111111-2222-4333-8444-555555555555"
			n.agents[id] = host.Agent{PaneID: tc.pane}
			if tc.transcript {
				p := filepath.Join(home, ".claude", "projects", "-synthetic-persona-assistant", id+".jsonl")
				if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				n.paths[id] = p
			} else {
				n.state.put(id, sessionState{Cwd: filepath.Join(home, ".persona", "assistant")})
			}
			if tc.process {
				n.procs[id] = &assistantProc{}
			}
			if got := n.isAssistantSession(id); got != tc.want {
				t.Fatalf("isAssistantSession = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTerminalWatchStillRejectsHeadlessAssistant(t *testing.T) {
	n, home := testNode(t)
	n.state.put("headless", sessionState{Cwd: filepath.Join(home, ".persona", "assistant")})
	if _, err := n.terminalWatch(context.Background(), "headless", nil); err == nil || !strings.Contains(err.Error(), "has no terminal") {
		t.Fatalf("watch headless assistant: %v", err)
	}
}
