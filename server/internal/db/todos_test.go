package db

import (
	"context"
	"testing"
	"time"
)

func TestTodos(t *testing.T) {
	d, ctx := open(t), context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	// 1. CreateTodo and GetTodo
	t1 := TodoRow{
		ID:        "t1",
		Title:     "天气 App 发 1.3 版",
		Notes:     "发布说明",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := d.CreateTodo(ctx, t1); err != nil {
		t.Fatalf("CreateTodo: %v", err)
	}

	got, err := d.GetTodo(ctx, "t1")
	if err != nil {
		t.Fatalf("GetTodo: %v", err)
	}
	if got.Title != t1.Title || got.Notes != t1.Notes || got.DoneAt != nil {
		t.Fatalf("unexpected got: %+v", got)
	}

	// 2. ListTodos
	t2 := TodoRow{
		ID:        "t2",
		Title:     "照片同步",
		Notes:     "HEIC 支持",
		CreatedAt: now.Add(time.Minute),
		UpdatedAt: now.Add(time.Minute),
	}
	if err := d.CreateTodo(ctx, t2); err != nil {
		t.Fatalf("CreateTodo: %v", err)
	}
	list, err := d.ListTodos(ctx)
	if err != nil {
		t.Fatalf("ListTodos: %v", err)
	}
	if len(list) != 2 || list[0].ID != "t2" || list[1].ID != "t1" {
		t.Fatalf("unexpected list order: %+v", list)
	}

	// 3. UpdateTodo (PATCH semantics)
	newTitle := "天气 App 发 1.3.1 版"
	upTime := now.Add(2 * time.Minute)
	up, err := d.UpdateTodo(ctx, "t1", &newTitle, nil, nil, upTime)
	if err != nil {
		t.Fatalf("UpdateTodo title: %v", err)
	}
	if up.Title != newTitle || up.Notes != t1.Notes || up.DoneAt != nil {
		t.Fatalf("unexpected updated todo: %+v", up)
	}

	// Mark done
	doneTrue := true
	doneTime := now.Add(3 * time.Minute)
	up, err = d.UpdateTodo(ctx, "t1", nil, nil, &doneTrue, doneTime)
	if err != nil {
		t.Fatalf("UpdateTodo done: %v", err)
	}
	if up.DoneAt == nil || !up.DoneAt.Equal(doneTime) {
		t.Fatalf("expected DoneAt to be set, got %+v", up)
	}

	// Reopen
	doneFalse := false
	reopenTime := now.Add(4 * time.Minute)
	up, err = d.UpdateTodo(ctx, "t1", nil, nil, &doneFalse, reopenTime)
	if err != nil {
		t.Fatalf("UpdateTodo reopen: %v", err)
	}
	if up.DoneAt != nil {
		t.Fatalf("expected DoneAt to be nil, got %+v", up)
	}

	// 4. LinkTodoAgent and transfer
	l1Time := now.Add(5 * time.Minute)
	_, err = d.LinkTodoAgent(ctx, "t1", "macbook", "s1", "测试 Agent", "/code/weather", "claude", "", l1Time)
	if err != nil {
		t.Fatalf("LinkTodoAgent: %v", err)
	}

	agents, err := d.ListAllTodoAgents(ctx)
	if err != nil {
		t.Fatalf("ListAllTodoAgents: %v", err)
	}
	if len(agents) != 1 || agents[0].TodoID != "t1" || agents[0].ReleasedAt != nil {
		t.Fatalf("unexpected agents: %+v", agents)
	}

	// Move same agent to t2: t1's link should become released
	l2Time := now.Add(6 * time.Minute)
	released, err := d.LinkTodoAgent(ctx, "t2", "macbook", "s1", "测试 Agent", "/code/weather", "claude", "", l2Time)
	if err != nil {
		t.Fatalf("LinkTodoAgent move: %v", err)
	}
	if len(released) != 1 || released[0] != "t1" {
		t.Fatalf("expected released [t1], got %v", released)
	}

	agents, err = d.ListAllTodoAgents(ctx)
	if err != nil {
		t.Fatalf("ListAllTodoAgents: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("expected 2 agent rows, got %d", len(agents))
	}
	var t1Link, t2Link *TodoAgentRow
	for i := range agents {
		if agents[i].TodoID == "t1" {
			t1Link = &agents[i]
		} else if agents[i].TodoID == "t2" {
			t2Link = &agents[i]
		}
	}
	if t1Link == nil || t1Link.ReleasedAt == nil {
		t.Fatalf("expected t1Link to be released, got %+v", t1Link)
	}
	if t2Link == nil || t2Link.ReleasedAt != nil {
		t.Fatalf("expected t2Link to be active, got %+v", t2Link)
	}

	// Re-link to t1: revives t1 link (released_at becomes nil), releases t2
	l3Time := now.Add(7 * time.Minute)
	released, err = d.LinkTodoAgent(ctx, "t1", "macbook", "s1", "测试 Agent", "/code/weather", "claude", "", l3Time)
	if err != nil {
		t.Fatalf("LinkTodoAgent revive: %v", err)
	}
	if len(released) != 1 || released[0] != "t2" {
		t.Fatalf("expected released [t2], got %v", released)
	}
	agents, err = d.ListAllTodoAgents(ctx)
	if err != nil {
		t.Fatalf("ListAllTodoAgents: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("expected still 2 agent rows, got %d", len(agents))
	}

	// Cannot link to completed todo
	_, err = d.UpdateTodo(ctx, "t2", nil, nil, &doneTrue, now.Add(8*time.Minute))
	if err != nil {
		t.Fatalf("UpdateTodo done: %v", err)
	}
	_, err = d.LinkTodoAgent(ctx, "t2", "macbook", "s2", "Agent 2", "/code", "claude", "", now.Add(9*time.Minute))
	if err != ErrTodoCompleted {
		t.Fatalf("expected ErrTodoCompleted, got %v", err)
	}

	// UpdateTodoAgentSnapshot
	if err := d.UpdateTodoAgentSnapshot(ctx, "macbook", "s1", "更新后的 Agent 标题", "some start error"); err != nil {
		t.Fatalf("UpdateTodoAgentSnapshot: %v", err)
	}
	agents, _ = d.ListAllTodoAgents(ctx)
	for _, a := range agents {
		if a.TodoID == "t1" && a.SessionID == "s1" {
			if a.Title != "更新后的 Agent 标题" || a.StartError != "some start error" {
				t.Fatalf("snapshot not updated: %+v", a)
			}
		}
	}

	// UnlinkTodoAgent
	if err := d.UnlinkTodoAgent(ctx, "t1", "macbook", "s1"); err != nil {
		t.Fatalf("UnlinkTodoAgent: %v", err)
	}
	agents, _ = d.ListAllTodoAgents(ctx)
	for _, a := range agents {
		if a.TodoID == "t1" && a.SessionID == "s1" {
			t.Fatalf("expected t1 s1 to be unlinked")
		}
	}

	// DeleteTodo cascades to links
	_, _ = d.LinkTodoAgent(ctx, "t1", "macbook", "s3", "A3", "/cwd", "claude", "", now.Add(10*time.Minute))
	if err := d.DeleteTodo(ctx, "t1"); err != nil {
		t.Fatalf("DeleteTodo: %v", err)
	}
	if _, err := d.GetTodo(ctx, "t1"); err != ErrTodoNotFound {
		t.Fatalf("expected ErrTodoNotFound, got %v", err)
	}
	agents, _ = d.ListAllTodoAgents(ctx)
	for _, a := range agents {
		if a.TodoID == "t1" {
			t.Fatalf("expected links for t1 to be deleted via cascade")
		}
	}
}
