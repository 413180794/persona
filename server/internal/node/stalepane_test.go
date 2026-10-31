package node

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/herdr"
)

// The panes of sessions Jarvis started are kept in the hub's database and come back when node restarts. After a
// redeploy (2026-10-31) two panes showed sessions they had run days earlier: one pane's claude had since been
// replaced, the other had resumed another session, and the remembered binding won over what the claude in the pane
// said in its sessions/<pid>.json. The process wins; the binding is for a claude that has not written its file yet.
func TestAPaneShowsWhatItsClaudeRunsNotWhatItOnceRan(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	const (
		before = "0c000000-0000-4000-8000-000000000001" // started by Jarvis in w:p1 days ago
		now    = "0c000000-0000-4000-8000-000000000002" // what w:p1's claude runs today
		fresh  = "0c000000-0000-4000-8000-000000000003" // just started in w:p2, no pid file yet
	)
	pids := map[string]float64{"w:p1": 4242, "w:p2": 4343}
	h, _ := methodHerdr(t, func(method string, params map[string]any) any {
		if method != "pane.process_info" {
			return map[string]any{}
		}
		return map[string]any{"process_info": map[string]any{"foreground_processes": []any{
			map[string]any{"pid": pids[params["pane_id"].(string)], "name": "claude", "argv0": "claude"}}}}
	})
	n, home := testNode(t)
	n.herdr, n.panes, n.panePIDs = h, map[string]string{"w:p1": before, "w:p2": fresh}, map[string]panePID{}
	dir := filepath.Join(home, ".claude", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "4242.json"), []byte(`{"pid":4242,"sessionId":"`+now+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	n.setAgents([]herdr.Agent{
		{Agent: "claude", PaneID: "w:p1", Cwd: "/work/repo"},
		{Agent: "claude", PaneID: "w:p2", Cwd: "/work/repo"},
	})
	if a, ok := n.agents[now]; !ok || a.PaneID != "w:p1" {
		t.Errorf("w:p1 runs %s, listed %v", now, n.agents)
	}
	if _, ok := n.agents[before]; ok {
		t.Errorf("w:p1 still listed with the session it ran before")
	}
	if a, ok := n.agents[fresh]; !ok || a.PaneID != "w:p2" {
		t.Errorf("a just started claude keeps its binding until its file is written: %v", n.agents)
	}
}

// Right after node starts, a claude's config directory may not be known yet and its pid file is not found. That
// answer was kept for a minute, the stale binding showing meanwhile (seen for a minute or two after a redeploy).
func TestAPidFileNotFoundIsLookedForAgainSoon(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	const id = "0c000000-0000-4000-8000-000000000004"
	h, _ := methodHerdr(t, func(method string, params map[string]any) any {
		return map[string]any{"process_info": map[string]any{"foreground_processes": []any{
			map[string]any{"pid": 4444, "name": "claude", "argv0": "claude"}}}}
	})
	n, home := testNode(t)
	n.herdr, n.panePIDs = h, map[string]panePID{}
	if got := n.sessionFromProcess("w:p1"); got != "" {
		t.Fatalf("no pid file yet, got %q", got)
	}
	dir := filepath.Join(home, ".claude", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "4444.json"), []byte(`{"sessionId":"`+id+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	n.panePIDs["w:p1"] = panePID{checked: time.Now().Add(-panePIDRetry - time.Second)}
	if got := n.sessionFromProcess("w:p1"); got != id {
		t.Fatalf("looked again after %v: %q, want %q", panePIDRetry, got, id)
	}
}
