package db

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// A supervised task's ledger is written whole, read back, and goes with its todo.
func TestTodoSupervision(t *testing.T) {
	d, ctx := open(t), context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := d.CreateTodo(ctx, TodoRow{ID: "t1", Title: "部署", CreatedAt: now, UpdatedAt: now, Origin: "main"}); err != nil {
		t.Fatal(err)
	}
	r := TodoSupervisionRow{TodoID: "t1", Supervisor: "main", DoneWhen: []string{"PR 合并", "线上验证"}, ExpectMinutes: 30, UpdatedAt: now}
	if err := d.PutTodoSupervision(ctx, r); err != nil {
		t.Fatal(err)
	}
	r.State, r.Round, r.Reviews = "rework", 1, json.RawMessage(`[{"verdict":"rework"}]`)
	if err := d.PutTodoSupervision(ctx, r); err != nil {
		t.Fatal(err)
	}
	list, err := d.ListTodoSupervision(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	got := list[0]
	if got.Supervisor != "main" || len(got.DoneWhen) != 2 || got.DoneWhen[1] != "线上验证" || got.ExpectMinutes != 30 ||
		got.State != "rework" || got.Round != 1 || !json.Valid(got.Reviews) || !got.UpdatedAt.Equal(now) {
		t.Fatalf("row = %+v (%s)", got, got.Reviews)
	}
	if err := d.PutTodoSupervision(ctx, TodoSupervisionRow{TodoID: "nope", Supervisor: "main", UpdatedAt: now}); !errors.Is(err, ErrTodoNotFound) {
		t.Fatalf("missing todo: %v", err)
	}
	row, ok, err := d.ReviewDoneTodo(ctx, "t1", "验收通过", now)
	if err != nil || !ok || row.DoneBy != DoneByReview || row.DoneReason != "验收通过" || row.DoneAt == nil {
		t.Fatalf("ReviewDoneTodo = %+v, %v, %v", row, ok, err)
	}
	if _, ok, err := d.ReviewDoneTodo(ctx, "t1", "again", now); err != nil || ok {
		t.Fatalf("done twice: %v, %v", ok, err)
	}
	if _, _, err := d.ReviewDoneTodo(ctx, "nope", "x", now); !errors.Is(err, ErrTodoNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if err := d.DeleteTodo(ctx, "t1"); err != nil {
		t.Fatal(err)
	}
	if list, err := d.ListTodoSupervision(ctx); err != nil || len(list) != 0 {
		t.Fatalf("after delete: %+v, %v", list, err)
	}
}
