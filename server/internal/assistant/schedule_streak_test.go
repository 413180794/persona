package assistant

import (
	"strings"
	"testing"
)

// outcome records one run of task ref as it ends (recordRun is where the history and the streak are kept).
func outcome(h *Service, ref, status, why string) {
	h.recordRun(ScheduleRun{Kind: "routine", RefID: ref, Title: ref, Status: status, Error: why})
}

// The user's ask (#4658): a repeating task that keeps failing or being skipped was only a line in the run history.
// At streakLimit runs in a row that did not go through, the user gets one push naming the task, the count and the
// last reason; nothing more until it goes through once.
func TestARepeatingTaskThatKeepsNotGoingThroughIsToldOnce(t *testing.T) {
	n := newFakeHub().withMachine()
	h := newStorelessService(t, n)
	setTasks(h, Task{ID: "p", Title: "夜间巡检", Prompt: "巡检", Repeat: repeatDaily, Time: "02:00", Enabled: true})

	outcome(h, "p", runFailed, "API Error: 503")
	outcome(h, "p", runSkipped, "Persona 停机，错过了时间")
	if p := n.pushed(); len(p) != 0 {
		t.Fatalf("two in a row is still the history's business: %+v", p)
	}

	outcome(h, "p", runFailed, "API Error: 503 upstream attempt budget is exhausted")
	p := n.pushed()
	if len(p) != 1 || !strings.Contains(p[0].Title, "夜间巡检") || !strings.Contains(p[0].Title, "3") ||
		!strings.Contains(p[0].Body, "API Error: 503 upstream attempt budget is exhausted") {
		t.Fatalf("one push naming the task, how many and the last reason: %+v", p)
	}

	// A fourth and a fifth: no second push. The streak only grows until a run goes through.
	outcome(h, "p", runFailed, "x")
	outcome(h, "p", runSkipped, "y")
	if p := n.pushed(); len(p) != 1 {
		t.Fatalf("nothing more until it goes through: %+v", p)
	}

	// Its automatic retry is part of the failure it retried: an ok record with Retried neither ends the streak nor
	// tells again when the retry fails too.
	h.recordRun(ScheduleRun{Kind: "routine", RefID: "p", Status: runOK, Retried: true})
	h.recordRun(ScheduleRun{Kind: "routine", RefID: "p", Status: runFailed, Retried: true, Error: "retry"})
	if p := n.pushed(); len(p) != 1 {
		t.Fatalf("a retry is the same failure over again: %+v", p)
	}

	// Going through once ends it: three more in a row is news again.
	outcome(h, "p", runOK, "")
	if p := n.pushed(); len(p) != 1 {
		t.Fatalf("a run that went through is quiet: %+v", p)
	}
	outcome(h, "p", runFailed, "a")
	outcome(h, "p", runFailed, "b")
	outcome(h, "p", runFailed, "c")
	if p := n.pushed(); len(p) != 2 || !strings.Contains(p[1].Body, "c") {
		t.Fatalf("after a success the next three in a row are told again: %+v", p)
	}
}

// Only the runs that were meant to happen count: 立即运行 is answered in the app (and a failure of it there), and a
// one-off is not a repeating task (its failure is pushed and retried by the run itself).
func TestOnlyRepeatingTasksAndTheirScheduledRunsCountTowardsTheStreak(t *testing.T) {
	n := newFakeHub().withMachine()
	h := newStorelessService(t, n)
	setTasks(h,
		Task{ID: "p", Title: "夜间巡检", Prompt: "巡检", Repeat: repeatDaily, Time: "02:00", Enabled: true},
		Task{ID: "f", Title: "提醒", Prompt: "提醒", Repeat: repeatOnce, Enabled: true})

	for i := 0; i < streakLimit; i++ {
		h.recordRun(ScheduleRun{Kind: "routine", RefID: "p", Title: "夜间巡检", Status: runFailed, Error: "manual", Manual: true})
		h.recordRun(ScheduleRun{Kind: "followup", RefID: "f", Title: "提醒", Status: runFailed, Error: "one-off"})
	}
	if p := n.pushed(); len(p) != 0 {
		t.Fatalf("立即运行 and one-off runs are not a streak: %+v", p)
	}
	outcome(h, "p", runFailed, "scheduled")
	if p := n.pushed(); len(p) != 0 {
		t.Fatalf("one scheduled failure is not a streak either: %+v", p)
	}
}

// A task skipped night after night because its conversation is still on the last run is a streak like any other: a
// last run stuck working is not pushed anywhere else (#4506 only tells a wait on the user), so the user is told once.
// And an automatic retry that went through ends the streak: the task worked.
func TestABusyStreakIsToldAndARetryThatWorkedEndsIt(t *testing.T) {
	n := newFakeHub().withMachine()
	h := newStorelessService(t, n)
	setTasks(h, Task{ID: "p", Title: "夜间巡检", Prompt: "巡检", Repeat: repeatDaily, Time: "02:00", Enabled: true})
	for i := 0; i < streakLimit; i++ {
		h.recordRun(ScheduleRun{Kind: "routine", RefID: "p", Title: "夜间巡检", Status: runSkipped, Error: "上一轮还在运行，这次跳过",
			MachineID: "server", SessionID: "s1"})
	}
	if p := n.pushed(); len(p) != 1 {
		t.Fatalf("three busy skips in a row are told once: %+v", p)
	}
	h.recordRun(ScheduleRun{Kind: "routine", RefID: "p", Status: runOK, Retried: true})
	outcome(h, "p", runFailed, "a")
	outcome(h, "p", runFailed, "b")
	if p := n.pushed(); len(p) != 1 {
		t.Fatalf("a retry that went through ended the streak, two more is not three: %+v", p)
	}
}
