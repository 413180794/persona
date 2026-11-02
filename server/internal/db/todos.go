package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrTodoNotFound  = errors.New("todo not found")
	ErrTodoCompleted = errors.New("todo is already completed")
)

type TodoRow struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Notes     string     `json:"notes"`
	DoneAt    *time.Time `json:"done_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type TodoAgentRow struct {
	TodoID     string     `json:"todo_id"`
	MachineID  string     `json:"machine_id"`
	SessionID  string     `json:"session_id"`
	LinkedAt   time.Time  `json:"linked_at"`
	ReleasedAt *time.Time `json:"released_at"`
	Title      string     `json:"title"`
	Cwd        string     `json:"cwd"`
	Agent      string     `json:"agent"`
	StartError string     `json:"start_error"`
}

// ListTodos returns all todos ordered by created_at DESC.
func (d *DB) ListTodos(ctx context.Context) ([]TodoRow, error) {
	rows, err := d.pool.Query(ctx, `SELECT id, title, notes, done_at, created_at, updated_at FROM todos ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TodoRow
	for rows.Next() {
		var r TodoRow
		if err := rows.Scan(&r.ID, &r.Title, &r.Notes, &r.DoneAt, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListAllTodoAgents returns all agent links.
func (d *DB) ListAllTodoAgents(ctx context.Context) ([]TodoAgentRow, error) {
	rows, err := d.pool.Query(ctx, `SELECT todo_id, machine_id, session_id, linked_at, released_at, title, cwd, agent, start_error FROM todo_agents`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TodoAgentRow
	for rows.Next() {
		var r TodoAgentRow
		if err := rows.Scan(&r.TodoID, &r.MachineID, &r.SessionID, &r.LinkedAt, &r.ReleasedAt, &r.Title, &r.Cwd, &r.Agent, &r.StartError); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetTodo returns one todo by id, or ErrTodoNotFound.
func (d *DB) GetTodo(ctx context.Context, id string) (*TodoRow, error) {
	var r TodoRow
	err := d.pool.QueryRow(ctx, `SELECT id, title, notes, done_at, created_at, updated_at FROM todos WHERE id = $1`, id).
		Scan(&r.ID, &r.Title, &r.Notes, &r.DoneAt, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTodoNotFound
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// CreateTodo inserts a new todo.
func (d *DB) CreateTodo(ctx context.Context, t TodoRow) error {
	_, err := d.pool.Exec(ctx, `INSERT INTO todos (id, title, notes, done_at, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		t.ID, t.Title, t.Notes, t.DoneAt, t.CreatedAt, t.UpdatedAt)
	return err
}

// UpdateTodo updates only the fields that are non-nil.
func (d *DB) UpdateTodo(ctx context.Context, id string, title *string, notes *string, done *bool, updatedAt time.Time) (*TodoRow, error) {
	conn, err := d.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	var updated TodoRow
	err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		var cur TodoRow
		err := tx.QueryRow(ctx, `SELECT id, title, notes, done_at, created_at, updated_at FROM todos WHERE id = $1 FOR UPDATE`, id).
			Scan(&cur.ID, &cur.Title, &cur.Notes, &cur.DoneAt, &cur.CreatedAt, &cur.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTodoNotFound
		}
		if err != nil {
			return err
		}

		if title != nil {
			cur.Title = *title
		}
		if notes != nil {
			cur.Notes = *notes
		}
		if done != nil {
			if *done {
				if cur.DoneAt == nil {
					cur.DoneAt = &updatedAt
				}
			} else {
				cur.DoneAt = nil
			}
		}
		cur.UpdatedAt = updatedAt

		_, err = tx.Exec(ctx, `UPDATE todos SET title = $2, notes = $3, done_at = $4, updated_at = $5 WHERE id = $1`,
			cur.ID, cur.Title, cur.Notes, cur.DoneAt, cur.UpdatedAt)
		if err != nil {
			return err
		}
		updated = cur
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

// DeleteTodo deletes a todo by id. Cascading deletes remove its todo_agents.
func (d *DB) DeleteTodo(ctx context.Context, id string) error {
	tag, err := d.pool.Exec(ctx, `DELETE FROM todos WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrTodoNotFound
	}
	return nil
}

// LinkTodoAgent links an agent to a todo in a transaction:
// 1. Checks that the target todo exists and is not completed.
// 2. Marks any active link for (machine_id, session_id) on other todos as released.
// 3. Inserts or unreleases the link on the target todo with updated snapshot.
// Returns the list of affected other todo IDs whose active link was released.
func (d *DB) LinkTodoAgent(ctx context.Context, todoID, machineID, sessionID, title, cwd, agent, startError string, linkedAt time.Time) ([]string, error) {
	conn, err := d.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	var releasedTodoIDs []string
	err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		var targetDoneAt *time.Time
		err := tx.QueryRow(ctx, `SELECT done_at FROM todos WHERE id = $1 FOR UPDATE`, todoID).Scan(&targetDoneAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTodoNotFound
		}
		if err != nil {
			return err
		}
		if targetDoneAt != nil {
			return ErrTodoCompleted
		}

		// Find other active links for this agent and release them.
		rows, err := tx.Query(ctx, `SELECT todo_id FROM todo_agents WHERE machine_id = $1 AND session_id = $2 AND released_at IS NULL AND todo_id != $3`,
			machineID, sessionID, todoID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var oid string
			if err := rows.Scan(&oid); err != nil {
				rows.Close()
				return err
			}
			releasedTodoIDs = append(releasedTodoIDs, oid)
		}
		rows.Close()

		if len(releasedTodoIDs) > 0 {
			_, err = tx.Exec(ctx, `UPDATE todo_agents SET released_at = $3 WHERE machine_id = $1 AND session_id = $2 AND released_at IS NULL AND todo_id != $4`,
				machineID, sessionID, linkedAt, todoID)
			if err != nil {
				return err
			}
			for _, oid := range releasedTodoIDs {
				if _, err = tx.Exec(ctx, `UPDATE todos SET updated_at = $2 WHERE id = $1`, oid, linkedAt); err != nil {
					return err
				}
			}
		}

		// Upsert link on target todo.
		_, err = tx.Exec(ctx, `INSERT INTO todo_agents (todo_id, machine_id, session_id, linked_at, released_at, title, cwd, agent, start_error)
			VALUES ($1, $2, $3, $4, NULL, $5, $6, $7, $8)
			ON CONFLICT (todo_id, machine_id, session_id) DO UPDATE SET
				linked_at = $4,
				released_at = NULL,
				title = CASE WHEN $5 != '' THEN $5 ELSE todo_agents.title END,
				cwd = CASE WHEN $6 != '' THEN $6 ELSE todo_agents.cwd END,
				agent = CASE WHEN $7 != '' THEN $7 ELSE todo_agents.agent END,
				start_error = $8`,
			todoID, machineID, sessionID, linkedAt, title, cwd, agent, startError)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `UPDATE todos SET updated_at = $2 WHERE id = $1`, todoID, linkedAt)
		return err
	})
	if err != nil {
		return nil, err
	}
	return releasedTodoIDs, nil
}

// UnlinkTodoAgent removes the agent link row from a todo.
func (d *DB) UnlinkTodoAgent(ctx context.Context, todoID, machineID, sessionID string) error {
	conn, err := d.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	return pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		var exists bool
		err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM todos WHERE id = $1)`, todoID).Scan(&exists)
		if err != nil {
			return err
		}
		if !exists {
			return ErrTodoNotFound
		}
		_, err = tx.Exec(ctx, `DELETE FROM todo_agents WHERE todo_id = $1 AND machine_id = $2 AND session_id = $3`,
			todoID, machineID, sessionID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE todos SET updated_at = now() WHERE id = $1`, todoID)
		return err
	})
}

// UpdateTodoAgentSnapshot updates the title and start_error snapshot of an active agent link.
func (d *DB) UpdateTodoAgentSnapshot(ctx context.Context, machineID, sessionID, title, startError string) error {
	_, err := d.pool.Exec(ctx, `UPDATE todo_agents SET
		title = CASE WHEN $3 != '' THEN $3 ELSE title END,
		start_error = $4
		WHERE machine_id = $1 AND session_id = $2 AND released_at IS NULL`,
		machineID, sessionID, title, startError)
	return err
}
