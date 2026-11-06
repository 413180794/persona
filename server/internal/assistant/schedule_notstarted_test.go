package assistant

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A one-off follow-up whose run cannot start (its machine offline) was its only run: it is told and tried once more,
// not just recorded as failed and switched off (#5030). A repeating task is left to its next run and streak notice.
func TestAOneOffThatCannotStartIsToldAndRetriedOnce(t *testing.T) {
	n := newFakeHub().withMachine()
	h := newStorelessService(t, n)
	h.runRetry = 50 * time.Millisecond
	n.failAssistant = errors.New("machine is offline")
	setTasks(h,
		Task{ID: "fu", Title: "看一眼 CI", Prompt: "看一眼 CI", Repeat: repeatOnce, At: time.Now().Add(-time.Minute), Enabled: true, CreatedBy: "assistant"},
		Task{ID: "p", Title: "巡检", Prompt: "巡检", Repeat: repeatInterval, EveryMinutes: 30, At: time.Now().Add(-time.Minute), Enabled: true})
	h.runDue(context.Background(), time.Now())

	runs := waitRuns(t, h, 3) // the follow-up, the patrol, the follow-up's retry
	time.Sleep(300 * time.Millisecond)
	runs = waitRuns(t, h, 3)
	if len(runs) != 3 {
		t.Fatalf("one retry of the follow-up, none of the patrol: %+v", runs)
	}
	retried := 0
	for _, r := range runs {
		if r.Status != runFailed {
			t.Fatalf("every run failed: %+v", r)
		}
		if r.Retried {
			retried++
			if r.RefID != "fu" {
				t.Fatalf("only the one-off is retried: %+v", r)
			}
		}
	}
	if retried != 1 {
		t.Fatalf("the follow-up is retried once: %+v", runs)
	}
	p := n.pushed()
	if len(p) != 2 || p[0].Title != "定时任务失败：看一眼 CI" || p[1].Title != "定时任务失败：看一眼 CI" || p[0].Body != "machine is offline" {
		t.Fatalf("the follow-up's failure and its retry's are pushed, the patrol's not: %+v", p)
	}
}

// A one-off deleted after its run could not start is not tried again.
func TestAOneOffDeletedAfterItCouldNotStartIsNotRetried(t *testing.T) {
	n := newFakeHub().withMachine()
	h := newStorelessService(t, n)
	h.runRetry = 100 * time.Millisecond
	n.failAssistant = errors.New("machine is offline")
	setTasks(h, Task{ID: "fu", Title: "看一眼 CI", Prompt: "看一眼 CI", Repeat: repeatOnce, At: time.Now().Add(-time.Minute), Enabled: true})
	h.runDue(context.Background(), time.Now())
	waitRuns(t, h, 1)
	if _, err := h.deleteTask(context.Background(), "fu"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	if runs := waitRuns(t, h, 1); len(runs) != 1 {
		t.Fatalf("a deleted follow-up is not retried: %+v", runs)
	}
}
