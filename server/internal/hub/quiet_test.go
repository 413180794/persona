package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
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

// A task the main conversation opens is quiet while 主对话派的任务只在主对话里通知 is on (the default), kept in the
// store; one from another conversation is not; turning the setting off leaves the tasks there are. A quiet task's
// agent is flagged on the session list, stays off the Live Activity and gets no notify event (#1731).
func TestQuietTasksFromTheMainConversation(t *testing.T) {
	database := setupTestDB(t)
	h := newTestHub(t)
	h.store = database
	ctx := context.Background()
	if err := database.PutSetting(ctx, "assistant", AssistantConfig{MachineID: "box", Main: "conv1"}); err != nil {
		t.Fatal(err)
	}
	h.initTodos(ctx)
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
	if got, _ := h.toolJarvisSettings(ctx, m, "conv1", json.RawMessage(`{"action":"get","key":"assistant.main_tasks_quiet"}`)); got != "true" {
		t.Fatalf("jarvis_settings get = %q", got)
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
	change, err := h.setMainTasksQuiet(ctx, json.RawMessage(`true`))
	if err != nil || !strings.Contains(change.summary, "关 → 开") {
		t.Fatalf("change %+v %v", change, err)
	}
	if _, err := change.apply(ctx, nil); err != nil || !h.mainTasksQuiet(ctx) {
		t.Fatalf("apply: %v", err)
	}

	// s1 is the quiet task's agent, s2 the side conversation's: both working, s1 also asks something.
	m.setSessions([]proto.Session{
		{SessionID: "s1", Cwd: "/code/app", Agent: "claude", Status: proto.StatusWorking, PaneID: "p1"},
		{SessionID: "s2", Cwd: "/code/app", Agent: "claude", Status: proto.StatusWorking, PaneID: "p2"},
	})
	flags := map[string]bool{}
	for _, s := range h.decorated(m) {
		flags[s.SessionID] = s.Quiet
	}
	if !flags["s1"] || flags["s2"] {
		t.Fatalf("quiet flags = %v", flags)
	}
	if st := h.live.snapshot(); st.Working != 1 || len(st.Items) != 1 || st.Items[0].ID != "box/s2" {
		t.Fatalf("fleet = %+v", st)
	}

	c := &client{out: make(chan []byte, 64)}
	h.clients[c] = struct{}{}
	h.onClientHello(c, []byte(`{"platform":"web","version":"test","features":["ws_notify"]}`))
	<-c.out
	notified := func() []string {
		var kinds []string
		for {
			select {
			case raw := <-c.out:
				var ev struct {
					Type      string `json:"type"`
					SessionID string `json:"session_id"`
				}
				_ = json.Unmarshal(raw, &ev)
				if ev.Type == "notify" {
					kinds = append(kinds, ev.SessionID)
				}
			case <-time.After(100 * time.Millisecond):
				return kinds
			}
		}
	}
	h.announce(proto.Interaction{ID: "ix_q", MachineID: "box", SessionID: "s1", Kind: proto.IxPermission, ToolName: "Bash"})
	h.announce(proto.Interaction{ID: "ix_s", MachineID: "box", SessionID: "s2", Kind: proto.IxPermission, ToolName: "Bash"})
	if got := notified(); len(got) != 1 || got[0] != "s2" {
		t.Fatalf("notify events for %v", got)
	}
}
