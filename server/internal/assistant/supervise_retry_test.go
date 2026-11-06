package assistant

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A digest due while the main conversation cannot be found (the hub's hello has no machine while the bus is down)
// keeps its signals: they go out once it is back, instead of being marked told and dropped (#5420).
func TestDigestWaitsForTheMainConversation(t *testing.T) {
	s, f := newQuietService(t)
	ctx := context.Background()
	todo, err := s.createTodo(ctx, "甲", "做甲", "conv1", "")
	if err != nil {
		t.Fatal(err)
	}
	s.superviseTodo(ctx, todo.ID, "conv1", []string{"PR 合并"}, 0)
	if _, _, err := s.updateSupervision(ctx, todo.ID, func(v *Supervision) bool { v.State = reviewPending; return true }); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.hello.Assistant.MachineID = "" // the hub is not reachable: no hello
	f.mu.Unlock()
	s.signal(superviseSignal{Todo: todo.ID, Kind: sigReview, Key: "1", Detail: "做完了"})
	time.Sleep(10 * superviseGather) // the digest was due, and found nowhere to go
	if n := f.notices(); len(n) != 0 {
		t.Fatalf("sent with no main conversation: %q", n)
	}
	f.mu.Lock()
	f.hello.Assistant.MachineID = "server"
	f.mu.Unlock()
	waitFor(t, "the message once the hub is back", func() bool { return len(f.notices()) == 1 })
	if n := f.notices()[0]; !strings.Contains(n, "任务「甲」") {
		t.Fatalf("message:\n%s", n)
	}
}
