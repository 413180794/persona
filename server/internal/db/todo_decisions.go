package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrDecisionNotFound = errors.New("decision not found")
	// ErrDecisionSupersede: the decision named as superseded is not an earlier one of the same todo.
	ErrDecisionSupersede = errors.New("a decision can only supersede an earlier decision of the same todo")
)

// Decision sources: written in the todo by hand, from the user's answer to an agent's question card, what a task
// came to when it was finished (its outcome, decisions and pitfalls, #2820), or noted by an agent on it as it worked
// (todo_decision_add, #5279: the agent's call, not the user's).
const (
	DecisionManual  = "manual"
	DecisionCard    = "card"
	DecisionOutcome = "outcome"
	DecisionAgent   = "agent"
)

// TodoDecisionRow is one call the user made on a todo (#1618). SupersedesID names the earlier decision this one
// replaces; SupersededBy is filled in when listing: the newest decision that supersedes this one, empty while it is
// in effect.
type TodoDecisionRow struct {
	ID            string    `json:"id"`
	TodoID        string    `json:"todo_id"`
	Question      string    `json:"question"`
	Choice        string    `json:"choice"`
	Reason        string    `json:"reason"`
	Source        string    `json:"source"`
	SourceMachine string    `json:"source_machine"`
	SourceSession string    `json:"source_session"`
	SourceMessage string    `json:"source_message"`
	SupersedesID  string    `json:"supersedes_id"`
	SupersededBy  string    `json:"superseded_by"`
	CreatedAt     time.Time `json:"created_at"`
}

// ListTodoDecisions returns a todo's decisions, oldest first: only those in effect, or all of them with all.
func (d *DB) ListTodoDecisions(ctx context.Context, todoID string, all bool) ([]TodoDecisionRow, error) {
	rows, err := d.pool.Query(ctx, `SELECT d.id, d.todo_id, d.question, d.choice, d.reason, d.source, d.source_machine,
			d.source_session, d.source_message, COALESCE(d.supersedes_id, ''),
			COALESCE((SELECT n.id FROM todo_decisions n WHERE n.supersedes_id = d.id ORDER BY n.created_at DESC, n.id DESC LIMIT 1), ''),
			d.created_at
		FROM todo_decisions d WHERE d.todo_id = $1
		ORDER BY d.created_at, d.id`, todoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TodoDecisionRow{}
	for rows.Next() {
		var r TodoDecisionRow
		if err := rows.Scan(&r.ID, &r.TodoID, &r.Question, &r.Choice, &r.Reason, &r.Source, &r.SourceMachine,
			&r.SourceSession, &r.SourceMessage, &r.SupersedesID, &r.SupersededBy, &r.CreatedAt); err != nil {
			return nil, err
		}
		if all || r.SupersededBy == "" {
			out = append(out, r)
		}
	}
	return out, rows.Err()
}

// InsertTodoDecision writes a decision. ErrTodoNotFound when the todo is gone, ErrDecisionSupersede when SupersedesID
// is not an earlier decision of the same todo. A card's answer already written (same todo, interaction and question)
// is not written again: inserted is false then.
func (d *DB) InsertTodoDecision(ctx context.Context, r TodoDecisionRow) (inserted bool, err error) {
	conn, err := d.pool.Acquire(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Release()
	err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		var one int
		err := tx.QueryRow(ctx, `SELECT 1 FROM todos WHERE id = $1 FOR UPDATE`, r.TodoID).Scan(&one)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTodoNotFound
		}
		if err != nil {
			return err
		}
		if r.SupersedesID != "" {
			if err := checkSupersede(ctx, tx, r.TodoID, r.SupersedesID, r.ID, r.CreatedAt); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `INSERT INTO todo_decisions (id, todo_id, question, choice, reason, source, source_machine,
				source_session, source_message, supersedes_id, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, ''), $11)
			ON CONFLICT (todo_id, source_message, question) WHERE source_message <> '' DO NOTHING`,
			r.ID, r.TodoID, r.Question, r.Choice, r.Reason, r.Source, r.SourceMachine, r.SourceSession, r.SourceMessage,
			r.SupersedesID, r.CreatedAt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		inserted = true
		_, err = tx.Exec(ctx, `UPDATE todos SET updated_at = $2 WHERE id = $1`, r.TodoID, r.CreatedAt)
		return err
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" { // the todo was deleted between the check and the insert
		return false, ErrTodoNotFound
	}
	return inserted, err
}

// SetTodoDecisionSupersedes marks decision id of todoID as superseding the earlier decision supersedesID ("" clears
// it), and returns it as listed. ErrDecisionNotFound when id is not the todo's, ErrDecisionSupersede as for
// InsertTodoDecision.
func (d *DB) SetTodoDecisionSupersedes(ctx context.Context, todoID, id, supersedesID string, at time.Time) (*TodoDecisionRow, error) {
	conn, err := d.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		var createdAt time.Time
		err := tx.QueryRow(ctx, `SELECT created_at FROM todo_decisions WHERE id = $1 AND todo_id = $2 FOR UPDATE`, id, todoID).Scan(&createdAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDecisionNotFound
		}
		if err != nil {
			return err
		}
		if supersedesID != "" {
			if supersedesID == id {
				return ErrDecisionSupersede
			}
			if err := checkSupersede(ctx, tx, todoID, supersedesID, id, createdAt); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE todo_decisions SET supersedes_id = NULLIF($2, '') WHERE id = $1`, id, supersedesID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE todos SET updated_at = $2 WHERE id = $1`, todoID, at)
		return err
	})
	if err != nil {
		return nil, err
	}
	all, err := d.ListTodoDecisions(ctx, todoID, true)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}
	return nil, ErrDecisionNotFound
}

// checkSupersede: old is a decision of todoID that comes before the decision (id, at) in the list order (created_at,
// then id), so supersession never goes in a circle.
func checkSupersede(ctx context.Context, tx pgx.Tx, todoID, old, id string, at time.Time) error {
	var oldAt time.Time
	err := tx.QueryRow(ctx, `SELECT created_at FROM todo_decisions WHERE id = $1 AND todo_id = $2`, old, todoID).Scan(&oldAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDecisionSupersede
	}
	if err != nil {
		return err
	}
	if oldAt.After(at) || oldAt.Equal(at) && old >= id {
		return ErrDecisionSupersede
	}
	return nil
}
