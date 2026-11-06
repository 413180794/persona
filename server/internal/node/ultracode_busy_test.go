package node

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
)

// Synthetic transcripts exercise the same corrected status used by the list.
func TestUltracodeBusyUsesClaudeStatus(t *testing.T) {
	const id = "aaaaaaaa-0000-4000-8000-000000000099"
	// written just now: a fresh turn is one whose lines are recent, not only its file (#5967)
	at := time.Now().UTC().Format(time.RFC3339Nano)
	prompt := `{"type":"user","uuid":"u1","timestamp":"` + at + `","message":{"role":"user","content":"check the example"}}`
	done := `{"type":"system","subtype":"turn_duration","uuid":"d1","timestamp":"` + at + `","durationMs":2000}`
	// the turn that ended long before the hook's prompt
	doneLongAgo := `{"type":"system","subtype":"turn_duration","uuid":"d1","timestamp":"2026-01-01T00:00:02Z","durationMs":2000}`
	for _, tc := range []struct {
		name, status, title, body string
		hookOpen, want            bool
	}{
		{"host done but fresh turn open", proto.StatusDone, "✳ example", prompt, false, true},
		{"host idle but fresh turn open", proto.StatusIdle, "✳ example", prompt, false, true},
		{"host unknown with working title", proto.StatusUnknown, "◐ example", "{}", false, true},
		{"closed turn clears stale title", proto.StatusDone, "◐ example", prompt + "\n" + done, false, false},
		{"resting after a closed turn", proto.StatusIdle, "✳ example", prompt + "\n" + done, false, false},
		{"blocked remains queued", proto.StatusBlocked, "✳ example", done, false, true},
		{"host working remains queued", proto.StatusWorking, "✳ example", done, false, true},
		{"hook prompt precedes transcript", proto.StatusDone, "", doneLongAgo, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), id+".jsonl")
			if err := os.WriteFile(path, []byte(tc.body+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			n := &Node{paths: map[string]string{id: path}, metas: map[string]quickMeta{}, bgScans: map[string]*bgScan{}, hooks: &hookTurns{},
				agents: map[string]host.Agent{id: {Agent: "claude", AgentStatus: tc.status, RawTitle: tc.title}}}
			if tc.hookOpen {
				now := time.Now()
				old := now.Add(-time.Hour)
				if err := os.Chtimes(path, old, old); err != nil {
					t.Fatal(err)
				}
				n.hooks.m = map[string]*hookTurn{id: {prompts: 2, completed: 1, stopped: true, promptAt: now.Add(-time.Second), stopAt: old}}
			}
			if got := n.ultracodeBusy(id); got != tc.want {
				t.Fatalf("busy = %v, want %v", got, tc.want)
			}
		})
	}
}

// Only the foreground turn holds a switch back (#5510): a dynamic workflow that runs on after the turn ended, or a
// background read not done yet, leaves the input box free, and a message must not wait tens of minutes behind it.
func TestUltracodeBusyIgnoresBackgroundWork(t *testing.T) {
	for _, ended := range []bool{false, true} {
		name := "running after foreground turn"
		if ended {
			name = "ended workflow"
		}
		t.Run(name, func(t *testing.T) {
			n, id, sess := workflowSession(t)
			n.metas, n.bgScans = map[string]quickMeta{}, map[string]*bgScan{}
			n.agents = map[string]host.Agent{id: {Agent: "claude", AgentStatus: proto.StatusDone, RawTitle: "✳ example"}}
			writeRun(t, sess, "wf_ultra", `{"type":"started","agentId":"a1","label":"check"}`+"\n", map[string]string{"a1": "checking"}, time.Second)
			if ended {
				if err := os.WriteFile(filepath.Join(sess, "workflows", "wf_ultra.json"), []byte(`{"runId":"wf_ultra","taskId":"w1","status":"killed"}`), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			at := time.Now().UTC().Format(time.RFC3339)
			body := strings.Join([]string{
				`{"type":"assistant","uuid":"a1","timestamp":"` + at + `","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Workflow","input":{"name":"example"}}]}}`,
				`{"type":"user","uuid":"u1","timestamp":"` + at + `","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"Workflow launched in background. Task ID: w1\nSummary: check\nRun ID: wf_ultra\n"}]}}`,
				`{"type":"system","subtype":"turn_duration","timestamp":"` + at + `","durationMs":1000,"pendingWorkflowCount":1}`,
			}, "\n") + "\n"
			if err := os.WriteFile(n.paths[id], []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if !ended {
				running := false
				m := n.ultracodeMeta(id)
				for _, w := range n.workflows(id, m.wf, m.wfScript) {
					running = running || w.Running
				}
				if !running {
					t.Fatal("fixture: the workflow should still run")
				}
			}
			if n.ultracodeBusy(id) {
				t.Fatalf("a background workflow (ended = %v) held the switch back", ended)
			}
		})
	}
}

func TestUltracodeBusyIgnoresBackgroundScan(t *testing.T) {
	n, id, _ := workflowSession(t)
	n.metas = map[string]quickMeta{}
	n.bgScans = map[string]*bgScan{n.paths[id]: {pending: true}}
	n.agents = map[string]host.Agent{id: {Agent: "claude", AgentStatus: proto.StatusIdle}}
	if n.ultracodeBusy(id) {
		t.Fatal("a background read not done yet held the switch back")
	}
}
