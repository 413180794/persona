package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nwverse/persona/server/internal/assistant"
)

// quietTodo is the task the conversation conv opened last, once its agent is linked.
func quietTodo(t *testing.T, h *Hub, conv string, n int) Todo {
	t.Helper()
	if _, err := taskPlan(h, conv, planArgs(true)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the link", func() bool {
		ts := taskTodos(h)
		return len(ts) == n && func() bool {
			for _, td := range ts {
				if len(td.Agents) == 0 {
					return false
				}
			}
			return true
		}()
	})
	var last Todo
	for _, td := range taskTodos(h) {
		if td.Origin == conv && td.CreatedAt.After(last.CreatedAt) {
			last = td
		}
	}
	return last
}

// A task the main conversation opens is quiet while "Notify main chat tasks in the main chat only" is on (the default), kept in the
// store; one from another conversation is not; turning the setting off leaves the tasks there are. A quiet task's
// agent is flagged on the session list, stays off the Live Activity and gets no notify event (#1731).
func TestQuietTasksFromTheMainConversation(t *testing.T) {
	database := setupTestDB(t)
	h := newTestHub(t)
	h.store = database
	ctx := context.Background()
	if err := database.PutSetting(ctx, "assistant", assistant.Settings{MachineID: "box", Main: "conv1"}); err != nil {
		t.Fatal(err)
	}
	connectTaskNode(t, h, "box")
	connectAssistant(t, h)

	quiet := quietTodo(t, h, "conv1", 1)
	side := quietTodo(t, h, "conv2", 2)
	if !quiet.Quiet || side.Quiet {
		t.Fatalf("quiet %v, side %v", quiet.Quiet, side.Quiet)
	}
	if row, err := database.GetTodo(ctx, quiet.ID); err != nil || !row.Quiet {
		t.Fatalf("stored: %+v %v", row, err)
	}

	mux := http.NewServeMux()
	h.assistantRoutes(mux)
	call := func(method, body string) map[string]any {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, "/api/v1/assistant", bytes.NewBufferString(body)))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", method, w.Code, w.Body)
		}
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	if got := call(http.MethodGet, ""); got["main_tasks_quiet"] != true {
		t.Fatalf("GET = %v", got["main_tasks_quiet"])
	}
	m := h.localMachine("box")
	if got := h.callAssistantTool(ctx, m, "conv1", "persona_settings", json.RawMessage(`{"action":"get","key":"assistant.main_tasks_quiet"}`)); got.Content[0].Text != "true" {
		t.Fatalf("persona_settings get = %+v", got)
	}
	if got := call(http.MethodPut, `{"main_tasks_quiet":false}`); got["main_tasks_quiet"] != false || got["main"] != "conv1" {
		t.Fatalf("PUT = %v", got)
	}
	later := quietTodo(t, h, "conv1", 3)
	if later.Quiet {
		t.Fatal("a task opened with the setting off is quiet")
	}
	if row, _ := database.GetTodo(ctx, quiet.ID); !row.Quiet {
		t.Fatal("turning the setting off changed an existing task")
	}
	if got := call(http.MethodPut, `{"main_tasks_quiet":true}`); got["main_tasks_quiet"] != true {
		t.Fatalf("PUT = %v, reported %+v", got, h.assistantProfile())
	}

	// A quiet task's agent flagged on the list, off the Live Activity and without a push is the manager's
	// (assistant TestQuietAgents).
}
