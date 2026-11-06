package node

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
)

// herdr 0.9.3 said done through a whole Claude turn started from the phone (#2069): the app showed it finished while
// it searched code and its transcript grew. A Claude the host calls done, idle or unknown is working while its title
// shows Claude Code's spinner or its transcript has an unfinished turn written to lately (its newest timed line, not
// its mtime: #5967); a finished turn (its turn_duration, an interrupt) keeps the host's word, and so do blocked and
// other agents.
func TestClaudeStatusOverTheHost(t *testing.T) {
	dir := t.TempDir()
	// {{T}}: the case's time the lines were written
	const (
		prompt    = `{"type":"user","message":{"role":"user","content":"look for the bug"},"uuid":"u1","timestamp":"{{T}}"}`
		toolUse   = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Grep","input":{"pattern":"x"}}]},"uuid":"a1","parentUuid":"u1","timestamp":"{{T}}"}`
		reply     = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"found it"}]},"uuid":"a2","parentUuid":"a1","timestamp":"{{T}}"}`
		duration  = `{"type":"system","subtype":"turn_duration","uuid":"d1","parentUuid":"a2","durationMs":2000,"timestamp":"{{T}}"}`
		interrupt = `{"type":"user","message":{"role":"user","content":"[Request interrupted by user]"},"uuid":"u2","timestamp":"{{T}}"}`
		// Claude Code's metadata, rewritten in place while it is idle: no time of its own
		title = `{"type":"ai-title","aiTitle":"fix the bug","sessionId":"x"}`
	)
	n := &Node{paths: map[string]string{}, metas: map[string]quickMeta{}, bgScans: map[string]*bgScan{}}
	now := time.Now()
	for i, tc := range []struct {
		name   string
		agent  string
		status string
		title  string
		lines  []string // nil: no transcript
		age    time.Duration
		want   string
	}{
		{"a turn from the phone, herdr still on done", "claude", proto.StatusDone, "✳ fix", []string{duration, prompt, toolUse}, 2 * time.Second, proto.StatusWorking},
		{"idle", "claude", proto.StatusIdle, "✳ fix", []string{prompt, toolUse}, 2 * time.Second, proto.StatusWorking},
		{"unknown", "claude", proto.StatusUnknown, "", []string{prompt}, time.Second, proto.StatusWorking},
		{"the turn finished", "claude", proto.StatusDone, "✳ fix", []string{prompt, toolUse, reply, duration}, 2 * time.Second, proto.StatusDone},
		{"interrupted", "claude", proto.StatusDone, "✳ fix", []string{prompt, toolUse, interrupt}, 2 * time.Second, proto.StatusDone},
		{"an open turn not written to for minutes", "claude", proto.StatusDone, "✳ fix", []string{prompt, toolUse}, 5 * time.Minute, proto.StatusDone},
		{"a long tool call: the spinner in the title", "claude", proto.StatusDone, "◐ fix", []string{prompt, toolUse}, 5 * time.Minute, proto.StatusWorking},
		{"the spinner's other quarters", "claude", proto.StatusDone, "◒ fix", nil, 0, proto.StatusWorking},
		{"an older Claude's braille spinner", "claude", proto.StatusIdle, "⠸ fix", nil, 0, proto.StatusWorking},
		{"the title read just before the turn ended", "claude", proto.StatusDone, "◑ fix", []string{prompt, reply, duration}, time.Second, proto.StatusDone},
		{"no signs", "claude", proto.StatusDone, "✳ fix", nil, 0, proto.StatusDone},
		{"blocked stays", "claude", proto.StatusBlocked, "◐ fix", []string{prompt, toolUse}, time.Second, proto.StatusBlocked},
		{"working stays", "claude", proto.StatusWorking, "✳ fix", []string{prompt, reply, duration}, time.Second, proto.StatusWorking},
		{"not a Claude", "codex", proto.StatusDone, "◐ fix", nil, 0, proto.StatusDone},
	} {
		id := fmt.Sprintf("%08d-0000-4000-8000-000000000000", i)
		if tc.lines != nil {
			p := filepath.Join(dir, id+".jsonl")
			body := strings.ReplaceAll(strings.Join(tc.lines, "\n")+"\n", "{{T}}", now.Add(-tc.age).UTC().Format(time.RFC3339Nano))
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(p, now.Add(-tc.age), now.Add(-tc.age)); err != nil {
				t.Fatal(err)
			}
			n.paths[id] = p
		}
		a := host.Agent{Agent: tc.agent, AgentStatus: tc.status, RawTitle: tc.title}
		if got := n.claudeStatus(id, a, now); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
	// An open turn whose lines are minutes old, the file written a second ago: only its metadata was rewritten.
	id := "00000090-0000-4000-8000-000000000000"
	p := filepath.Join(dir, id+".jsonl")
	body := strings.ReplaceAll(strings.Join([]string{prompt, toolUse, title}, "\n")+"\n", "{{T}}", now.Add(-5*time.Minute).UTC().Format(time.RFC3339Nano))
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, now.Add(-time.Second), now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	n.paths[id] = p
	if got := n.claudeStatus(id, host.Agent{Agent: "claude", AgentStatus: proto.StatusDone, RawTitle: "✳ fix"}, now); got != proto.StatusDone {
		t.Errorf("an open turn whose metadata alone was just rewritten: %s, want done", got)
	}
}

// The same correction reaches everything that asks for the live agent (the status line, closing it, updating the
// node), not only the list.
func TestAgentCarriesClaudeStatus(t *testing.T) {
	id := "00000099-0000-4000-8000-000000000000"
	n := &Node{paths: map[string]string{}, metas: map[string]quickMeta{}, bgScans: map[string]*bgScan{},
		agents: map[string]host.Agent{id: {Agent: "claude", AgentStatus: proto.StatusDone, RawTitle: "◓ fix"}}}
	if a, ok := n.agent(id); !ok || a.AgentStatus != proto.StatusWorking {
		t.Fatalf("agent: %+v %v, want working", a, ok)
	}
}

// Claude Code compacts on its own while idle (2.1.31x, "Compacted while idle"). A session whose transcript ended so
// showed as working for hours (#5967): the quick tail read took the 17 KB compaction summary for a long prompt, a turn
// that never finished, and Claude Code's in-place metadata rewrites kept the file fresh. A synthetic transcript of the
// same shape: a finished turn, a local /copy, an away summary, the idle compaction, the metadata rewritten just now.
func TestIdleCompactionIsNoTurn(t *testing.T) {
	now := time.Now().UTC()
	at := func(ago time.Duration) string { return now.Add(-ago).Format(time.RFC3339Nano) }
	summary := "This session is being continued from a previous conversation that ran out of context. " + strings.Repeat("Earlier work on the example project. ", 500)
	lines := []string{
		`{"type":"user","uuid":"u1","message":{"role":"user","content":"tidy the example module"},"timestamp":"` + at(3*time.Hour) + `"}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/src/example.go"}}]},"timestamp":"` + at(3*time.Hour-time.Second) + `"}`,
		`{"type":"user","uuid":"r1","parentUuid":"a1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"package example"}]},"timestamp":"` + at(3*time.Hour-2*time.Second) + `"}`,
		`{"type":"assistant","uuid":"a2","parentUuid":"r1","message":{"role":"assistant","content":[{"type":"text","text":"Tidied it."}],"stop_reason":"end_turn"},"timestamp":"` + at(3*time.Hour-3*time.Second) + `"}`,
		`{"type":"system","subtype":"turn_duration","uuid":"d1","parentUuid":"a2","durationMs":3000,"timestamp":"` + at(3*time.Hour-4*time.Second) + `"}`,
		// a local command: /copy, no turn
		`{"type":"user","uuid":"c1","isMeta":true,"message":{"role":"user","content":"<local-command-caveat>Caveat: the messages below were generated by the user while running local commands.</local-command-caveat>"},"timestamp":"` + at(2*time.Hour+50*time.Minute) + `"}`,
		`{"type":"user","uuid":"c2","message":{"role":"user","content":"<command-name>/copy</command-name>\n<command-message>copy</command-message>\n<command-args></command-args>"},"timestamp":"` + at(2*time.Hour+50*time.Minute) + `"}`,
		`{"type":"user","uuid":"c3","message":{"role":"user","content":"<local-command-stdout>Copied to clipboard</local-command-stdout>"},"timestamp":"` + at(2*time.Hour+50*time.Minute) + `"}`,
		`{"type":"system","subtype":"away_summary","uuid":"s1","content":"You tidied the example module.","timestamp":"` + at(2*time.Hour+45*time.Minute) + `"}`,
		// the idle compaction
		`{"type":"system","subtype":"compact_boundary","uuid":"b1","content":"Conversation compacted","compactMetadata":{"trigger":"auto","preTokens":180000},"timestamp":"` + at(2*time.Hour) + `"}`,
		`{"type":"user","uuid":"s2","parentUuid":"b1","isCompactSummary":true,"isVisibleInTranscriptOnly":true,"message":{"role":"user","content":"` + summary + `"},"timestamp":"` + at(2*time.Hour) + `"}`,
		`{"type":"attachment","uuid":"x1","attachment":{"type":"environment","snapshot":{}},"timestamp":"` + at(2*time.Hour-time.Second) + `"}`,
		`{"type":"system","subtype":"informational","uuid":"i1","level":"notice","content":"Compacted while idle, before the prompt cache expired","timestamp":"` + at(2*time.Hour-time.Second) + `"}`,
		// its metadata, rewritten in place since: no time of its own
		`{"type":"last-prompt","lastPrompt":"tidy the example module","sessionId":"x"}`,
		`{"type":"ai-title","aiTitle":"Tidy the example module","sessionId":"x"}`,
		`{"type":"mode","mode":"default","sessionId":"x"}`,
		`{"type":"permission-mode","permissionMode":"bypassPermissions","sessionId":"x"}`,
		`{"type":"atis-latch","sessionId":"x"}`,
	}
	if len(lines[10]) <= 8<<10 {
		t.Fatalf("the summary line is %d bytes: the quick read only skips lines over 8 KB", len(lines[10]))
	}
	write := func(lines []string) (*Node, string) {
		t.Helper()
		id := "00000098-0000-4000-8000-000000000000"
		p := filepath.Join(t.TempDir(), id+".jsonl")
		if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, now, now); err != nil { // the metadata, rewritten just now
			t.Fatal(err)
		}
		return &Node{paths: map[string]string{id: p}, metas: map[string]quickMeta{}, bgScans: map[string]*bgScan{}}, id
	}
	n, id := write(lines)
	st, err := os.Stat(n.paths[id])
	if err != nil {
		t.Fatal(err)
	}
	m := n.meta(n.paths[id], st)
	if m.claudeTurn || !m.turnClosed {
		t.Fatalf("turn open %v, closed %v: the compaction, the /copy and the away summary start no turn", m.claudeTurn, m.turnClosed)
	}
	if want := now.Add(-3 * time.Hour); !m.lastPromptAt.Equal(want) {
		t.Fatalf("last prompt %v, want the user's %v, not the compaction", m.lastPromptAt, want)
	}
	for _, host := range []string{proto.StatusIdle, proto.StatusDone} {
		if got := n.claudeStatus(id, host2(host), now); got != host {
			t.Fatalf("host %s: %s; the host's word stands", host, got)
		}
	}

	// A real long prompt is still a turn that starts (the shortcut stays for what it is for).
	long := `{"type":"user","uuid":"u9","message":{"role":"user","content":"` + strings.Repeat("Please also check the example. ", 400) + `"},"timestamp":"` + at(time.Second) + `"}`
	n, id = write(append(lines[:5:5], long))
	st, _ = os.Stat(n.paths[id])
	if m := n.meta(n.paths[id], st); !m.claudeTurn {
		t.Fatal("a long prompt starts a turn")
	}
	if got := n.claudeStatus(id, host2(proto.StatusIdle), now); got != proto.StatusWorking {
		t.Fatalf("a long prompt just sent, host idle: %s, want working", got)
	}
}

// host2 is a Claude in a herdr pane the host says is in state, its title Claude Code's waiting mark.
func host2(state string) host.Agent {
	return host.Agent{Agent: "claude", AgentStatus: state, RawTitle: "✳ example"}
}

// Another Claude session's message to an idle one starts a turn: the session shows as working while it does, a short
// message or a long hand-over alike (Claude Code writes it as a hidden user line with the sender in its origin).
func TestPeerMessageStartsATurn(t *testing.T) {
	now := time.Now().UTC()
	at := func(ago time.Duration) string { return now.Add(-ago).Format(time.RFC3339Nano) }
	peer := func(body string) string {
		msg, _ := json.Marshal("Another Claude session sent a message:\n<cross-session-message from=\"uds:/tmp/x.sock\" from-name=\"ark-d1\" from-mode=\"bypass\">\n" +
			body + "\n</cross-session-message>\n\nThis came from another Claude session — not typed by your user.")
		origin, _ := json.Marshal(map[string]any{"kind": "peer", "from": "uds:/tmp/x.sock", "name": "ark-d1", "body": body})
		return `{"type":"user","uuid":"p1","parentUuid":"d1","isMeta":true,"promptSource":"system","turnOrigin":"peer","origin":` + string(origin) +
			`,"message":{"role":"user","content":` + string(msg) + `},"timestamp":"` + at(5*time.Second) + `"}`
	}
	for name, body := range map[string]string{"short": "Take over issue 1.", "long": strings.Repeat("Take over issue 1 and its tests. ", 400)} {
		lines := []string{
			`{"type":"user","uuid":"u1","message":{"role":"user","content":"tidy the example module"},"timestamp":"` + at(time.Hour) + `"}`,
			`{"type":"assistant","uuid":"a1","parentUuid":"u1","message":{"role":"assistant","content":[{"type":"text","text":"Done."}]},"timestamp":"` + at(time.Hour-time.Second) + `"}`,
			`{"type":"system","subtype":"turn_duration","uuid":"d1","parentUuid":"a1","durationMs":1000,"timestamp":"` + at(time.Hour-time.Second) + `"}`,
			peer(body),
		}
		id := "00000099-0000-4000-8000-000000000000"
		p := filepath.Join(t.TempDir(), id+".jsonl")
		if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		n := &Node{paths: map[string]string{id: p}, metas: map[string]quickMeta{}, bgScans: map[string]*bgScan{}}
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if m := n.meta(p, st); !m.claudeTurn || m.turnClosed {
			t.Fatalf("%s: turn open %v, closed %v", name, m.claudeTurn, m.turnClosed)
		}
		if got := n.claudeStatus(id, host2(proto.StatusIdle), now); got != proto.StatusWorking {
			t.Fatalf("%s: a peer's message just came, host idle: %s, want working", name, got)
		}
		// A session Persona started runs its hooks: they saw the last turn's Stop an hour ago and no prompt since (the
		// peer's message fires none), and the transcript's open turn written after it says working.
		n.hooks = &hookTurns{}
		n.hooks.prompt(id, now.Add(-time.Hour))
		n.hooks.stop(id, now.Add(-time.Hour+time.Second))
		if got := n.claudeStatus(id, host2(proto.StatusIdle), now); got != proto.StatusWorking {
			t.Fatalf("%s, with hooks: %s, want working", name, got)
		}
		n.hooks = nil
	}
}
