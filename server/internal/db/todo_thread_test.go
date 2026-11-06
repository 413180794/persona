package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A todo is bound to one thread, a thread to one todo; the binding goes with the todo.
func TestTodoThread(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	base := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	for _, id := range []string{"todo_a", "todo_b"} {
		if err := d.CreateTodo(ctx, TodoRow{ID: id, Title: id, CreatedAt: base, UpdatedAt: base}); err != nil {
			t.Fatal(err)
		}
	}
	th := TodoThread{Channel: "feishu", ChatID: "oc_test", ThreadID: "omt_test_1", Title: "Release 1.3"}

	if err := d.SetTodoThread(ctx, "todo_a", th, base.Add(time.Minute)); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if got, _ := d.GetTodo(ctx, "todo_a"); !got.UpdatedAt.Equal(base.Add(time.Minute)) {
		t.Fatalf("updated_at after bind = %v", got.UpdatedAt)
	}
	all, err := d.ListTodoThreads(ctx)
	if err != nil || len(all) != 1 || all["todo_a"] != th {
		t.Fatalf("list = %+v, %v", all, err)
	}
	if id, err := d.TodoOfThread(ctx, "feishu", "oc_test", "omt_test_1"); err != nil || id != "todo_a" {
		t.Fatalf("todo of thread = %q, %v", id, err)
	}
	if _, err := d.TodoOfThread(ctx, "feishu", "oc_test", "om_other"); !errors.Is(err, ErrTodoNotFound) {
		t.Fatalf("an unbound thread: %v", err)
	}

	// Another todo cannot take it; nothing changes for either.
	if err := d.SetTodoThread(ctx, "todo_b", th, base.Add(2*time.Minute)); !errors.Is(err, ErrThreadTaken) {
		t.Fatalf("taken thread: %v", err)
	}
	if got, _ := d.GetTodo(ctx, "todo_b"); !got.UpdatedAt.Equal(base) {
		t.Fatalf("updated_at after the refused bind = %v", got.UpdatedAt)
	}
	// The same chat with another thread, or the same thread id on another channel, is another thread.
	if err := d.SetTodoThread(ctx, "todo_b", TodoThread{Channel: "feishu", ChatID: "oc_test", ThreadID: "omt_test_2"}, base.Add(2*time.Minute)); err != nil {
		t.Fatalf("another thread of the chat: %v", err)
	}
	if err := d.SetTodoThread(ctx, "todo_b", TodoThread{Channel: "other", ChatID: "oc_test", ThreadID: "omt_test_1"}, base.Add(3*time.Minute)); err != nil {
		t.Fatalf("the same ids on another channel: %v", err)
	}

	// Rebinding replaces: the first thread is free again, the same todo can bind it back.
	if err := d.SetTodoThread(ctx, "todo_a", TodoThread{Channel: "feishu", ChatID: "oc_test", ThreadID: "omt_test_3"}, base.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := d.SetTodoThread(ctx, "todo_b", th, base.Add(5*time.Minute)); err != nil {
		t.Fatalf("a freed thread: %v", err)
	}

	// Unbinding: only a bound todo is touched.
	if changed, err := d.ClearTodoThread(ctx, "todo_a", base.Add(6*time.Minute)); err != nil || !changed {
		t.Fatalf("unbind = %v, %v", changed, err)
	}
	if changed, err := d.ClearTodoThread(ctx, "todo_a", base.Add(7*time.Minute)); err != nil || changed {
		t.Fatalf("unbind again = %v, %v", changed, err)
	}
	if got, _ := d.GetTodo(ctx, "todo_a"); !got.UpdatedAt.Equal(base.Add(6 * time.Minute)) {
		t.Fatalf("updated_at after unbinding twice = %v", got.UpdatedAt)
	}

	// An unknown todo.
	if err := d.SetTodoThread(ctx, "todo_gone", th, base); !errors.Is(err, ErrTodoNotFound) {
		t.Fatalf("bind unknown: %v", err)
	}
	if _, err := d.ClearTodoThread(ctx, "todo_gone", base); !errors.Is(err, ErrTodoNotFound) {
		t.Fatalf("unbind unknown: %v", err)
	}

	// Deleting the todo takes the binding with it: the thread can be bound again, and nothing finds it.
	if err := d.DeleteTodo(ctx, "todo_b"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.TodoOfThread(ctx, "feishu", "oc_test", "omt_test_1"); !errors.Is(err, ErrTodoNotFound) {
		t.Fatalf("after deleting the todo: %v", err)
	}
	if all, _ := d.ListTodoThreads(ctx); len(all) != 0 {
		t.Fatalf("list after delete = %+v", all)
	}
	if err := d.SetTodoThread(ctx, "todo_a", th, base.Add(8*time.Minute)); err != nil {
		t.Fatalf("bind after the holder was deleted: %v", err)
	}

	// A binding without one of the three keys is refused by the table itself: the unique index would read the
	// missing one as NULL, and NULLs never collide.
	for _, raw := range []string{`{}`, `{"channel":"feishu","chat_id":"oc_test"}`, `{"chat_id":"oc_test","thread_id":"omt_x"}`, `{"channel":"feishu","thread_id":"omt_x"}`} {
		if _, err := d.pool.Exec(ctx, `UPDATE todos SET thread = $2::jsonb WHERE id = $1`, "todo_a", raw); err == nil {
			t.Fatalf("the table took the binding %s", raw)
		}
	}
}
