package hub

import (
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

func TestTodoReportConflictUsesSessionOwner(t *testing.T) {
	const (
		builtIn = assistantwire.AssistantID
		owner   = "m-owner"
		other   = "m-other"
	)
	for _, tc := range []struct {
		name         string
		machineOwner string
		conversation string
		reporters    []string
		want         string
	}{
		{"machine owner", owner, "", []string{builtIn, owner, other}, owner},
		// A session is its machine's owner's (ownership.go): a conversation another manager lists there overrides nothing.
		{"conversation on another manager's machine", other, owner, []string{builtIn, owner, other}, other},
		{"built-in conversation on another manager's machine", owner, builtIn, []string{builtIn, owner, other}, owner},
		{"built-in machine owner", builtIn, "", []string{builtIn, owner, other}, builtIn},
		{"owner has no link", owner, "", []string{builtIn, other}, builtIn},
		{"owner without built-in", owner, "", []string{owner, other}, owner},
		{"only another reporter", owner, "", []string{other}, other},
		{"single built-in manager", builtIn, "", []string{builtIn}, builtIn},
		{"single other manager", owner, "", []string{owner}, owner},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Exercise both arrival orders; the merge also ranges over unordered maps.
			for _, reverse := range []bool{false, true} {
				h := &Hub{byID: map[string]*Machine{"machine": {owner: tc.machineOwner}}, cfg: &Config{Machines: []MachineConfig{{ID: "machine"}}}}
				h.devices.assign = map[string]string{"machine": tc.machineOwner} // the roster's say (ownership.go)
				if tc.conversation != "" {
					h.assistantOf(tc.conversation).sessions = map[string]assistantwire.SessionView{
						"session": {SessionID: "session"},
					}
				}
				for i := range tc.reporters {
					if reverse {
						i = len(tc.reporters) - 1 - i
					}
					id := tc.reporters[i]
					h.takeTodoReport(id, &assistantwire.TodoReport{Links: []assistantwire.TodoLink{
						{MachineID: "machine", SessionID: "session", TodoID: "todo-" + id, Title: "Task " + id, Task: true},
					}})
				}
				want := assistantwire.TodoLink{MachineID: "machine", SessionID: "session", TodoID: "todo-" + tc.want, Title: "Task " + tc.want, Task: true}
				if got := h.todoLinks.links[[2]string{"machine", "session"}]; got != want {
					t.Fatalf("reverse=%v: got %+v, want %+v", reverse, got, want)
				}
				for _, id := range tc.reporters {
					wantID := ""
					if id == tc.want {
						wantID = want.TodoID
					}
					if got := h.taskByTitle("Task " + id); got != wantID {
						t.Fatalf("reverse=%v: task from %s = %q, want %q", reverse, id, got, wantID)
					}
				}
			}
		})
	}
}

func TestTodoReportsReplaceOnlyTheirOwnLinks(t *testing.T) {
	const owner = "m-owner"
	h := &Hub{byID: map[string]*Machine{"machine": {owner: owner}}, cfg: &Config{Machines: []MachineConfig{{ID: "machine"}}}}
	h.devices.assign = map[string]string{"machine": owner} // the roster's say (ownership.go)
	key := [2]string{"machine", "session"}
	owned := assistantwire.TodoLink{MachineID: key[0], SessionID: key[1], TodoID: "todo-owned", Title: "Owned", Task: true}
	fallback := assistantwire.TodoLink{MachineID: key[0], SessionID: key[1], TodoID: "todo-fallback", Title: "Fallback", Task: true}
	// Session IDs are only unique within a machine. Keep the other machine's link.
	unrelated := assistantwire.TodoLink{MachineID: "other-machine", SessionID: key[1], TodoID: "todo-unrelated"}
	h.takeTodoReport(assistantwire.AssistantID, &assistantwire.TodoReport{Links: []assistantwire.TodoLink{fallback, unrelated}})
	h.takeTodoReport(owner, &assistantwire.TodoReport{Links: []assistantwire.TodoLink{owned}})
	check := func(want assistantwire.TodoLink) {
		t.Helper()
		if got := h.todoLinks.links[key]; got != want {
			t.Fatalf("link = %+v, want %+v", got, want)
		}
		if len(h.todoLinks.links) != 2 || h.todoLinks.links[[2]string{unrelated.MachineID, unrelated.SessionID}] != unrelated {
			t.Fatalf("unrelated link lost: %+v", h.todoLinks.links)
		}
	}
	check(owned)
	h.takeTodoReport(owner, nil) // A failed read keeps the last report.
	check(owned)
	owned.TodoID, owned.Title = "todo-updated", "Updated"
	h.takeTodoReport(owner, &assistantwire.TodoReport{Links: []assistantwire.TodoLink{owned}})
	check(owned)
	h.takeTodoReport(owner, &assistantwire.TodoReport{}) // An empty report withdraws its links.
	check(fallback)
	h.takeTodoReport(assistantwire.AssistantID, &assistantwire.TodoReport{Links: []assistantwire.TodoLink{unrelated}})
	if _, ok := h.todoLinks.links[key]; ok {
		t.Fatal("link remains after every reporter withdrew it")
	}
}
