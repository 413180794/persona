package assistant

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// The node sends separate supervision instructions only through its native Claude
// and Codex paths. Older nodes leave Agent empty for native Claude conversations.
func runtimeSupervisionUnavailable(c proto.Session) bool {
	return c.SourceKind == proto.SourceRuntime || c.Agent != "" && assistantKind(c.Agent) == ""
}

// Repair ledgers written before task_plan checked the runtime. The origin still
// owns the card and receives taskEndedNotice. A missing node listing proves nothing.
func (s *Service) rehomeRuntimeSupervision(ctx context.Context, cfg Settings) map[string]bool {
	if cfg.MachineID == "" {
		return nil
	}
	rows, err := s.sessions(ctx, cfg.MachineID)
	if err != nil {
		return nil
	}
	runtimes := map[string]bool{}
	for i := range rows {
		if rows[i].Assistant && runtimeSupervisionUnavailable(rows[i]) {
			runtimes[rows[i].SessionID] = true
		}
	}
	if cfg.Main != "" && len(runtimes) > 0 {
		if err := s.ensureTodosLoaded(ctx); err != nil {
			return runtimes
		}
		for _, t := range s.todosWhere(ctx, "*", func(t *Todo) bool {
			return t.DoneAt == nil && t.Supervision != nil && t.Supervision.Supervisor != cfg.Main && runtimes[t.Supervision.Supervisor]
		}) {
			_, _, err := s.updateSupervision(ctx, t.ID, func(v *Supervision) bool {
				if v.Supervisor != t.Supervision.Supervisor {
					return false
				}
				v.Supervisor = cfg.Main
				return true
			})
			if err != nil {
				s.logf("task %s: hand runtime supervision over: %v", t.ID, err)
				return runtimes // retry without moving the report ahead of its ledger
			}
		}
	}

	// A tagged message can outlive both its source listing and its assigned
	// background supervisor. Old, untagged relays to the secretary retain their
	// existing escalation semantics.
	b := &s.relays
	b.mu.Lock()
	var waiting []string
	if s.relayLocked(ctx) {
		own := s.ownSupervisor(cfg)
		for id, queue := range b.st.Queue {
			away := id != own || cfg.secretary(id)
			raw := runtimes[id] && !cfg.secretary(id)
			if slices.ContainsFunc(queue, func(m relayMsg) bool {
				return m.Tool == superviseTool && (raw || m.NeedsSupervisor && away)
			}) {
				waiting = append(waiting, id)
			}
		}
	}
	b.mu.Unlock()
	if len(waiting) == 0 {
		return runtimes
	}
	to := cfg.Main
	bg, gone := s.ensureBackground(ctx)
	if bg != "" && !runtimes[bg] {
		to = bg
	}
	if gone != "" {
		if lost := s.backgroundLost(ctx, gone, cmp.Or(bg, cfg.Main), s.webBase(ctx)); len(lost) > 0 {
			s.restoreDigest(lost, time.Time{}, time.Time{})
		}
	}
	if policy, err := s.supervisionInstructions(ctx, cfg.MachineID, to); err != nil || policy == "" {
		to = ""
	}
	for _, from := range waiting {
		if from != to && s.moveRuntimeRelays(ctx, from, to, runtimes[from] && !cfg.secretary(from)) {
			if to != "" {
				if runtimes[from] && !cfg.secretary(from) {
					s.supervise.mu.Lock()
					s.supervise.setQueuedLocked(from, nil)
					s.supervise.mu.Unlock()
				}
				s.drainConversations(cfg.MachineID, []string{to})
			}
			s.publish(ctx)
		}
	}
	return runtimes
}

// Move only the rendered supervision messages in one relay-setting write. Keeping
// their timestamps and the existing dedup reservations preserves evidence even if
// the manager restarted after queue admission. Ordinary relays stay at their origin.
func (s *Service) moveRuntimeRelays(ctx context.Context, from, to string, runtime bool) bool {
	b := &s.relays
	b.mu.Lock()
	defer b.mu.Unlock()
	if !s.relayLocked(ctx) {
		return false
	}
	src, dst := b.st.Queue[from], b.st.Queue[to]
	var keep []relayMsg
	moved := false
	for i, m := range src {
		if i == 0 && b.draining[from] || m.Tool != superviseTool || !runtime && !m.NeedsSupervisor || to == "" && m.NeedsSupervisor {
			keep = append(keep, m)
			continue
		}
		m.NeedsSupervisor = true
		if to == "" {
			keep = append(keep, m) // visibly waiting; retry when a compatible supervisor is available
		} else {
			b.st.Queue[to] = append(b.st.Queue[to], m)
		}
		moved = true
	}
	if !moved {
		return false
	}
	if len(keep) == 0 {
		delete(b.st.Queue, from)
	} else {
		b.st.Queue[from] = keep
	}
	if err := s.saveRelayLocked(); err != nil {
		b.st.Queue[from] = src
		if to != "" {
			b.st.Queue[to] = dst
		}
		return false
	}
	return true
}
