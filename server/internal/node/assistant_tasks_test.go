package node

import (
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/transcript"
)

func assistantNode(t *testing.T) *Node {
	t.Helper()
	return &Node{home: t.TempDir(), paths: map[string]string{}, files: map[string]*transcript.File{}, procs: map[string]*assistantProc{},
		agents: map[string]host.Agent{}, state: &stateFile{Sessions: map[string]sessionState{}}, logger: log.New(io.Discard, "", 0)}
}

// claude -p reports its running background tasks and monitors (background_tasks_changed); the process waiting on
// them must outlive the idle timeout, and the list goes into the session state for after a restart.
func TestAssistantWithBackgroundTasksIsNotReaped(t *testing.T) {
	n := assistantNode(t)
	const id = "11111111-2222-3333-4444-555555555555"
	n.state.put(id, sessionState{Launcher: "claude", Cwd: "/home/u/.persona/assistant"})
	p := &assistantProc{id: id}
	stream := func(lines ...string) { n.readAssistant(p, strings.NewReader(strings.Join(lines, "\n")+"\n")) }

	stream(`{"type":"system","subtype":"background_tasks_changed","tasks":[{"task_id":"b1","task_type":"local_bash","description":"Sleep 23 minutes"}]}`,
		`{"type":"result","subtype":"success"}`)
	if st, _ := n.state.get(id); len(st.Tasks) != 1 || st.Tasks[0] != "Sleep 23 minutes" {
		t.Fatalf("remembered tasks %v", st.Tasks)
	}
	later := time.Now().Add(assistantIdleTimeout + time.Minute)
	p.mu.Lock()
	reap := p.reapable(later)
	forgotten := p.reapable(time.Now().Add(assistantTaskMaxIdle + time.Minute))
	p.mu.Unlock()
	if reap {
		t.Fatal("an idle process with a background task running was reaped")
	}
	if !forgotten {
		t.Fatal("a process idle past assistantTaskMaxIdle should go even with tasks")
	}

	stream(`{"type":"system","subtype":"background_tasks_changed","tasks":[]}`)
	p.mu.Lock()
	reap = p.reapable(time.Now().Add(assistantIdleTimeout + time.Minute))
	p.mu.Unlock()
	if !reap {
		t.Fatal("with its tasks done the process is reaped after the idle timeout again")
	}
	if st, _ := n.state.get(id); len(st.Tasks) != 0 {
		t.Fatalf("finished tasks still remembered: %v", st.Tasks)
	}
}

// After a restart, a conversation whose tasks died gets told once; herdr sessions and running ones are left alone.
func TestRecoverAssistantTasksAfterRestart(t *testing.T) {
	n := assistantNode(t)
	const lost, alive, herdrOne = "11111111-2222-3333-4444-000000000001", "11111111-2222-3333-4444-000000000002", "11111111-2222-3333-4444-000000000003"
	n.state.put(lost, sessionState{Cwd: "/home/u/.persona/assistant", Tasks: []string{"tail the deploy log", "Sleep 23 minutes"}})
	n.state.put(alive, sessionState{Cwd: "/home/u/.persona/assistant", Tasks: []string{"still mine"}})
	n.state.put(herdrOne, sessionState{Cwd: "/home/u/src", Tasks: []string{"not an assistant"}})
	n.procs[alive] = &assistantProc{id: alive}
	sent := map[string]string{}
	n.recoverAssistantTasks(func(id, text string) error { sent[id] = text; return nil })
	if len(sent) != 1 || !strings.Contains(sent[lost], "tail the deploy log") || !strings.Contains(sent[lost], "Sleep 23 minutes") {
		t.Fatalf("sent %v", sent)
	}
	if st, _ := n.state.get(lost); len(st.Tasks) != 0 {
		t.Fatal("the lost tasks should be forgotten once told")
	}
	n.recoverAssistantTasks(func(id, text string) error { t.Fatalf("told %s twice", id); return nil })
}
