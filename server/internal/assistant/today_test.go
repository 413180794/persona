package assistant

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestTodayCardListsPendingReviewBeforeAgentState(t *testing.T) {
	for _, tc := range []struct {
		name      string
		state     string
		turnError string
	}{
		{"followup", proto.StateIdle, ""},
		{"unread", proto.StateUnread, ""},
		{"working", proto.StateWorking, ""},
		{"needs_you", proto.StateNeedsYou, ""},
		{"interrupted", proto.StateIdle, "request failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, f := newTaskService(t)
			todo := startedTask(t, s, f)
			ctx := context.Background()
			f.setSessions("box", proto.Session{MachineID: "box", SessionID: "s1", State: tc.state, TurnError: tc.turnError})
			if _, _, err := s.updateSupervision(ctx, todo.ID, func(v *Supervision) bool {
				v.State = reviewPending
				return true
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.appendFeed(ctx, "conv1", feedReceipt, todo.ID, receiptBody{Title: todo.Title, State: "done"}); err != nil {
				t.Fatal(err)
			}
			it, made, err := s.ensureToday(ctx, "conv1")
			if err != nil || !made {
				t.Fatalf("today made=%v err=%v", made, err)
			}
			var body todayBody
			if err := json.Unmarshal(it.Body, &body); err != nil {
				t.Fatal(err)
			}
			if want := []todayTask{{ID: todo.ID, Title: todo.Title}}; !reflect.DeepEqual(body.PendingReview, want) {
				t.Fatalf("pending review = %+v, want %+v", body.PendingReview, want)
			}
			if len(body.Waiting) != 0 || body.Working != 0 || len(body.Done) != 0 {
				t.Fatalf("pending review also counted in another group: %+v", body)
			}
		})
	}
}

func TestTodayCardPendingReviewScopeAndOrder(t *testing.T) {
	s, f := newTaskService(t)
	ctx := context.Background()
	now := time.Now()
	var sessions []proto.Session
	var wantReview, wantWaiting []todayTask
	for i, tc := range []struct {
		title       string
		origin      string
		review      string
		agentState  string
		closed      bool
		wantReview  bool
		wantWaiting bool
	}{
		{"Older review", "conv1", reviewPending, "", false, true, false},
		{"Newer review", "conv1", reviewPending, "", false, true, false},
		{"Other conversation", "conv2", reviewPending, "", false, false, false},
		{"Manually closed", "conv1", reviewPending, "", true, false, false},
		{"Accepted", "conv1", reviewAccepted, "", true, false, false},
		{"Not supervised", "conv1", "", proto.StateUnread, false, false, false},
		{"Ordinary question", "conv1", "", proto.StateNeedsYou, false, false, true},
		{"Ordinary work", "conv1", "", proto.StateWorking, false, false, false},
		{"Awaiting a decision", "conv1", reviewNeedsUser, proto.StateNeedsYou, false, false, true},
	} {
		todo, err := s.createTodo(ctx, tc.title, "", tc.origin, "")
		if err != nil {
			t.Fatal(err)
		}
		if tc.review != "" {
			if _, _, err := s.updateSupervision(ctx, todo.ID, func(v *Supervision) bool {
				v.Supervisor, v.State = tc.origin, tc.review
				return true
			}); err != nil {
				t.Fatal(err)
			}
		}
		// Review waits are not limited to the overnight window used for completed tasks.
		s.todos.mu.Lock()
		s.todos.todos[todo.ID].CreatedAt = now.Add(-48*time.Hour + time.Duration(i)*time.Minute)
		if tc.closed {
			s.todos.todos[todo.ID].DoneAt = &now
		}
		s.todos.mu.Unlock()
		if tc.agentState != "" {
			if err := s.linkTodoAgent(ctx, todo.ID, "box", todo.ID, tc.title, "/code/app", "claude"); err != nil {
				t.Fatal(err)
			}
			sessions = append(sessions, proto.Session{MachineID: "box", SessionID: todo.ID, State: tc.agentState})
		}
		if tc.wantReview {
			wantReview = append(wantReview, todayTask{ID: todo.ID, Title: tc.title})
		}
		if tc.wantWaiting {
			wantWaiting = append(wantWaiting, todayTask{ID: todo.ID, Title: tc.title})
		}
	}
	f.setSessions("box", sessions...)
	it, _, err := s.ensureToday(ctx, "conv1")
	if err != nil {
		t.Fatal(err)
	}
	var body todayBody
	if err := json.Unmarshal(it.Body, &body); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(body.PendingReview, wantReview) {
		t.Fatalf("pending review = %+v, want open tasks of this conversation, oldest first: %+v", body.PendingReview, wantReview)
	}
	if !reflect.DeepEqual(body.Waiting, wantWaiting) || body.Working != 1 {
		t.Fatalf("ordinary waiting / working tasks changed: %+v", body)
	}
}

func TestTodayCardOmitsEmptyPendingReview(t *testing.T) {
	s, _ := newTaskService(t)
	it, _, err := s.ensureToday(context.Background(), "conv1")
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(it.Body, &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["pending_review"]; ok {
		t.Fatalf("empty pending_review must be omitted: %s", it.Body)
	}
}
