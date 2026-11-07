package assistant

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// countingHub is the fake hub counting its hellos: each turn of scheduleLoop asks for one.
type countingHub struct {
	*fakeHub
	hellos atomic.Int32
}

func (h *countingHub) Hello(ctx context.Context) (Hello, error) {
	h.hellos.Add(1)
	return h.fakeHub.Hello(ctx)
}

// A mesh member leaves its tasks to the center (docs/server/mesh.md), so a one-off already past stays past: the loop must
// still sleep, not wake every 50 ms for as long as the hub is a member.
func TestStandbyScheduleLoopDoesNotSpinOnAPastTask(t *testing.T) {
	f := newFakeHub().withMachine()
	f.hello.Assistant.Standby = true
	h := &countingHub{fakeHub: f}
	s := withSettings(t, f, New(Options{Hub: h, Logf: testLogf(t)}))
	setTasks(s, Task{ID: "fu_1", Repeat: repeatOnce, At: time.Now().Add(-time.Minute), Prompt: "x", Enabled: true})

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	s.scheduleLoop(ctx)

	if n := h.hellos.Load(); n > 3 {
		t.Fatalf("the standby loop woke %d times in 1.5 s; it should sleep until the idle wake-up", n)
	}
	if got, _ := taskByID(s, "fu_1"); !got.Enabled || !got.LastRun.IsZero() {
		t.Fatalf("a member hub does not run the task: %+v", got)
	}
}
