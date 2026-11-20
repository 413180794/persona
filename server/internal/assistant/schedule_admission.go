package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

var errScheduleAdmission = errors.New("schedule admission unavailable")

type scheduledOccurrence struct {
	Task string    `json:"task_id"`
	Due  time.Time `json:"due"`
}

func (s *Service) keepScheduleDispatch(due []dueTask) (func(), error) {
	if s.maintenanceGate == nil || len(due) == 0 {
		return func() {}, nil
	}
	occurrences := make([]scheduledOccurrence, 0, len(due))
	for _, task := range due {
		occurrences = append(occurrences, scheduledOccurrence{Task: task.t.ID, Due: task.due.UTC()})
	}
	payload, err := json.Marshal(occurrences)
	if err != nil {
		return nil, err
	}
	return s.maintenanceGate.ContinuePayload("manager-schedules", "schedule-dispatch", "dispatch", payload)
}

// scheduleWork owns a durable operation through the dispatch, outcome writes
// and retries that its earlier stages spawn.
type scheduleWork struct {
	active    sync.WaitGroup
	uncertain atomic.Bool
}

func (w *scheduleWork) hold() func() {
	if w == nil {
		return func() {}
	}
	w.active.Add(1)
	return w.active.Done
}

func (w *scheduleWork) retain() {
	if w != nil {
		w.uncertain.Store(true)
	}
}

func (s *Service) scheduleAdmission(t Task, due time.Time, fromScan bool) (*scheduleWork, func(), error) {
	if s.maintenanceGate == nil {
		return nil, func() {}, nil
	}
	payload, err := json.Marshal(scheduledOccurrence{Task: t.ID, Due: due.UTC()})
	if err != nil {
		return nil, nil, err
	}
	var settle func()
	if fromScan {
		settle, err = s.maintenanceGate.ContinuePayload("manager-schedules", "schedule-run", "dispatch", payload)
	} else {
		settle, err = s.maintenanceGate.AdmitPayload("schedule-run", t.ID, payload)
	}
	if err != nil {
		return nil, nil, err
	}
	work := &scheduleWork{}
	finish := work.hold()
	return work, func() {
		finish()
		go func() {
			work.active.Wait()
			if !work.uncertain.Load() {
				settle()
			}
		}()
	}, nil
}

func (s *Service) verifyScheduleReceipt(work *scheduleWork, id, status string) {
	if work == nil {
		return
	}
	st := s.store()
	if st == nil || id == "" {
		work.retain()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var runs []ScheduleRun
	ok, err := st.Setting(ctx, runsKey, &runs)
	if err != nil || !ok || !slices.ContainsFunc(runs, func(r ScheduleRun) bool {
		return r.ID == id && r.Status == status
	}) {
		work.retain()
		s.logf("schedule receipt %s status %s requires reconciliation", id, status)
	}
}
