package assistant

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestQueuedSupervisionTaskEnds(t *testing.T) {
	for _, outcome := range []string{"completed", reviewAccepted, reviewCancelled, "deleted", "transferred"} {
		t.Run(outcome, func(t *testing.T) {
			s, f, task := newSupervisedService(t)
			ctx := t.Context()
			s.superviseTodo(ctx, task.ID, "side", task.Supervision.DoneWhen, 30)
			listRound(f, "side", proto.StatusWorking, false)
			wakeRound(s, superviseSignal{Todo: task.ID, Kind: sigInterrupted, Key: "one"},
				superviseSignal{Todo: task.ID, Kind: sigUnjudged, Key: "two"})
			q := queuedRelays(s, "side")
			if len(q) != 1 || strings.Count(q[0].Text, "signal=") != 2 {
				t.Fatalf("initial digest = %+v", q)
			}
			s.queueRelay("side", relayMsg{Tool: taskEndTool, From: task.ID, Text: "closeout"})
			var err error
			switch outcome {
			case "completed":
				done := true
				_, err = s.editTodo(ctx, task.ID, db.TodoEdit{Done: &done})
			case reviewAccepted, reviewCancelled:
				_, err = reviewErr(s, task.ID, map[string]any{"verdict": outcome, "note": "checked",
					"checks": completeReviewChecks(task)})
			case "deleted":
				err = s.deleteTodo(ctx, task.ID)
			case "transferred":
				_, _, err = s.updateSupervision(ctx, task.ID, func(v *Supervision) bool { v.Supervisor = "another"; return true })
			}
			if err != nil {
				t.Fatal(err)
			}
			waitFor(t, "held digest removed without an idle receiver", func() bool {
				q := queuedRelays(s, "side")
				return len(q) == 1 && q[0].Tool == taskEndTool
			})
			if len(f.noticesTo("side")) != 0 {
				t.Fatal("closing or transferring the task started a turn")
			}
		})
	}
}

func TestQueuedSupervisionPrunesMergedDigest(t *testing.T) {
	st := &memStore{}
	s, f := newQuietServiceOn(t, st)
	listRound(f, "side", proto.StatusWorking, false)
	task := func(title string) Todo {
		t.Helper()
		task, err := s.createDirectionTodo(t.Context(), "side", title, "", db.TodoEdit{})
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	closed, open := task("First"), task("Second")
	wakeRound(s, superviseSignal{Todo: closed.ID, Kind: sigInterrupted, Key: "first", Detail: "old failure"})
	wakeRound(s, superviseSignal{Todo: open.ID, Kind: sigStalled, Key: "second", Detail: "current evidence"})
	q := queuedRelays(s, "side")
	if len(q) != 1 || !q[0].Urgent || !strings.Contains(q[0].Text, closed.ID) || !strings.Contains(q[0].Text, open.ID) {
		t.Fatalf("not merged: %+v", q)
	}
	old := q[0]
	done := true
	if _, err := s.editTodo(t.Context(), closed.ID, db.TodoEdit{Done: &done}); err != nil {
		t.Fatal(err)
	}
	var remaining relayMsg
	waitFor(t, "persisted single actionable entry", func() bool {
		var saved relayState
		_, err := st.Setting(t.Context(), relayKey, &saved)
		q := saved.Queue["side"]
		if err != nil || len(q) != 1 || strings.Contains(q[0].Text, closed.ID) {
			return false
		}
		remaining = q[0]
		return true
	})
	if remaining.Urgent || !remaining.QueuedAt.Equal(old.QueuedAt) || remaining.Depth != old.Depth ||
		relayMsgID("side", remaining) != relayMsgID("side", old) || !strings.Contains(remaining.Text, "1. 任务「Second」") ||
		!strings.Contains(remaining.Text, "current evidence") || strings.Contains(remaining.Text, "old failure") {
		t.Fatalf("filtered digest lost its facts or queue identity: %+v", remaining)
	}
	s.drainRelays("server", listRound(f, "side", proto.StatusIdle, false))
	waitFor(t, "only the remaining task delivered", func() bool { return len(f.noticesTo("side")) == 1 && s.queuedFor("side") == 0 })
	if got := f.noticesTo("side")[0]; !strings.Contains(got, open.ID) || strings.Contains(got, closed.ID) {
		t.Fatal(got)
	}
}

// Settings survive in memStore. Supply the actual task and ledger rows for a
// restarted manager too, including the distinction between missing and unreadable.
type relayLedgerStore struct {
	*ruleTodos
	supervisionStore
	ledgers []db.TodoSupervisionRow
}

func (st *relayLedgerStore) ListTodoSupervision(context.Context) ([]db.TodoSupervisionRow, error) {
	return st.ledgers, nil
}

func TestRestoredLegacySupervisionRecheckedOnDrain(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		name := "all closed"
		if mixed {
			name = "one still open"
		}
		t.Run(name, func(t *testing.T) {
			at := time.Now().Add(-time.Minute).UTC()
			closed, open := "todo_0000000000000001", "todo_0000000000000002"
			st := &relayLedgerStore{ruleTodos: &ruleTodos{ruleStore: ruleStore{&memStore{}}, todos: []db.TodoRow{
				{ID: closed, DoneAt: &at}, {ID: open}}}, ledgers: []db.TodoSupervisionRow{
				{TodoID: closed, Supervisor: "side", State: reviewAccepted}, {TodoID: open, Supervisor: "side"}}}
			entries := []prompts.SuperviseEntry{{ID: closed, Title: "Closed", Kind: sigUnjudged}, {ID: closed, Title: "Closed", Kind: sigInterrupted}}
			if mixed {
				entries = append(entries, prompts.SuperviseEntry{ID: open, Title: "Open", Kind: sigStalled, Last: "keep the worker's evidence"})
			} else {
				// The side conversation has lost its last supervised task.
				st.ledgers[1].Supervisor = "another"
			}
			legacy := relayMsg{From: superviseTool, Tool: superviseTool, At: at, Urgent: true, Text: prompts.RenderSupervise(prompts.Supervise{Side: true, Entries: entries})}
			if err := st.PutSetting(t.Context(), relayKey, relayState{Queue: map[string][]relayMsg{"side": {legacy}}}); err != nil {
				t.Fatal(err)
			}
			s, f := newQuietServiceOn(t, st)
			rows := listRound(f, "side", proto.StatusIdle, false)
			s.drainRelays("server", rows)
			waitFor(t, "old relay removed from storage", func() bool {
				var saved relayState
				_, err := st.Setting(t.Context(), relayKey, &saved)
				return err == nil && len(saved.Queue["side"]) == 0
			})
			got := f.noticesTo("side")
			if !mixed && len(got) != 0 || mixed && (len(got) != 1 || strings.Contains(got[0], closed) || !strings.Contains(got[0], "1. 任务「Open」") || !strings.Contains(got[0], "keep the worker's evidence")) {
				t.Fatalf("legacy relay delivered %q", got)
			}
		})
	}
}

func TestQueuedSupervisionUnreadableLedgerOrFailedSave(t *testing.T) {
	st := &relayLedgerStore{ruleTodos: &ruleTodos{ruleStore: ruleStore{&memStore{}}, fail: errors.New("ledger offline")}}
	s, f := newQuietServiceOn(t, st)
	msg := relayMsg{Tool: superviseTool, Text: prompts.RenderSupervise(prompts.Supervise{Entries: []prompts.SuperviseEntry{{ID: "todo_missing", Kind: sigInterrupted}}})}
	s.queueRelay("side", msg)
	s.drainRelays("server", listRound(f, "side", proto.StatusIdle, false))
	waitFor(t, "unreadable task held for recovery", func() bool {
		s.relays.mu.Lock()
		defer s.relays.mu.Unlock()
		return s.relays.stuck["side"].Text == msg.Text
	})
	if len(f.noticesTo("side")) != 0 {
		t.Fatal("sent despite unknown ledger")
	}
	st.fail = nil
	st.mu.Lock()
	st.failPut = errors.New("queue offline")
	st.mu.Unlock()
	s.pruneQueuedSupervision(t.Context())
	if s.queuedFor("side") != 1 {
		t.Fatal("failed write removed in-memory queue")
	}
	st.mu.Lock()
	st.failPut = nil
	st.mu.Unlock()
	s.pruneQueuedSupervision(t.Context())
	var saved relayState
	if _, err := st.Setting(t.Context(), relayKey, &saved); err != nil || len(saved.Queue["side"]) != 0 || s.queuedFor("side") != 0 {
		t.Fatalf("recovery failed: %+v %v", saved.Queue, err)
	}
}

func TestQueuedSupervisionClosesDuringAdmission(t *testing.T) {
	s, f := newQuietService(t)
	listRound(f, "side", proto.StatusWorking, false)
	task, err := s.createDirectionTodo(t.Context(), "side", "Assignment", "", db.TodoEdit{})
	if err != nil {
		t.Fatal(err)
	}
	wakeRound(s, superviseSignal{Todo: task.ID, Kind: sigInterrupted, Key: "interruption"})
	var attempts int
	f.mu.Lock()
	f.onAssistantSend = func(f *fakeHub, p map[string]any) {
		if p["session_id"] != "side" {
			return
		}
		attempts++
		at := time.Now().UTC()
		s.todos.mu.Lock()
		s.todos.todos[task.ID].DoneAt = &at
		s.todos.mu.Unlock()
		f.failAssistant = errSupervisorUnavailable
	}
	f.mu.Unlock()
	s.drainRelays("server", listRound(f, "side", proto.StatusIdle, false))
	waitFor(t, "permanent refusal rechecked against the closed task", func() bool { return s.queuedFor("side") == 0 })
	f.mu.Lock()
	defer f.mu.Unlock()
	if attempts != 1 {
		t.Fatalf("%d attempted deliveries", attempts)
	}
}

// List the receiver independently of whether its current turn can take a relay.
func listRound(f *fakeHub, id, status string, starting bool) []proto.Session {
	rows := []proto.Session{{SessionID: "conv1", MachineID: "server", Assistant: true, Status: proto.StatusIdle},
		{SessionID: id, MachineID: "server", Assistant: true, Status: status, Starting: starting}}
	f.setSessions("server", rows...)
	return rows
}
