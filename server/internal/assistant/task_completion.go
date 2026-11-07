package assistant

import (
	"context"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// Every linked agent must be observed. A missing session or disconnected node is
// not evidence that its work ended; raw runtime status also outranks a stale UI state.
func taskAgentsSettled(t Todo, agents []proto.Session) bool {
	expected := 0
	for _, a := range t.Agents {
		if a.ReleasedAt == nil && a.DismissedAt == nil {
			expected++
		}
	}
	if expected == 0 || len(agents) != expected {
		return false
	}
	for _, c := range agents {
		if c.Status != proto.StatusDone && c.Status != proto.StatusIdle ||
			c.State == proto.StateWorking || c.State == proto.StateNeedsYou ||
			c.PendingInteractions > 0 || backgroundRunning(c) {
			return false
		}
		if w := c.Work; w != nil && w.Phase == proto.WorkBlocked && w.On == proto.WaitTransient && w.WakeAt != nil {
			return false
		}
	}
	return true
}

// A status tick can briefly say idle between tool output and the next model
// response. Earlier assistant words cannot finish that still-open exchange.
func taskTurnAwaitingAnswer(items []proto.Item) bool {
	for i := len(items) - 1; i >= 0; i-- {
		it := items[i]
		switch it.Kind {
		case "turn_duration":
			return false
		case proto.KindText:
			return it.Role != "assistant"
		case proto.KindToolUse, proto.KindToolResult, proto.KindThinking:
			return true
		}
	}
	return false
}

// A final answer can arrive after an idle tick without another working tick.
// Retry only a receipt held for that answer, rather than notifying the turn twice.
func (s *Service) takeTaskAnswerRetry(machine, sid string) bool {
	s.tasks.mu.Lock()
	defer s.tasks.mu.Unlock()
	ref := SessionRef{MachineID: machine, SessionID: sid}
	if !s.tasks.awaitingAnswer[ref] {
		return false
	}
	delete(s.tasks.awaitingAnswer, ref)
	return true
}

func (s *Service) taskWorkCurrent(id string, work time.Time) bool {
	s.tasks.mu.Lock()
	defer s.tasks.mu.Unlock()
	return !s.tasks.working[id].After(work)
}

// Model calls and transcript reads can outlive a turn. Check both the observed
// turn and fresh runtime state before applying their result, including review state.
func (s *Service) taskCompletionCurrent(ctx context.Context, t Todo, work time.Time) bool {
	current, ok := s.todoNow(t.ID)
	if !ok || current.DoneAt != nil || current.Notes != t.Notes || autoDoneSkip(current, work, true) != "" || !s.taskWorkCurrent(t.ID, work) {
		return false
	}
	return s.taskRuntimeSettled(ctx, current) && s.taskWorkCurrent(t.ID, work)
}

func (s *Service) taskRuntimeSettled(ctx context.Context, t Todo) bool {
	agents, reached := s.taskAgentsReached(ctx, t)
	return reached && taskAgentsSettled(t, agents)
}

// A turn that starts before this lock invalidates the judgment. One that starts
// afterwards reopens the automatically closed task through taskWorking.
func (s *Service) finishCurrentTask(ctx context.Context, id, reason string, work time.Time) (bool, error) {
	s.tasks.completionMu.Lock()
	defer s.tasks.completionMu.Unlock()
	if !s.taskWorkCurrent(id, work) {
		return false, nil
	}
	return s.autoDoneTodo(ctx, id, reason, work)
}
