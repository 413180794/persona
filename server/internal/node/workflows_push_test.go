package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// Workflow progress must reach the hub even when only its journal grows and the main turn stays stopped.
func TestWorkflowPollPushesJournalProgress(t *testing.T) {
	n, f, home := herdrNode(t)
	const cwd = "/tmp/workflow-poll"
	at := time.Now().Add(-time.Minute)
	stamp := at.UTC().Format(time.RFC3339)
	tp := writeTranscript(t, home, cwd, sidA, at,
		`{"type":"user","timestamp":"`+stamp+`","cwd":"`+cwd+`","message":{"role":"user","content":"Review this project"}}`,
		`{"type":"assistant","uuid":"a1","timestamp":"`+stamp+`","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu1","name":"Workflow","input":{"name":"review"}}]}}`,
		`{"type":"user","uuid":"u1","timestamp":"`+stamp+`","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu1","content":"Workflow launched in background. Task ID: wt1\nSummary: Review\nRun ID: wf_poll\n"}]}}`,
		`{"type":"system","subtype":"turn_duration","durationMs":1000,"pendingWorkflowCount":1,"timestamp":"`+stamp+`"}`)
	sess := strings.TrimSuffix(tp, ".jsonl")
	writeRun(t, sess, "wf_poll", `{"type":"started","agentId":"a1","label":"review:first","phase":"Review"}`+"\n"+
		`{"type":"started","agentId":"a2","label":"review:second","phase":"Review"}`+"\n", nil, time.Minute)
	f.reply("session.snapshot", snapshot(nil, nil,
		[]any{claudeAgent(sidA, proto.StatusIdle, "w1:p1", "w1:t1", "w1", cwd, "review")}, nil))
	pushes, stop := pollNode(t, n)
	defer stop()
	nextChanged := func(timeout time.Duration) proto.Session {
		t.Helper()
		deadline := time.NewTimer(timeout)
		defer deadline.Stop()
		for {
			select {
			case push := <-pushes:
				if push.Event != "sessions.changed" {
					continue
				}
				var changed struct {
					Sessions []proto.Session `json:"sessions"`
				}
				if err := json.Unmarshal(push.Params, &changed); err != nil {
					t.Fatal(err)
				}
				for _, s := range changed.Sessions {
					if s.SessionID == sidA {
						return s
					}
				}
			case <-deadline.C:
				t.Fatalf("no sessions.changed within %s", timeout)
				return proto.Session{}
			}
		}
	}
	before := nextChanged(5 * time.Second)
	if before.Rev == 0 || len(before.Workflows) != 1 || before.Workflows[0].AgentsDone != 0 || before.Workflows[0].AgentsRunning != 2 {
		t.Fatalf("initial session: %+v", before)
	}

	journal := filepath.Join(sess, "subagents", "workflows", "wf_poll", "journal.jsonl")
	file, err := os.OpenFile(journal, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteString(`{"type":"result","agentId":"a1","result":{"ok":true}}` + "\n")
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("append result: write=%v close=%v", writeErr, closeErr)
	}
	after := nextChanged(3 * time.Second)
	if after.Rev != before.Rev || after.Status != before.Status || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("main session changed: before=%+v after=%+v", before, after)
	}
	if len(after.Workflows) != 1 || after.Workflows[0].AgentsDone != 1 || after.Workflows[0].AgentsStarted != 2 || after.Workflows[0].AgentsRunning != 1 {
		t.Fatalf("journal progress was not pushed: %+v", after.Workflows)
	}
}
