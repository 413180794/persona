package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// writeClaudeProc writes sessions/<pid>.json as a running claude does, with the given fields.
func writeClaudeProc(t *testing.T, home string, pid int, fields map[string]any) {
	t.Helper()
	dir := filepath.Join(home, ".claude", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	fields["pid"] = pid
	b, _ := json.Marshal(fields)
	if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(pid)+".json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// bgTranscript writes a Claude transcript with a title and one exchange, padded past what the offline list skips.
func bgTranscript(t *testing.T, home, cwd, id, title string) string {
	t.Helper()
	return writeTranscript(t, home, cwd, id, time.Now(),
		`{"type":"ai-title","aiTitle":"`+title+`","sessionId":"`+id+`"}`,
		`{"type":"user","uuid":"u1","timestamp":"2026-11-04T09:00:00Z","cwd":"`+cwd+`","sessionId":"`+id+`","message":{"role":"user","content":"deploy it"}}`,
		`{"type":"assistant","uuid":"a1","timestamp":"2026-11-04T09:00:05Z","sessionId":"`+id+`","message":{"role":"assistant","content":[{"type":"text","text":"on it"}]}}`,
		strings.Repeat(" ", 4096))
}

// A /fork in Claude Code 2.1.30x: the fork runs in Claude's daemon (sessions/<pid>.json kind "bg"), no pane; the
// interactive claude in the pane parked its own session and shows the agents view, while herdr still names the pane
// after that session and calls it working off the fork's terminal title. The fork lists live, read-only, with its
// subagents and its status from the registry; the parked session does not take the pane (offline) until its claude
// comes back to it. A background session whose claude exited is not live.
func TestClaudeBackgroundSessionListedLive(t *testing.T) {
	n, f, home := herdrNode(t)
	cwd := home
	const dead = "0d000000-0000-4000-8000-00000000000d"
	bgTranscript(t, home, cwd, sidA, "deploy")
	fork := bgTranscript(t, home, cwd, sidB, "deploy ⑂ try it")
	bgTranscript(t, home, cwd, dead, "gone")
	sub := filepath.Join(strings.TrimSuffix(fork, ".jsonl"), "subagents")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(sub, "agent-a1.jsonl"), []byte(`{"type":"assistant","timestamp":"2026-11-04T09:01:00Z","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"looking"}]}}`+"\n"), 0o600)
	_ = os.WriteFile(filepath.Join(sub, "agent-a1.meta.json"), []byte(`{"agentType":"Explore","description":"look around","toolUseId":"tu1"}`), 0o600)

	parent := map[string]any{"sessionId": sidA, "cwd": cwd, "kind": "interactive", "entrypoint": "cli", "status": "idle", "parkedJobId": "0d0000aa"}
	writeClaudeProc(t, home, os.Getppid(), parent)
	bg := func(status string) {
		writeClaudeProc(t, home, os.Getpid(), map[string]any{"sessionId": sidB, "cwd": cwd, "kind": "bg", "entrypoint": "cli",
			"status": status, "name": "deploy ⑂ try it"})
	}
	bg("busy")
	writeClaudeProc(t, home, 999999, map[string]any{"sessionId": dead, "cwd": cwd, "kind": "bg", "status": "busy"}) // no such process
	f.reply("session.snapshot", snapshot(nil, nil, []any{
		claudeAgent(sidA, proto.StatusWorking, "w1:p1", "w1:t1", "w1", cwd, "deploy ⑂ try it")}, nil))

	m := byID(listSessions(t, n))
	b := m[sidB]
	if b.Status != proto.StatusWorking || b.SourceKind != sourceClaudeBg || b.PaneID != "" || b.Title != "deploy ⑂ try it" ||
		b.Agent != "claude" || b.Cwd != cwd || b.ReadOnlyReason == "" || b.LastText != "on it" {
		t.Errorf("fork = %+v", b)
	}
	if c := b.Capabilities; c == nil || !c.Read || c.Send || c.Interrupt || c.Approve || c.Terminal || c.Attach {
		t.Errorf("fork capabilities = %+v", c)
	}
	if len(b.Subagents) != 1 || b.Subagents[0].SessionID != sidB+"@a1" || !b.Subagents[0].Running {
		t.Errorf("fork subagents = %+v", b.Subagents)
	}
	if a := m[sidA]; a.Status != proto.StatusOffline || a.PaneID != "" {
		t.Errorf("parked parent = %+v", a)
	}
	if s, ok := m[dead]; ok && s.Status != proto.StatusOffline {
		t.Errorf("exited background session = %+v", s)
	}

	for status, want := range map[string]string{"idle": proto.StatusIdle, "waiting": proto.StatusBlocked, "later": proto.StatusUnknown} {
		bg(status)
		if got := byID(listSessions(t, n))[sidB].Status; got != want {
			t.Errorf("registry status %s: listed %s, want %s", status, got, want)
		}
	}

	// The claude comes back to its own session: the pane is that session's again.
	delete(parent, "parkedJobId")
	writeClaudeProc(t, home, os.Getppid(), parent)
	if a := byID(listSessions(t, n))[sidA]; a.Status != proto.StatusWorking || a.PaneID != "w1:p1" {
		t.Errorf("parent back in its pane = %+v", a)
	}
}

// Nothing is typed for a background session: a send is refused before anything starts (the claude running it has it
// open), and herdr is never asked to make a tab.
func TestClaudeBackgroundSessionSendRefused(t *testing.T) {
	n, f, home := herdrNode(t)
	bgTranscript(t, home, home, sidB, "fork")
	writeClaudeProc(t, home, os.Getpid(), map[string]any{"sessionId": sidB, "cwd": home, "kind": "bg", "entrypoint": "cli", "status": "idle"})
	f.reset()
	_, err := request(t, n, "session.send", map[string]any{"session_id": sidB, "text": "still there?", "client_msg_id": "m-1"})
	if !isRefusal(err, openElsewhere) {
		t.Fatalf("send to a background session: %v", err)
	}
	for _, c := range f.trace() {
		if strings.Contains(c, "workspace") || strings.Contains(c, "tab.create") || strings.Contains(c, "pane.") {
			t.Fatalf("herdr was asked to start something: %v", f.trace())
		}
	}
}
