package node

import (
	"os"
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

// 2026-11-05: an evaluation run opened some sixty `codex exec` sessions, each in its own CODEX_HOME, each one verdict
// and done; they filled Agents as offline read-only rows over the agents at work. A rollout's session_meta says how
// its thread was opened: exec runs (and the subagents they spawn) are a program's, not the user's.
func TestCodexExecRunsAreNotListed(t *testing.T) {
	n := remoteTestNode(t)
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "2026", "11", "05")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		id, meta string
		listed   bool
	}{
		{"0e000000-0000-4000-8000-0000000000e1", `{"originator":"codex_exec","source":"exec","cwd":"/tmp/eval-a"}`, false},
		{"0e000000-0000-4000-8000-0000000000e2", `{"originator":"codex_sdk_ts","source":"exec","cwd":"/tmp/eval-b"}`, false},
		{"0e000000-0000-4000-8000-0000000000e3", `{"originator":"codex_exec","source":{"subagent":{"thread_spawn":{"parent_thread_id":"0e000000-0000-4000-8000-0000000000e1"}}}}`, false},
		{"0e000000-0000-4000-8000-0000000000e4", `{"originator":"codex_exec","source":{"subagent":"review"}}`, false},
		{"0e000000-0000-4000-8000-0000000000e5", `{"originator":"Codex Desktop","source":"exec"}`, false},
		{"0e000000-0000-4000-8000-0000000000c1", `{"originator":"codex-tui","source":"cli","cwd":"/work/mine"}`, true},
		{"0e000000-0000-4000-8000-0000000000c2", `{"originator":"Codex Desktop","source":"vscode","cwd":"/work/app"}`, true},
	}
	for _, c := range cases {
		p := filepath.Join(dir, "rollout-2026-11-05T01-00-00-"+c.id+".jsonl")
		if err := os.WriteFile(p, []byte(`{"type":"session_meta","payload":`+c.meta+"}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		n.registerCodex(home, c.id, p)
	}
	listed := map[string]bool{}
	for _, s := range n.codexSessions(map[string]bool{}) {
		listed[s.SourceThreadID] = true
	}
	for _, c := range cases {
		if listed[c.id] != c.listed {
			t.Errorf("%s listed=%v, want %v", c.meta, listed[c.id], c.listed)
		}
	}
}

// An exec run a shared app-server holds is drivable from the phone: it stays (#2131 hides only the history rows).
func TestCodexExecRunOnAnAppServerIsListed(t *testing.T) {
	n := remoteTestNode(t)
	home := t.TempDir()
	connectRemoteFixture(t, n, home, "one", remoteRaw)
	p := filepath.Join(home, "rollout-exec.jsonl")
	if err := os.WriteFile(p, []byte(`{"type":"session_meta","payload":{"originator":"codex_exec","source":"exec"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	id := n.registerCodex(home, remoteRaw, p)
	for _, s := range n.codexSessions(map[string]bool{}) {
		if s.SessionID == id {
			if s.SourceKind != "codex_app_server" {
				t.Fatalf("source = %s", s.SourceKind)
			}
			return
		}
	}
	t.Fatal("an app-server's exec thread was hidden")
}
