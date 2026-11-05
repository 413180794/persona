package db

import (
	"context"
	"testing"
	"time"
)

func TestUnlinkTodoAgentParentSemantics(t *testing.T) {
	for _, missing := range []bool{false, true} {
		name := "completed todo"
		if missing {
			name = "missing todo"
		}
		t.Run(name, func(t *testing.T) {
			d := open(t)
			ctx := context.Background()
			now := time.Now().UTC()
			if !missing {
				if err := d.CreateTodo(ctx, TodoRow{ID: "parent", Title: "Example task", CreatedAt: now, UpdatedAt: now}); err != nil {
					t.Fatal(err)
				}
				if _, err := d.LinkTodoAgent(ctx, "parent", "machine-1", "session-1", "", "", "", "", now); err != nil {
					t.Fatal(err)
				}
				done := true
				if _, err := d.UpdateTodo(ctx, "parent", nil, nil, &done, now); err != nil {
					t.Fatal(err)
				}
			}
			err := d.UnlinkTodoAgent(ctx, "parent", "machine-1", "session-1")
			if missing {
				if err != ErrTodoNotFound {
					t.Fatalf("missing parent: %v", err)
				}
			} else if err != nil {
				t.Fatalf("completed parent cannot unlink: %v", err)
			}
			links, err := d.ListAllTodoAgents(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if missing {
				if len(links) != 0 {
					t.Fatalf("missing parent created a link: %+v", links)
				}
			} else if len(links) != 1 || links[0].ReleasedAt == nil || links[0].DismissedAt == nil {
				t.Fatalf("completed parent did not soft unlink: %+v", links)
			}
		})
	}
}

func TestTodoAgentMembershipSerializesBeforeParentLocks(t *testing.T) {
	d := open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Now().UTC()
	for _, id := range []string{"left", "right"} {
		if err := d.CreateTodo(ctx, TodoRow{ID: id, Title: "Example task", CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	for _, todo := range []string{"left", "right"} {
		var id string
		if err := tx.QueryRow(ctx, `SELECT id FROM todos WHERE id = $1 FOR UPDATE`, todo).Scan(&id); err != nil {
			t.Fatal(err)
		}
	}
	var parentPID int
	if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&parentPID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 2)
	go func() {
		_, err := d.LinkTodoAgent(ctx, "left", "machine-1", "session-1", "", "", "", "", now)
		result <- err
	}()
	go func() {
		_, _, err := d.AutoLinkTodoAgent(ctx, "right", "machine-1", "session-1", "", "", "", "", "chat", "example-chat", now)
		result <- err
	}()
	finished := 0
	defer func() {
		_ = tx.Rollback(context.Background())
		for finished < 2 {
			select {
			case <-result:
				finished++
			case <-ctx.Done():
				return
			}
		}
	}()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting int
		if err := d.pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND cardinality(pg_blocking_pids(pid)) > 0`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting == 2 {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("membership transactions did not reach their lock waits")
		}
	}
	var parents int
	if err := d.pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
		WHERE datname = current_database() AND $1::int = ANY(pg_blocking_pids(pid))`, parentPID).Scan(&parents); err != nil {
		t.Fatal(err)
	}
	if parents != 1 {
		t.Fatalf("%d membership transactions locked parents before serializing the same agent", parents)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for finished < 2 {
		select {
		case err := <-result:
			finished++
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("membership transactions did not finish")
		}
	}
	links, err := d.ListAllTodoAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	active := 0
	for _, link := range links {
		if link.ReleasedAt == nil && link.DismissedAt == nil {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("%d active links after competing manual and automatic membership", active)
	}
}

func TestUnlinkTodoAgentLocksTodoBeforeAgent(t *testing.T) {
	d := open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Now().UTC()
	if err := d.CreateTodo(ctx, TodoRow{ID: "lock-order", Title: "Example task", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.LinkTodoAgent(ctx, "lock-order", "machine-1", "session-1", "", "", "", "", now); err != nil {
		t.Fatal(err)
	}

	// An automatic link locks the parent before touching its agent row.
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	var id string
	if err := tx.QueryRow(ctx, `SELECT id FROM todos WHERE id = $1 FOR UPDATE`, "lock-order").Scan(&id); err != nil {
		t.Fatal(err)
	}
	var parentPID int
	if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&parentPID); err != nil {
		t.Fatal(err)
	}
	removed := make(chan error, 1)
	go func() { removed <- d.UnlinkTodoAgent(ctx, "lock-order", "machine-1", "session-1") }()
	finished := false
	defer func() {
		_ = tx.Rollback(context.Background())
		if !finished {
			select {
			case <-removed:
			case <-ctx.Done():
			}
		}
	}()

	// Wait for the other transaction's real lock wait, not a guessed delay.
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := d.pool.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity WHERE datname = current_database()
			AND $1::int = ANY(pg_blocking_pids(pid)))`, parentPID).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("unlink did not wait for the parent row")
		}
	}

	// A blocked unlink must not hold the row the parent owner needs next.
	var row int
	if err := tx.QueryRow(ctx, `SELECT 1 FROM todo_agents
		WHERE todo_id = $1 AND machine_id = $2 AND session_id = $3 FOR UPDATE NOWAIT`,
		"lock-order", "machine-1", "session-1").Scan(&row); err != nil {
		t.Fatalf("unlink locked the agent before acquiring its parent: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-removed:
		finished = true
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("unlink did not finish after the parent was released")
	}
	links, err := d.ListAllTodoAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].ReleasedAt == nil || links[0].DismissedAt == nil {
		t.Fatalf("soft unlink changed: %+v", links)
	}
}
