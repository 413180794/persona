package node

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
)

// A claude just started in a new tab has no sessions/<pid>.json for its first seconds and no transcript of its own until
// its first prompt. It used to be guessed as the newest transcript in its directory, an older session the tab then
// showed for those seconds (2026-11-04, a new Orca tab on Windows, #1980). Now a pane that appeared after node started
// is left unlisted until its claude says what it runs, for freshPaneGrace; then it is guessed as before.
func TestAJustStartedClaudeIsNotTakenForAnOlderSession(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	const (
		older = "0c000000-0000-4000-8000-000000000011" // the session that ran in this directory before
		fresh = "0c000000-0000-4000-8000-000000000012" // what the new claude runs, once it says so
		cwd   = "/work/repo"
	)
	pids := map[string]int{"w:p1": 5151, "w:p2": 5252, "w:p3": 5353}
	h, _ := methodHerdr(t, func(method string, params map[string]any) any {
		if method != "pane.process_info" {
			return map[string]any{}
		}
		return map[string]any{"process_info": map[string]any{"foreground_processes": []any{
			map[string]any{"pid": float64(pids[params["pane_id"].(string)]), "name": "claude", "argv0": "claude"}}}}
	})
	n, home := testNode(t)
	n.hosts, n.panePIDs = herdrHosts(h), map[string]panePID{}
	writeTranscript(t, home, cwd, older, time.Now().Add(-time.Minute), `{"type":"user","sessionId":"`+older+`"}`)
	writeSessionFile := func(pane, id string) {
		dir := filepath.Join(home, ".claude", "sessions")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		pid := strconv.Itoa(pids[pane])
		if err := os.WriteFile(filepath.Join(dir, pid+".json"), []byte(`{"pid":`+pid+`,"sessionId":"`+id+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	paneOf := func(id string) string {
		if a, ok := n.agents[id]; ok {
			return a.PaneID
		}
		return ""
	}

	n.setAgents(nil) // node's first look: no claude yet
	n.setAgents([]host.Agent{{Agent: "claude", PaneID: "w:p1", Cwd: cwd}})
	if p := paneOf(older); p != "" {
		t.Fatalf("the just started claude in %s was taken for the older session", p)
	}
	if len(n.agents) != 0 {
		t.Fatalf("listed %v before its claude said what it runs", n.agents)
	}

	// Its claude writes the file: the next refresh sees it (not panePIDRetry later).
	writeSessionFile("w:p1", fresh)
	n.setAgents([]host.Agent{{Agent: "claude", PaneID: "w:p1", Cwd: cwd}})
	if p := paneOf(fresh); p != "w:p1" {
		t.Fatalf("%s in %q, want w:p1", fresh, p)
	}

	// A claude that never writes the file (older Claude Code) is guessed once the grace is over.
	agents := []host.Agent{{Agent: "claude", PaneID: "w:p1", Cwd: cwd}, {Agent: "claude", PaneID: "w:p2", Cwd: cwd}}
	n.setAgents(agents)
	if p := paneOf(older); p != "" {
		t.Fatalf("w:p2 guessed within its grace: %s in %s", older, p)
	}
	n.mu.Lock()
	n.paneSeen["w:p2"] = time.Now().Add(-freshPaneGrace - time.Second)
	n.mu.Unlock()
	n.setAgents(agents)
	if p := paneOf(older); p != "w:p2" {
		t.Fatalf("after the grace %s in %q, want w:p2", older, p)
	}

	// A pane that goes (its claude exited) and comes back is fresh again.
	n.setAgents([]host.Agent{{Agent: "claude", PaneID: "w:p1", Cwd: cwd}})
	n.setAgents(agents)
	if p := paneOf(older); p != "" {
		t.Fatalf("w:p2 came back and was guessed at once: %s in %s", older, p)
	}
}

// Panes there when node starts are not fresh: after a restart a claude without a pid file (older Claude Code) is
// guessed at once, as before, not left out for freshPaneGrace.
func TestPanesThereWhenNodeStartsAreGuessedAtOnce(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	const (
		older = "0c000000-0000-4000-8000-000000000013"
		cwd   = "/work/repo"
	)
	h, _ := methodHerdr(t, func(method string, params map[string]any) any {
		return map[string]any{"process_info": map[string]any{"foreground_processes": []any{
			map[string]any{"pid": float64(6161), "name": "claude", "argv0": "claude"}}}}
	})
	n, home := testNode(t)
	n.hosts, n.panePIDs = herdrHosts(h), map[string]panePID{}
	writeTranscript(t, home, cwd, older, time.Now().Add(-time.Minute), `{"type":"user","sessionId":"`+older+`"}`)
	n.setAgents([]host.Agent{{Agent: "claude", PaneID: "w:p1", Cwd: cwd}})
	if a, ok := n.agents[older]; !ok || a.PaneID != "w:p1" {
		t.Fatalf("a pane there at start was not guessed: %v", n.agents)
	}
	if n.freshPane("w:p1") {
		t.Fatal("a pane there at start counts as fresh")
	}
}
