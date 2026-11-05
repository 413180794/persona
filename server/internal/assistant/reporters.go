package assistant

import (
	"context"
	"sync"
	"time"
)

// Reporting conversations (#3192, docs/lark-agent.md「派出去的任务」): a conversation started with report_tasks (a
// plugin's /agent asks for it) is told about the tasks it opens as the main conversation is about its quiet ones
// (quiet.go): they are quiet, and each notice goes into it, so it can hand the result on (the /agent conversation
// replies in Feishu). Kept in the settings table: a task outlives the hub's process.

const reportersKey = "assistant.task_reporters"

// reporterKeep is how long a reporting conversation stays one after it was last started or marked.
const reporterKeep = 14 * 24 * time.Hour

type reporter struct {
	MachineID string    `json:"machine_id"`
	At        time.Time `json:"at"`
}

type reporterBook struct {
	mu     sync.Mutex
	loaded bool
	m      map[string]reporter // by session id
}

// reportersLocked reads the book once; b.mu is held. False while it cannot be read.
func (s *Service) reportersLocked(ctx context.Context) bool {
	b := &s.reporters
	if b.loaded {
		return true
	}
	m := map[string]reporter{}
	if st := s.store(); st != nil {
		if _, err := st.Setting(ctx, reportersKey, &m); err != nil {
			s.logf("assistant: reporting conversations: %v", err)
			return false
		}
	}
	b.m, b.loaded = m, true
	return true
}

// markReporter makes conversation sid on machine one that is told about the tasks it opens.
func (s *Service) markReporter(ctx context.Context, machine, sid string) {
	if machine == "" || sid == "" {
		return
	}
	b := &s.reporters
	b.mu.Lock()
	defer b.mu.Unlock()
	if !s.reportersLocked(ctx) {
		return
	}
	now := time.Now().UTC()
	for id, r := range b.m {
		if now.Sub(r.At) > reporterKeep {
			delete(b.m, id)
		}
	}
	b.m[sid] = reporter{MachineID: machine, At: now}
	if st := s.store(); st != nil {
		if err := st.PutSetting(ctx, reportersKey, b.m); err != nil {
			s.logf("assistant: save reporting conversation %s: %v", short(sid), err)
		}
	}
}

// reporterOf is the machine of reporting conversation sid; false when it is none.
func (s *Service) reporterOf(ctx context.Context, sid string) (string, bool) {
	if sid == "" {
		return "", false
	}
	b := &s.reporters
	b.mu.Lock()
	defer b.mu.Unlock()
	if !s.reportersLocked(ctx) {
		return "", false
	}
	r, ok := b.m[sid]
	if !ok || time.Since(r.At) > reporterKeep {
		return "", false
	}
	return r.MachineID, true
}
