package node

import (
	"strings"
	"testing"
	"time"
)

// Claude Code with background tasks (a stuck Bash moved to the background) waits in its input box and takes text,
// while Orca's terminal.agentStatus says isRunningAgent false. Our scan sees the claude in front of the pane's own
// pty: the message goes in (#5463: refused as not_delivered, with nothing logged).
func TestOrcaSendsWhileClaudeHasBackgroundTasks(t *testing.T) {
	withSubmitGrace(t, 50*time.Millisecond)
	n, o, _ := orcaSession(t)
	o.SetScreen("term_1", "❯ \n")
	o.SetAgentStatus("term_1", false, "") // Orca's answer while Claude has background tasks
	if _, err := request(t, n, "session.send", map[string]any{"session_id": sidA, "text": "hello"}); err != nil {
		code, data := refusalOf(t, err)
		t.Fatalf("refused: code=%s data=%v trace=%v", code, data, o.Trace())
	}
	if w := o.Writes(); len(w) == 0 || !strings.Contains(w[0].Text, "hello") {
		t.Fatalf("writes %+v", w)
	}
}

// The process trees as a Mac shows them (2026-11-06): an agent in an Orca pane sits under Orca's terminal host, which
// has no pane key; one inside a tmux started from that pane sits under the tmux server, which inherited the key.
const orcaPSTree = `
    1     0 ??
26485     1 ??
30044 26485 ttys001
30046 30044 ttys001
30058 30046 ttys001
30059 30058 ttys001
22651     1 ??
22656 22651 ttys004
22658 22656 ttys004
22675 22658 ttys004
40000 30059 ttys009
40001 40000 ttys009
`

// A pty made inside the pane (tmux, zellij, screen, script, a node-pty in an MCP server…) is not the pane: its
// processes inherited the pane key and are each "in front" of that pty while the pane may show a shell. Counted as
// the pane's, tmux's `claude -p` workers took the pane from the claude that started them (#5463); a message would go
// to the pane's shell as a command once claude had left it. Told by the pty's owner, not by a variable: Orca's own
// agent teams set TMUX in a real pane, and it stays the pane's.
func TestOrcaNestedPtysAreNotThePanes(t *testing.T) {
	tree := parsePSTree(orcaPSTree)
	if q := ptyOwner(tree, 30059); q != 26485 {
		t.Errorf("owner of the pane's claude: %d, want Orca's terminal host 26485", q)
	}
	if q := ptyOwner(tree, 22675); q != 22651 {
		t.Errorf("owner of tmux's claude: %d, want the tmux server 22651", q)
	}
	if q := ptyOwner(tree, 99999); q != 0 {
		t.Errorf("an unknown process has owner %d", q)
	}
	cands := parseOrcaPSArgs(strings.Join([]string{
		"30059 S+   04:00:00 claude --resume 3029711c",
		"22675 S+   00:05:00 claude -p do-triage-a",
		"40001 S+   00:01:00 claude", // under `script` run by the pane's claude: its own pty
	}, "\n"), time.Now())
	owners := map[int]int{}
	for pid := range cands {
		owners[pid] = ptyOwner(tree, pid)
	}
	env := strings.Join([]string{
		"30059 claude --resume 3029711c ORCA_PANE_KEY=tab1:leaf1 TMUX=/tmp/orca-claude-agent-teams/t1,0,1",
		"22675 claude -p do-triage-a TMUX=/private/tmp/tmux-501/default,22651,0 ORCA_PANE_KEY=tab1:leaf1",
		"40001 claude ORCA_PANE_KEY=tab1:leaf1",
		"26485 orca-terminal-host HOME=/Users/u",
		"22651 tmux HOME=/Users/u ORCA_PANE_KEY=tab1:leaf1",
		"40000 node pty-host.js HOME=/Users/u ORCA_PANE_KEY=tab1:leaf1",
	}, "\n")
	got := parseOrcaPSEnv(env, cands, owners)
	if ps := got["tab1:leaf1"]; len(ps) != 1 || ps[0].pid != 30059 {
		t.Fatalf("the pane's processes: %+v, want only the claude of the pane's own pty", ps)
	}
	// An owner whose environment ps cannot show (Apple's own binaries: /usr/bin/script, screen) may carry the key:
	// not taken. Here the pane's own owner is shown without its environment.
	noEnv := strings.Replace(env, "26485 orca-terminal-host HOME=/Users/u", "26485 /usr/bin/script", 1)
	if ps := parseOrcaPSEnv(noEnv, cands, owners)["tab1:leaf1"]; len(ps) != 0 {
		t.Errorf("taken with an owner whose environment is unknown: %+v", ps)
	}
	delete(owners, 30059) // its owner is unknown (the chain broke): not taken as the pane's
	if ps := parseOrcaPSEnv(env, cands, owners)["tab1:leaf1"]; len(ps) != 0 {
		t.Errorf("taken without a known owner: %+v", ps)
	}
}
