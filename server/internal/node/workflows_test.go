package node

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
	// An hour ago: ended runs are listed for a day after they end (workflowKeep), so a fixed time stops being one (#5916).
	started := time.Now().Add(-time.Hour).UnixMilli()
	state := `{"runId":"wf_k","taskId":"wk","status":"killed","error":"Error: Workflow aborted\n    at S (x.js:1:2)","startTime":` +
		fmt.Sprint(started) + `,"durationMs":14725,"workflowName":"parity-audit","phases":[{"title":"Audit"},{"title":"Verify"}]}`
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
	if len(s.Workflows) != 1 || len(s.Workflows[0].Agents) == 0 {
		t.Fatalf("workflows: %+v", s.Workflows)
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

func TestWorkflowPhaseStats(t *testing.T) {
	for _, tt := range []struct {
		name, status string
		phases       []string
		want         []proto.WorkflowPhaseStats
	}{
		{"declared and unknown phases", proto.WorkflowRunning, []string{"Audit", "Verify", "Later"}, []proto.WorkflowPhaseStats{
			{Title: "Audit", Started: 1, Done: 1}, {Title: "Verify", Started: 1, Running: 1}, {Title: "Later"},
			{Title: "Extra", Started: 1, Done: 1}, {Title: "", Started: 1, Running: 1},
		}},
		{"stopped agents are neither done nor running", proto.WorkflowKilled, []string{"Audit", "Verify"}, []proto.WorkflowPhaseStats{
			{Title: "Audit", Started: 1, Done: 1}, {Title: "Verify", Started: 1},
			{Title: "Extra", Started: 1, Done: 1}, {Title: "", Started: 1},
		}},
		{"undeclared phases keep start order", proto.WorkflowRunning, nil, []proto.WorkflowPhaseStats{
			{Title: "Audit", Started: 1, Done: 1}, {Title: "Verify", Started: 1, Running: 1},
			{Title: "Extra", Started: 1, Done: 1}, {Title: "", Started: 1, Running: 1},
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, parent, sess := workflowSession(t)
			writeRun(t, sess, "wf_stats", `{"type":"started","agentId":"a","phase":"Audit"}
{"type":"started","agentId":"b","phase":"Verify"}
{"type":"started","agentId":"c","phase":"Extra"}
{"type":"started","agentId":"d"}
{"type":"result","agentId":"a"}
{"type":"result","agentId":"c"}
`, map[string]string{"a": "done", "b": "working", "c": "done", "d": "working"}, time.Minute)
			ended := time.Now()
			run := proto.Workflow{TaskID: "task", RunID: "wf_stats", Status: tt.status, Running: tt.status == proto.WorkflowRunning,
				Phases: tt.phases, StartedAt: ended.Add(-time.Hour)}
			if !run.Running {
				run.EndedAt = &ended
			}
			w, _ := workflowProgress(filepath.Join(sess, "subagents", "workflows", "wf_stats"), "", parent, run, "")
			if !reflect.DeepEqual(w.PhaseStats, tt.want) {
				t.Fatalf("phase_stats = %+v, want %+v", w.PhaseStats, tt.want)
			}
			if w.AgentsStarted != 4 || w.AgentsDone != 2 || w.AgentsRunning != map[bool]int{true: 2, false: 0}[run.Running] {
				t.Fatalf("run counters = %+v", w)
			}
			for _, a := range w.Agents {
				if a.Running != (a.EndedAt == nil) {
					t.Fatalf("running/ended_at mismatch: %+v", a)
				}
				if a.Status == "stopped" && !a.EndedAt.Equal(ended) {
					t.Fatalf("stopped agent ended_at = %v, want run end %v", a.EndedAt, ended)
				}
			}
			wire, err := json.Marshal(w.PhaseStats)
			if err != nil || !strings.Contains(string(wire), `"title":""`) {
				t.Fatalf("unknown phase title must be sent: %s (%v)", wire, err)
			}
		})
	}
}

func TestWorkflowTotalsBeforeAgentLimit(t *testing.T) {
	_, parent, sess := workflowSession(t)
	var journal strings.Builder
	const count = proto.WorkflowAgentsMax + 5
	for i := range count {
		fmt.Fprintf(&journal, "{\"type\":\"started\",\"agentId\":\"a%d\",\"phase\":\"Audit\"}\n", i)
		if i < 10 {
			fmt.Fprintf(&journal, "{\"type\":\"result\",\"agentId\":\"a%d\"}\n", i)
		}
	}
	writeRun(t, sess, "wf_many", journal.String(), nil, time.Minute)
	dir := filepath.Join(sess, "subagents", "workflows", "wf_many")
	for i := range count {
		body := edgeReply(fmt.Sprint(i), "2026-01-01T00:00:00Z", "model-a", `[{"type":"tool_use","id":"tool","name":"Read","input":{"file_path":"/work/project/example.go"}}]`,
			`{"input_tokens":1,"cache_read_input_tokens":2,"cache_creation_input_tokens":3,"output_tokens":4}`)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("agent-a%d.jsonl", i)), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run := proto.Workflow{TaskID: "task", RunID: "wf_many", Status: proto.WorkflowRunning, Running: true, StartedAt: time.Now().Add(-time.Hour)}
	w, _ := workflowProgress(dir, "", parent, run, "")
	if len(w.Agents) != proto.WorkflowAgentsMax || w.Agents[0].ID != "a5" || w.Tokens != count*10 || w.Calls != count ||
		w.AgentsStarted != count || w.AgentsDone != 10 || w.AgentsRunning != count-10 {
		t.Fatalf("totals before limit = %+v", w)
	}
	if want := []proto.WorkflowPhaseStats{{Title: "Audit", Started: count, Done: 10, Running: count - 10}}; !reflect.DeepEqual(w.PhaseStats, want) {
		t.Fatalf("phase_stats = %+v, want %+v", w.PhaseStats, want)
	}
	for _, a := range w.Agents {
		if a.Model != "model-a" || a.Tokens != 10 || a.Calls != 1 || a.LatestTool == nil || a.LatestTool.Name != "Read" || a.LatestTool.Summary != "在读 example.go" {
			t.Fatalf("agent details = %+v", a)
		}
	}
}

func TestWorkflowFinalRefreshesAgentDetails(t *testing.T) {
	for _, completedAgent := range []bool{true, false} {
		t.Run(fmt.Sprintf("agent done %v", completedAgent), func(t *testing.T) {
			_, parent, sess := workflowSession(t)
			writeRun(t, sess, "wf_final", `{"type":"started","agentId":"a","phase":"Audit"}`+"\n", nil, time.Minute)
			dir := filepath.Join(sess, "subagents", "workflows", "wf_final")
			path := filepath.Join(dir, "agent-a.jsonl")
			first := edgeReply("first", "2026-01-01T00:00:00Z", "model-a", `[{"type":"tool_use","id":"one","name":"Read","input":{}}]`, `{"input_tokens":10}`)
			if err := os.WriteFile(path, []byte(first), 0o600); err != nil {
				t.Fatal(err)
			}
			run := proto.Workflow{TaskID: "task", RunID: "wf_final", Status: proto.WorkflowRunning, Running: true, StartedAt: time.Now().Add(-time.Hour)}
			subagents := filepath.Join(sess, "subagents")
			if w := readWorkflow(subagents, parent, run, ""); w.Tokens != 10 || w.Calls != 1 {
				t.Fatalf("initial details = %+v", w)
			}
			last := edgeReply("last", "2026-01-01T00:01:00Z", "model-b", `[{"type":"tool_use","id":"two","name":"Bash","input":{"command":"go test ./..."}},{"type":"text","text":"Finished"}]`, `{"input_tokens":20}`)
			if err := os.WriteFile(path, []byte(first+last), 0o600); err != nil {
				t.Fatal(err)
			}
			if completedAgent {
				journal := `{"type":"started","agentId":"a","phase":"Audit"}` + "\n" + `{"type":"result","agentId":"a"}` + "\n"
				if err := os.WriteFile(filepath.Join(dir, "journal.jsonl"), []byte(journal), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(sess, "workflows", "wf_final.json"), []byte(`{"status":"completed"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			wfCache.Lock()
			key := dir + "\x00" + run.Status
			memo := wfCache.m[key]
			memo.readAt = time.Time{}
			wfCache.m[key] = memo
			wfCache.Unlock()
			w := readWorkflow(subagents, parent, run, "")
			if w.Running || w.Tokens != 20 || w.Calls != 2 || w.Agents[0].Model != "model-b" || w.Agents[0].EndedAt == nil ||
				w.Agents[0].LastText != "Finished" || w.Agents[0].LatestTool.Name != "Bash" {
				t.Fatalf("final details froze before final append: %+v, agent %+v", w, w.Agents[0])
			}
			if completedAgent && w.Agents[0].EndedAt.Format(time.RFC3339) != "2026-01-01T00:01:00Z" {
				t.Fatalf("done agent end = %v", w.Agents[0].EndedAt)
			}
			if again := readWorkflow(subagents, parent, run, ""); !reflect.DeepEqual(again, w) {
				t.Fatalf("final cache changed: %+v -> %+v", w, again)
			}
		})
	}
}
