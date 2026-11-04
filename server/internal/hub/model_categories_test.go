package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestFourModelCategoriesCatalog(t *testing.T) {
	h, mux := modelTestHub(t)
	setTasks(h, Task{ID: "one", Title: "不应出现在模型配置里的任务", Enabled: true})
	r := modelRequest(mux, "GET", "", "")
	if r.Code != http.StatusOK {
		t.Fatal(r.Body.String())
	}
	var catalog struct {
		Purposes []ModelPurpose `json:"purposes"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, p := range catalog.Purposes {
		ids = append(ids, p.ID)
		if p.CategoryID != p.ID || len(p.Uses) == 0 || len(p.Fields) == 0 {
			t.Fatalf("category missing scope or editor: %+v", p)
		}
	}
	if !reflect.DeepEqual(ids, modelCategoryIDs) {
		t.Fatalf("catalog is not exactly the four ordered categories: %v", ids)
	}
}

func TestModelUsesAreReadOnlyAndIgnoreDeprecatedOverrides(t *testing.T) {
	h, mux := modelTestHub(t)
	ctx := context.Background()
	setTasks(h, Task{ID: "one", Title: "任务", Launcher: "old-task", Model: "old-model", Effort: "low", Enabled: true})
	bindings := map[string]ModelBinding{
		"assistant":    {Provider: "launcher.conversation", Model: "conversation", Effort: "high"},
		"agent.new":    {Provider: "launcher.agent", Model: "agent", Effort: "xhigh"},
		"kb":           {Provider: "legacy.kb", Model: "processing"},
		"inbox.triage": {Provider: "legacy.triage", Model: "screening"},
	}
	for id, binding := range bindings {
		if err := h.saveModelRecord(ctx, id, 0, binding); err != nil {
			t.Fatal(err)
		}
	}
	ids := []string{"task.one"}
	for _, p := range modelPurposes {
		if p.Parent != "" {
			ids = append(ids, p.ID)
		}
	}
	for _, id := range ids {
		old := ModelBinding{Provider: "launcher.deprecated", Model: "deprecated", Effort: "low"}
		if err := h.saveModelRecord(ctx, id, 0, old); err != nil {
			t.Fatal(err)
		}
		b, err := h.resolveModel(ctx, id)
		if err != nil || b != bindings[modelCategoryID(id)] {
			t.Fatalf("%s did not follow its category: %+v %v", id, b, err)
		}
		p, err := h.modelPurpose(ctx, id, h.modelProviders(ctx))
		if err != nil || len(p.Fields) != 0 || p.Override != (ModelBinding{}) || p.CategoryID != modelCategoryID(id) {
			t.Fatalf("%s exposed an editable use: %+v %v", id, p, err)
		}
		if r := modelRequest(mux, "PATCH", id, `{"revision":1,"model":"another"}`); r.Code != http.StatusBadRequest {
			t.Fatalf("%s accepted an individual override: %d %s", id, r.Code, r.Body.String())
		}
		stored, err := h.modelRecord(ctx, id)
		if err != nil || stored.Binding != old {
			t.Fatalf("deprecated record was destroyed: %+v %v", stored, err)
		}
	}
}

// A task's own conversation (started, then continued) runs on the 主对话 category (#1544: not Agents, whatever that
// is set to), never on the task's old fields; delivery into the conversation it came from is a turn of that
// conversation (#1518): its launcher, still none of the task's old fields, the task's permission mode.
func TestScheduledExecutionUsesMainConversationCategoryAndNoOldOverrides(t *testing.T) {
	h, _ := modelTestHub(t)
	n := newAssistantNode(t, h)
	ctx := context.Background()
	if err := h.saveModelRecord(ctx, "assistant", 0, ModelBinding{Provider: "launcher.claude", Model: "category-model", Effort: "high"}); err != nil {
		t.Fatal(err)
	}
	if err := h.saveModelRecord(ctx, "agent.new", 0, ModelBinding{Provider: "launcher.agents", Model: "agents-model"}); err != nil {
		t.Fatal(err)
	}
	if err := h.saveModelRecord(ctx, "schedule", 0, ModelBinding{Provider: "launcher.old-schedule"}); err != nil {
		t.Fatal(err)
	}
	setTasks(h, Task{ID: "one", Title: "任务", Prompt: "执行任务", Launcher: "old-task", Model: "old-model", Effort: "low", PermissionMode: "bypassPermissions", Repeat: repeatInterval, EveryMinutes: 30, Enabled: true})
	for i := 0; i < 2; i++ {
		if _, err := runNow(t, h, "one"); err != nil {
			t.Fatal(err)
		}
		p := n.lastSend(t)
		if p["launcher"] != "claude" || p["model"] != "category-model" || p["effort"] != "high" || p["permission_mode"] != "bypassPermissions" {
			t.Fatalf("task creation/resume kept an old override or changed permission: %+v", p)
		}
	}
	n.m.mu.Lock()
	n.m.sessions = []proto.Session{{SessionID: "source", Launcher: "old-source"}}
	n.m.mu.Unlock()
	s := h.loadSchedule(ctx)
	s.mu.Lock()
	s.tasks[0].SessionID, s.tasks[0].MachineID = "source", "server"
	s.mu.Unlock()
	if _, err := runNow(t, h, "one"); err != nil {
		t.Fatal(err)
	}
	p := n.lastSend(t)
	if p["session_id"] != "source" || p["launcher"] != "old-source" || p["model"] != nil || p["effort"] != nil || p["permission_mode"] != "bypassPermissions" {
		t.Fatalf("delivery into the source conversation changed its launcher or kept an old override: %+v", p)
	}
}

func TestTaskEditsCannotCreateModelOverrides(t *testing.T) {
	old := Task{ID: "one", Launcher: "old", Model: "old-model", Effort: "high"}
	in := Task{Title: "任务", Launcher: "requested", Model: "requested-model", Effort: "low", PermissionMode: "bypassPermissions"}
	task := editTask(old, in, old.CreatedAt)
	if task.Launcher != "" || task.Model != "" || task.Effort != "" || task.PermissionMode != "bypassPermissions" {
		t.Fatalf("task edit created a hidden model override: %+v", task)
	}
}
