package assistant

import (
	"context"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
)

// streakLimit: how many runs in a row a repeating task may fail or be skipped before the user is told (#4658). A
// nightly task skipped five nights in a row was only five lines in the run history nobody reads; three is late
// enough to be a real problem and early enough to act on.
const streakLimit = 3

// counted: whether r is one of the runs a streak counts (#4658). A failed 立即运行 is answered in the app, and an
// automatic retry that failed is the same failure once more (watchScheduledRun): neither counts. Any run that went
// through, a retry or a 立即运行 included, ends the streak: the task works.
func counted(r ScheduleRun) bool {
	return r.Status == runOK || !r.Manual && !r.Retried
}

// runStreak counts ref's runs in a row that did not go through, newest first.
func runStreak(runs []ScheduleRun, ref string) int {
	n := 0
	for i := len(runs) - 1; i >= 0; i-- {
		r := runs[i]
		if r.RefID != ref || !counted(r) {
			continue
		}
		if r.Status == runOK {
			return n
		}
		n++
	}
	return n
}

// streakToldLocked says whether r, just recorded (or just turned failed), is the run that brought a repeating task to
// streakLimit runs in a row that did not go through; t is that task then. The schedule is locked. Exactly
// streakLimit, and only when r itself counts: the streak only grows until a run goes through, so this is once.
func streakToldLocked(sc *schedule, r ScheduleRun) (Task, bool) {
	if r.Status == runOK || !counted(r) {
		return Task{}, false
	}
	i := sc.indexLocked(r.RefID)
	if i < 0 || sc.tasks[i].Repeat == repeatOnce {
		return Task{}, false
	}
	return sc.tasks[i], runStreak(sc.runs, r.RefID) == streakLimit
}

// pushStreak tells the user t has not gone through its last streakLimit runs (#4658): which task, how many, and the
// last run's reason, in the conversation that run was in. Nothing repeats it until the task goes through once.
func (s *Service) pushStreak(t Task, last ScheduleRun) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if last.SessionID != "" && s.muted(last.MachineID, last.SessionID) {
		return
	}
	l := s.lang(ctx)
	name := t.labelIn(l)
	if t.Space == SpaceHealth { // the lock screen does not name a health task (health.go)
		name, _ = healthNotice(l)
	}
	why := clip(oneLine(last.Error), 300)
	if why == "" {
		why = i18n.T(l, "assistant-schedule.streak_unknown")
	}
	n := Notification{SessionID: last.SessionID, DeviceID: last.MachineID,
		Title: i18n.T(l, "assistant-schedule.streak", i18n.Args{"name": name, "count": streakLimit}),
		Body:  i18n.T(l, "assistant-schedule.streak_body", i18n.Args{"reason": why, "count": streakLimit})}
	if last.MachineID != "" {
		if m, ok := s.device(ctx, last.MachineID); ok {
			n.Subtitle = m.Name
		}
	}
	s.push(ctx, n)
}
