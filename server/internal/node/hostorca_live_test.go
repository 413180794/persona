//go:build orcalive

package node

// A read-only live check against the user's running Orca (go test -tags orcalive -run OrcaLive ./internal/node/ -v):
// it lists Orca's sessions the way a node with engine orca does, through a read-only client (status.get,
// terminal.list, worktree.ps, terminal.agentStatus only; anything else is refused before it is sent), and prints pane keys, agents, states,
// pids and session ids — never screen text or tokens. Each session id is checked against the claude's own
// sessions/<pid>.json.

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/orcart"
)

func TestOrcaLiveReadOnly(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	meta := os.Getenv("ORCA_LIVE_META") // TestMain points the default path away from the real Orca
	if meta == "" {
		meta = filepath.Join(home, "Library", "Application Support", "orca", "orca-runtime.json")
	}
	if _, err := os.Stat(meta); err != nil {
		t.Skip("Orca not running")
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	c := &orcart.Client{MetaPath: meta, ReadOnly: true, Log: t.Logf}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	st, err := c.Probe(ctx)
	if err != nil {
		t.Fatalf("status.get: %v", err)
	}
	t.Logf("Orca %s, runtime protocol %d (client floor %d)", st.AppVersion, st.RuntimeProtocolVersion,
		st.MinCompatibleRuntimeClientVersion)
	h := newOrcaHost(c)
	n := &Node{home: home, paths: map[string]string{}, agents: map[string]host.Agent{}, panes: map[string]string{},
		panePIDs: map[string]panePID{}, state: newState(), cdirs: newClaudeDirs()}
	n.hosts = host.NewRegistry(h)
	start := time.Now()
	if err := n.refreshHosts(ctx); err != nil {
		t.Fatal(err)
	}
	t.Logf("refresh took %s", time.Since(start).Round(time.Millisecond))
	procs, err := h.agentProcs(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	n.mu.Lock()
	var lines []string
	for id, a := range n.agents {
		ref := host.ParsePane(a.PaneID)
		pid, fileID := 0, ""
		if ps := procs[ref.ID]; len(ps) > 0 {
			pid = ps[0].pid
			fileID = readClaudeSessionID(filepath.Join(home, ".claude", "sessions", strconv.Itoa(pid)+".json"))
		}
		check := "ok"
		if a.Agent == "claude" && fileID != id {
			check = "MISMATCH sessions/<pid>.json=" + fileID
		}
		lines = append(lines, a.PaneID+"  "+a.Agent+"  "+a.AgentStatus+"  pid "+strconv.Itoa(pid)+"  "+id+"  "+check)
	}
	n.mu.Unlock()
	sort.Strings(lines)
	for _, l := range lines {
		t.Log(l)
	}
	snap, err := h.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d terminals, %d agent panes, %d worktrees, %d tabs", len(snap.Panes), len(snap.Agents), len(snap.Spaces),
		len(snap.Slots))
	for _, p := range snap.Panes {
		if p.Agent == "" {
			t.Logf("not an agent: %s", p.PaneID)
		}
	}
}
