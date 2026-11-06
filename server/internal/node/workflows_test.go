package node

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// A session with workflow runs on disk the way Claude Code 2.1.308 lays them out (#2935).
func workflowSession(t *testing.T) (*Node, string, string) {
	t.Helper()
	const parent = "aaaaaaaa-0000-0000-0000-000000000002"
	home := t.TempDir()
	tp := filepath.Join(home, ".claude", "projects", "-src-app", parent+".jsonl")
	sess := strings.TrimSuffix(tp, ".jsonl")
	for _, d := range []string{filepath.Join(sess, "subagents", "workflows"), filepath.Join(sess, "workflows", "scripts")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(tp, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &Node{home: home, paths: map[string]string{parent: tp}}, parent, sess
}

func writeRun(t *testing.T, sess, run, journal string, agents map[string]string, age time.Duration) {
	t.Helper()
	dir := filepath.Join(sess, "subagents", "workflows", run)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-age)
	files := map[string]string{"journal.jsonl": journal}
	for id, text := range agents {
		files["agent-"+id+".jsonl"] = `{"type":"user","timestamp":"` + at.UTC().Format(time.RFC3339) + `","isSidechain":true}` + "\n" +
			`{"type":"assistant","isSidechain":true,"timestamp":"` + at.UTC().Format(time.RFC3339) + `","message":{"role":"assistant","content":[{"type":"text","text":"` + text + `"}]}}` + "\n"
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
}

const twoPhaseScript = "export const meta = {\n  name: 'parity-audit',\n  description: 'x',\n" +
	"  phases: [{ title: 'Audit', detail: 'one per domain' }, { title: 'Verify' }],\n}\n\nconst X = 1\n"

func TestWorkflowProgress(t *testing.T) {
	n, parent, sess := workflowSession(t)
	script := filepath.Join(sess, "workflows", "scripts", "parity-audit-wf_r1.js")
	if err := os.WriteFile(script, []byte(twoPhaseScript), 0o600); err != nil {
		t.Fatal(err)
	}
	writeRun(t, sess, "wf_r1", `{"type":"launched"}
{"type":"started","agentId":"a1","label":"audit:chat","phase":"Audit"}
{"type":"started","agentId":"a2","label":"audit:inbox","phase":"Audit"}
{"type":"result","agentId":"a1","result":{"gaps":[]}}
{"type":"started","agentId":"a3","label":"verify:chat#0","phase":"Verify"}
`, map[string]string{"a1": "found 2 gaps", "a2": "reading web/src", "a3": "checking"}, time.Minute)

	started := time.Now().Add(-10 * time.Minute)
	runs := []proto.Workflow{{TaskID: "w1", RunID: "wf_r1", Status: proto.WorkflowRunning, Running: true, StartedAt: started}}
	m := quickMeta{wf: runs, wfScript: map[string]string{"wf_r1": script},
		bg: []proto.BackgroundTask{{ID: "w1", Kind: "workflow", RunID: "wf_r1"}, {ID: "b1", Kind: "shell"}, {ID: "x", Kind: "agent"}}}
	s := proto.Session{SessionID: parent}
	n.setBackground(&s, m)
	if len(s.Workflows) != 1 {
		t.Fatalf("workflows: %+v", s.Workflows)
	}
	w := s.Workflows[0]
	if !w.Running || w.Status != proto.WorkflowRunning || w.Name != "parity-audit" || strings.Join(w.Phases, ",") != "Audit,Verify" ||
		w.Phase != "Verify" || w.PhaseIndex != 2 || w.AgentsStarted != 3 || w.AgentsDone != 1 || w.AgentsRunning != 2 {
		t.Fatalf("progress: %+v", w)
	}
	if w.PhaseCount != 2 || w.Agents[0].Status != "done" || w.Agents[1].Status != "running" {
		t.Fatalf("phase count / agent status: %+v", w)
	}
	if len(w.Agents) != 3 || w.Agents[0].ID != "a1" || w.Agents[0].Running || !w.Agents[1].Running ||
		w.Agents[0].SessionID != parent+"@a1" || w.Agents[0].LastText != "found 2 gaps" || w.Agents[2].Phase != "Verify" {
		t.Fatalf("agents: %+v", w.Agents)
	}
	if len(s.Background) != 2 || s.Background[0].ID != "w1" || s.Background[1].ID != "b1" {
		t.Fatalf("background: %+v", s.Background)
	}
	// A workflow agent reads like a subagent.
	if f, err := n.subagentFile(parent + "@a2"); err != nil || !strings.HasSuffix(f.Path, filepath.Join("wf_r1", "agent-a2.jsonl")) {
		t.Fatalf("agent file: %v %v", f, err)
	}
	if _, err := n.subagentFile(parent + "@a*"); err == nil {
		t.Fatal("a glob in the id found a file")
	}
}

func TestWorkflowEnded(t *testing.T) {
	n, parent, sess := workflowSession(t)
	writeRun(t, sess, "wf_k", `{"type":"started","agentId":"k1","label":"audit:x","phase":"Audit"}`+"\n", map[string]string{"k1": "…"}, time.Minute)
	state := `{"runId":"wf_k","taskId":"wk","status":"killed","error":"Error: Workflow aborted\n    at S (x.js:1:2)","startTime":` +
		"1793886384925" + `,"durationMs":14725,"workflowName":"parity-audit","phases":[{"title":"Audit"},{"title":"Verify"}]}`
	if err := os.WriteFile(filepath.Join(sess, "workflows", "wf_k.json"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	// The state file says it ended even though the transcript has no notification yet.
	runs := []proto.Workflow{{TaskID: "wk", RunID: "wf_k", Status: proto.WorkflowRunning, Running: true, StartedAt: time.Now().Add(-time.Hour)}}
	s := proto.Session{SessionID: parent}
	n.setBackground(&s, quickMeta{wf: runs, bg: []proto.BackgroundTask{{ID: "wk", Kind: "workflow"}}})
	if len(s.Background) != 0 {
		t.Fatalf("an ended run is still background: %+v", s.Background)
	}
	w := s.Workflows[0]
	if w.Running || w.Status != proto.WorkflowKilled || w.Error != "Error: Workflow aborted" || w.Name != "parity-audit" ||
		w.Agents[0].Running || w.Agents[0].Status != "stopped" || w.AgentsRunning != 0 || w.EndedAt == nil {
		t.Fatalf("ended: %+v", w)
	}

	// The transcript's word wins: a notification seen means completed.
	runs[0].Status, runs[0].Running, runs[0].EndedAt = proto.WorkflowCompleted, false, ptrTime(time.Now())
	n.setBackground(&s, quickMeta{wf: runs})
	if w := s.Workflows[0]; w.Running || w.Status != proto.WorkflowCompleted || w.Error != "" {
		t.Fatalf("notified: %+v", w)
	}
}

func TestWorkflowStalled(t *testing.T) {
	n, parent, sess := workflowSession(t)
	writeRun(t, sess, "wf_s", `{"type":"started","agentId":"s1","label":"a","phase":"P"}`+"\n", map[string]string{"s1": "…"}, 2*time.Hour)
	runs := []proto.Workflow{{TaskID: "ws", RunID: "wf_s", Status: proto.WorkflowRunning, Running: true, StartedAt: time.Now().Add(-3 * time.Hour)}}
	s := proto.Session{SessionID: parent}
	n.setBackground(&s, quickMeta{wf: runs, bg: []proto.BackgroundTask{{ID: "ws", Kind: "workflow"}}})
	if w := s.Workflows[0]; w.Running || w.Status != proto.WorkflowStalled || len(s.Background) != 0 {
		t.Fatalf("stalled: %+v %+v", w, s.Background)
	}
	// Ended more than a day ago: not listed.
	runs[0].Status, runs[0].Running, runs[0].EndedAt = proto.WorkflowCompleted, false, ptrTime(time.Now().Add(-25*time.Hour))
	writeRun(t, sess, "wf_s", "", nil, 25*time.Hour)
	n.setBackground(&s, quickMeta{wf: runs})
	if len(s.Workflows) != 0 {
		t.Fatalf("old run listed: %+v", s.Workflows)
	}
}

// The transcript's launch line reaches the session's background through the background scan (the main turn has ended:
// the session would be "Unread" without it).
func TestWorkflowFromTranscript(t *testing.T) {
	n, parent, sess := workflowSession(t)
	n.bgScans, n.metas = map[string]*bgScan{}, map[string]quickMeta{}
	writeRun(t, sess, "wf_t", `{"type":"started","agentId":"t1","label":"a","phase":"P"}`+"\n", map[string]string{"t1": "…"}, time.Second)
	tp := n.paths[parent]
	launch := `"Workflow launched in background. Task ID: wt1\nSummary: s\nRun ID: wf_t\n"`
	body := `{"type":"assistant","uuid":"a1","timestamp":"` + time.Now().UTC().Format(time.RFC3339) + `","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu1","name":"Workflow","input":{"name":"saved-one"}}]}}` + "\n" +
		`{"type":"user","uuid":"u1","timestamp":"` + time.Now().UTC().Format(time.RFC3339) + `","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu1","content":` + launch + `}]}}` + "\n" +
		`{"type":"system","subtype":"turn_duration","durationMs":1000,"pendingWorkflowCount":1,"timestamp":"` + time.Now().UTC().Format(time.RFC3339) + `"}` + "\n"
	if err := os.WriteFile(tp, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(tp)
	m := n.meta(tp, st)
	s := proto.Session{SessionID: parent}
	n.setBackground(&s, m)
	if len(s.Background) != 1 || s.Background[0].Kind != "workflow" || len(s.Workflows) != 1 || !s.Workflows[0].Running ||
		s.Workflows[0].Name != "saved-one" || s.Workflows[0].AgentsRunning != 1 {
		t.Fatalf("background %+v workflows %+v", s.Background, s.Workflows)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
