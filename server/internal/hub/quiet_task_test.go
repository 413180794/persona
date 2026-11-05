package hub

import (
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// The own conversation of a scheduled task that pushes only what its runs send (#2396) is known by the task list the
// assistant reports; other tasks' conversations, a task without one yet and the main conversation are not.
func TestQuietTaskConversation(t *testing.T) {
	h := newTestHub(t)
	if h.quietTaskConversation("own-q") {
		t.Fatal("nothing reported yet: no conversation is quiet")
	}
	a := &h.assistant
	a.mu.Lock()
	a.got, a.tasks = true, []assistantwire.TaskView{
		{ID: "fu_q", Push: assistantwire.PushAsked, RunMachineID: "server", RunSessionID: "own-q"},
		{ID: "fu_e", RunMachineID: "server", RunSessionID: "own-e"},
		{ID: "fu_new", Push: assistantwire.PushAsked}, // its first run has not started its conversation yet
	}
	a.mu.Unlock()
	for session, want := range map[string]bool{"own-q": true, "own-e": false, "main-1": false, "": false} {
		if got := h.quietTaskConversation(session); got != want {
			t.Errorf("%q: %v, want %v", session, got, want)
		}
	}
}

// The hub leaves out a quiet task's normal finish on Claude Code, but not once that conversation is the main one, nor
// a headless Codex's (its failures are not on the session yet), nor an error.
func TestQuietTaskFinish(t *testing.T) {
	h := newTestHub(t)
	a := &h.assistant
	a.mu.Lock()
	a.got, a.tasks = true, []assistantwire.TaskView{{ID: "fu_q", Push: assistantwire.PushAsked, RunSessionID: "own-q"}}
	a.mu.Unlock()
	done := proto.Session{SessionID: "own-q", Status: proto.StatusDone, LastText: "没有新邮件。"}
	if h.quietTaskFinish(done) {
		t.Fatal("the assistant's settings (its main conversation) not known yet: pushed as ever")
	}
	a.mu.Lock()
	a.profile = &assistantwire.Profile{MachineID: "server", Main: "main-1"}
	a.mu.Unlock()
	if !h.quietTaskFinish(done) {
		t.Fatal("a quiet task's normal finish is not pushed")
	}
	codex := done
	codex.Agent = "codex"
	cut := done
	cut.LastText = "API Error: 529 overloaded"
	other := done
	other.SessionID = "main-1"
	for name, s := range map[string]proto.Session{"headless codex": codex, "cut off": cut, "another conversation": other} {
		if h.quietTaskFinish(s) {
			t.Errorf("%s is pushed as ever", name)
		}
	}
	a.mu.Lock()
	a.profile = &assistantwire.Profile{MachineID: "server", Main: "own-q"}
	a.mu.Unlock()
	if h.quietTaskFinish(done) {
		t.Fatal("the main conversation is never quiet, even when it was the task's")
	}
}
