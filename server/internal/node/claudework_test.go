package node

import (
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
// shows Claude Code's spinner or its transcript has an unfinished turn written to lately; a finished turn (its
// turn_duration, an interrupt) keeps the host's word, and so do blocked and other agents.
func TestClaudeStatusOverTheHost(t *testing.T) {
	dir := t.TempDir()
	const (
		prompt    = `{"type":"user","message":{"role":"user","content":"look for the bug"},"uuid":"u1","timestamp":"2026-01-01T00:00:00Z"}`
		toolUse   = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Grep","input":{"pattern":"x"}}]},"uuid":"a1","parentUuid":"u1","timestamp":"2026-01-01T00:00:01Z"}`
		reply     = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"found it"}]},"uuid":"a2","parentUuid":"a1","timestamp":"2026-01-01T00:00:02Z"}`
		duration  = `{"type":"system","subtype":"turn_duration","uuid":"d1","parentUuid":"a2","durationMs":2000,"timestamp":"2026-01-01T00:00:03Z"}`
		interrupt = `{"type":"user","message":{"role":"user","content":"[Request interrupted by user]"},"uuid":"u2","timestamp":"2026-01-01T00:00:04Z"}`
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
			if err := os.WriteFile(p, []byte(strings.Join(tc.lines, "\n")+"\n"), 0o644); err != nil {
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
