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
	// Origin is the assistant conversation that opened it as a task, PlanID the plan it came from (#930); empty
	// for a todo made by hand.
	Origin string `json:"origin"`
	PlanID string `json:"plan_id"`
	// StartError: the task's agent did not start, and why; such a todo is done (#1077). Empty otherwise.
	StartError string `json:"start_error"`
}

type TodoSpaceRow struct {
	TodoID    string    `json:"todo_id"`
	MachineID string    `json:"machine_id"`
	Space     string    `json:"space"`
	LinkedAt  time.Time `json:"linked_at"`
}

type TodoAgentRow struct {
	TodoID      string     `json:"todo_id"`
	MachineID   string     `json:"machine_id"`
	SessionID   string     `json:"session_id"`
	LinkedAt    time.Time  `json:"linked_at"`
	ReleasedAt  *time.Time `json:"released_at"`
	Title       string     `json:"title"`
	Cwd         string     `json:"cwd"`
	Agent       string     `json:"agent"`
	StartError  string     `json:"start_error"`
	Source      string     `json:"source"`
	Via         string     `json:"via"`
	DismissedAt *time.Time `json:"dismissed_at"`
}

// ListTodos returns all todos ordered by created_at DESC.
func (d *DB) ListTodos(ctx context.Context) ([]TodoRow, error) {
	rows, err := d.pool.Query(ctx, `SELECT id, title, notes, done_at, created_at, updated_at, origin, plan_id, start_error FROM todos ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TodoRow
	for rows.Next() {
		var r TodoRow
		if err := rows.Scan(&r.ID, &r.Title, &r.Notes, &r.DoneAt, &r.CreatedAt, &r.UpdatedAt, &r.Origin, &r.PlanID, &r.StartError); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListAllTodoSpaces returns all space links.
func (d *DB) ListAllTodoSpaces(ctx context.Context) ([]TodoSpaceRow, error) {
	rows, err := d.pool.Query(ctx, `SELECT todo_id, machine_id, space, linked_at FROM todo_spaces`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TodoSpaceRow
	for rows.Next() {
		var r TodoSpaceRow
		if err := rows.Scan(&r.TodoID, &r.MachineID, &r.Space, &r.LinkedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListAllTodoAgents returns all agent links.
func (d *DB) ListAllTodoAgents(ctx context.Context) ([]TodoAgentRow, error) {
	rows, err := d.pool.Query(ctx, `SELECT todo_id, machine_id, session_id, linked_at, released_at, title, cwd, agent, start_error, source, via, dismissed_at FROM todo_agents`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TodoAgentRow
	for rows.Next() {
		var r TodoAgentRow
		if err := rows.Scan(&r.TodoID, &r.MachineID, &r.SessionID, &r.LinkedAt, &r.ReleasedAt, &r.Title, &r.Cwd, &r.Agent, &r.StartError, &r.Source, &r.Via, &r.DismissedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetTodo returns one todo by id, or ErrTodoNotFound.
func (d *DB) GetTodo(ctx context.Context, id string) (*TodoRow, error) {
	var r TodoRow
	err := d.pool.QueryRow(ctx, `SELECT id, title, notes, done_at, created_at, updated_at, origin, plan_id, start_error FROM todos WHERE id = $1`, id).
		Scan(&r.ID, &r.Title, &r.Notes, &r.DoneAt, &r.CreatedAt, &r.UpdatedAt, &r.Origin, &r.PlanID, &r.StartError)
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
	_, err := d.pool.Exec(ctx, `INSERT INTO todos (id, title, notes, done_at, created_at, updated_at, origin, plan_id, start_error) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		t.ID, t.Title, t.Notes, t.DoneAt, t.CreatedAt, t.UpdatedAt, t.Origin, t.PlanID, t.StartError)
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
		err := tx.QueryRow(ctx, `SELECT id, title, notes, done_at, created_at, updated_at, origin, plan_id, start_error FROM todos WHERE id = $1 FOR UPDATE`, id).
			Scan(&cur.ID, &cur.Title, &cur.Notes, &cur.DoneAt, &cur.CreatedAt, &cur.UpdatedAt, &cur.Origin, &cur.PlanID, &cur.StartError)
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
				cur.StartError = "" // reopened: no longer failed
			}
		}
		cur.UpdatedAt = updatedAt

		_, err = tx.Exec(ctx, `UPDATE todos SET title = $2, notes = $3, done_at = $4, updated_at = $5, start_error = $6 WHERE id = $1`,
			cur.ID, cur.Title, cur.Notes, cur.DoneAt, cur.UpdatedAt, cur.StartError)
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

// FailTodo ends a todo whose agent did not start: done (unless it already was) with why (#1077).
func (d *DB) FailTodo(ctx context.Context, id, startError string, at time.Time) (*TodoRow, error) {
	var r TodoRow
	err := d.pool.QueryRow(ctx, `UPDATE todos SET done_at = COALESCE(done_at, $2), start_error = $3, updated_at = $2 WHERE id = $1
		RETURNING id, title, notes, done_at, created_at, updated_at, origin, plan_id, start_error`, id, at, startError).
		Scan(&r.ID, &r.Title, &r.Notes, &r.DoneAt, &r.CreatedAt, &r.UpdatedAt, &r.Origin, &r.PlanID, &r.StartError)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTodoNotFound
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
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

// LinkTodoSpace links a project space to a todo.
// If the space is already linked to another todo, that link is removed and its space-sourced agents are removed.
// Returns the old todo ID if one was unlinked.
func (d *DB) LinkTodoSpace(ctx context.Context, todoID, machineID, space string, linkedAt time.Time) (string, error) {
	conn, err := d.pool.Acquire(ctx)
	if err != nil {
		return "", err
	}
	defer conn.Release()

	var oldTodoID string
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

		// Check if space was linked to another todo
		var curTodoID string
		err = tx.QueryRow(ctx, `SELECT todo_id FROM todo_spaces WHERE machine_id = $1 AND space = $2 FOR UPDATE`, machineID, space).Scan(&curTodoID)
		if err == nil {
			if curTodoID != todoID {
				oldTodoID = curTodoID
				if _, err := tx.Exec(ctx, `DELETE FROM todo_spaces WHERE machine_id = $1 AND space = $2`, machineID, space); err != nil {
					return err
				}
				viaKey := machineID + "/" + space
				if _, err := tx.Exec(ctx, `DELETE FROM todo_agents WHERE todo_id = $1 AND source = 'space' AND via = $2`, oldTodoID, viaKey); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `UPDATE todos SET updated_at = $2 WHERE id = $1`, oldTodoID, linkedAt); err != nil {
					return err
				}
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		// Insert or update space link
		_, err = tx.Exec(ctx, `INSERT INTO todo_spaces (todo_id, machine_id, space, linked_at)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (machine_id, space) DO UPDATE SET
				todo_id = $1,
				linked_at = $4`,
			todoID, machineID, space, linkedAt)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `UPDATE todos SET updated_at = $2 WHERE id = $1`, todoID, linkedAt)
		return err
	})
	if err != nil {
		return "", err
	}
	return oldTodoID, nil
}

// UnlinkTodoSpace removes a space link and physically deletes source=space agents linked via this space.
func (d *DB) UnlinkTodoSpace(ctx context.Context, todoID, machineID, space string) error {
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

		tag, err := tx.Exec(ctx, `DELETE FROM todo_spaces WHERE todo_id = $1 AND machine_id = $2 AND space = $3`, todoID, machineID, space)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrTodoNotFound
		}

		viaKey := machineID + "/" + space
		if _, err := tx.Exec(ctx, `DELETE FROM todo_agents WHERE todo_id = $1 AND source = 'space' AND via = $2`, todoID, viaKey); err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `UPDATE todos SET updated_at = now() WHERE id = $1`, todoID)
		return err
	})
}

// RenameTodoSpace migrates todo_spaces and todo_agents via when a space is renamed via hub.
// If newSpace is already linked to another todo (unique constraint conflict), the old space's
// link is voided (deleted) rather than migrating, returning (voided: true, err: nil).
func (d *DB) RenameTodoSpace(ctx context.Context, machineID, oldSpace, newSpace string) (bool, error) {
	conn, err := d.pool.Acquire(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Release()

	var voided bool
	err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		// Check if newSpace already exists in todo_spaces for this machine.
		var existingTodoID string
		err := tx.QueryRow(ctx, `SELECT todo_id FROM todo_spaces WHERE machine_id = $1 AND space = $2 FOR UPDATE`, machineID, newSpace).Scan(&existingTodoID)
		if err == nil {
			// Conflict: newSpace is already linked to existingTodoID.
			// Void old link to avoid violating unique constraint.
			voided = true
			oldVia := machineID + "/" + oldSpace
			if _, err := tx.Exec(ctx, `DELETE FROM todo_spaces WHERE machine_id = $1 AND space = $2`, machineID, oldSpace); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM todo_agents WHERE source = 'space' AND via = $1`, oldVia); err != nil {
				return err
			}
			return nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		if _, err := tx.Exec(ctx, `UPDATE todo_spaces SET space = $3 WHERE machine_id = $1 AND space = $2`, machineID, oldSpace, newSpace); err != nil {
			return err
		}
		oldVia := machineID + "/" + oldSpace
		newVia := machineID + "/" + newSpace
		if _, err := tx.Exec(ctx, `UPDATE todo_agents SET via = $2 WHERE source = 'space' AND via = $1`, oldVia, newVia); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return voided, nil
}

// AutoLinkTodoAgent links an agent automatically if not already present.
// Unlike LinkTodoAgentWithSource, it uses INSERT ... ON CONFLICT DO NOTHING so that
// any existing row (such as a dismissed link from an immediate unlink) is NOT overwritten.
// Returns (inserted bool, releasedTodoIDs []string, err error).
func (d *DB) AutoLinkTodoAgent(ctx context.Context, todoID, machineID, sessionID, title, cwd, agent, startError, source, via string, linkedAt time.Time) (bool, []string, error) {
	conn, err := d.pool.Acquire(ctx)
	if err != nil {
		return false, nil, err
	}
	defer conn.Release()

	var inserted bool
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

		// Try insert with DO NOTHING so we never overwrite an existing/dismissed row.
		tag, err := tx.Exec(ctx, `INSERT INTO todo_agents (todo_id, machine_id, session_id, linked_at, released_at, title, cwd, agent, start_error, source, via, dismissed_at)
			VALUES ($1, $2, $3, $4, NULL, $5, $6, $7, $8, $9, $10, NULL)
			ON CONFLICT (todo_id, machine_id, session_id) DO NOTHING`,
			todoID, machineID, sessionID, linkedAt, title, cwd, agent, startError, source, via)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			// Row already exists (e.g. dismissed by immediate user unlink); do not change anything.
			return nil
		}
		inserted = true

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

		_, err = tx.Exec(ctx, `UPDATE todos SET updated_at = $2 WHERE id = $1`, todoID, linkedAt)
		return err
	})
	if err != nil {
		return false, nil, err
	}
	return inserted, releasedTodoIDs, nil
}

// LinkTodoAgent links an agent manually to a todo.
func (d *DB) LinkTodoAgent(ctx context.Context, todoID, machineID, sessionID, title, cwd, agent, startError string, linkedAt time.Time) ([]string, error) {
	return d.LinkTodoAgentWithSource(ctx, todoID, machineID, sessionID, title, cwd, agent, startError, "manual", "", linkedAt)
}

// LinkTodoAgentWithSource links an agent with a specific source ("manual", "space", "spawned") and via.
func (d *DB) LinkTodoAgentWithSource(ctx context.Context, todoID, machineID, sessionID, title, cwd, agent, startError, source, via string, linkedAt time.Time) ([]string, error) {
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

		// Upsert link on target todo. Clears dismissed_at and sets source/via.
		_, err = tx.Exec(ctx, `INSERT INTO todo_agents (todo_id, machine_id, session_id, linked_at, released_at, title, cwd, agent, start_error, source, via, dismissed_at)
			VALUES ($1, $2, $3, $4, NULL, $5, $6, $7, $8, $9, $10, NULL)
			ON CONFLICT (todo_id, machine_id, session_id) DO UPDATE SET
				linked_at = $4,
				released_at = NULL,
				dismissed_at = NULL,
				source = $9,
				via = $10,
				title = CASE WHEN $5 != '' THEN $5 ELSE todo_agents.title END,
				cwd = CASE WHEN $6 != '' THEN $6 ELSE todo_agents.cwd END,
				agent = CASE WHEN $7 != '' THEN $7 ELSE todo_agents.agent END,
				start_error = $8`,
			todoID, machineID, sessionID, linkedAt, title, cwd, agent, startError, source, via)
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

// UnlinkTodoAgent marks the agent as released and dismissed on a todo (soft unlink, #762).
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

		now := time.Now().UTC()
		_, err = tx.Exec(ctx, `INSERT INTO todo_agents (todo_id, machine_id, session_id, linked_at, released_at, dismissed_at)
			VALUES ($1, $2, $3, $4, $4, $4)
			ON CONFLICT (todo_id, machine_id, session_id) DO UPDATE SET
				released_at = $4,
				dismissed_at = $4`,
			todoID, machineID, sessionID, now)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE todos SET updated_at = $2 WHERE id = $1`, todoID, now)
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
