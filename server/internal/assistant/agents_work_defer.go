package assistant

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// deferredWorkOption only interprets an option actually offered on this card. A
// free-text answer, or one part of a multiple selection, remains an agent answer.
func deferredWorkOption(q proto.Question, answer string) bool {
	if q.MultiSelect {
		return false
	}
	for _, option := range q.Options {
		if option.Label != answer {
			continue
		}
		if option.Action == proto.QuestionOptionDefer {
			return true
		}
		// Legacy cards predate structured option actions. Migrate this explicit
		// pause label only; substring matching could discard a conditional answer.
		return option.Action == "" && legacyDeferredWorkLabel(answer)
	}
	return false
}

// legacyDeferredWorkLabel recognizes a standalone pause followed by an explicit
// postponement. The bounded suffixes intentionally exclude conditional requests.
func legacyDeferredWorkLabel(label string) bool {
	parts := strings.FieldsFunc(label, func(r rune) bool { return r == ',' || r == '，' })
	if len(parts) != 2 {
		return false
	}
	switch canonicalAnswer(strings.TrimSpace(parts[0])) {
	case answerSkip, answerNotNow, answerHold:
	default:
		return false
	}
	switch strings.TrimSpace(parts[1]) {
	case "稍后再处理", "稍后再验收", "稍后再决定", "稍后再确认", "稍后再回复", "稍后再讨论", "稍后再继续", "稍后再说", "以后再说": // i18n-ignore: compatibility with persisted model-generated option labels
		return true
	default:
		return false
	}
}

// deferWorkCard commits the decision before acknowledging it or removing the
// pending card. A failed write leaves the same card retryable; neither path sends
// an answer to the agent or changes another turn's work.
func (s *Service) deferWorkCard(ctx context.Context, ix proto.Interaction, line string) error {
	key := [2]string{ix.MachineID, ix.SessionID}
	unlock := s.lockWorkWrite(key)
	defer unlock()
	s.work.mu.Lock()
	e, ok := s.work.entries[key]
	if !ok || e.InteractionID != ix.ID {
		delete(s.work.cards, ix.ID)
		s.work.mu.Unlock()
		s.publishSoon()
		return nil
	}
	saved := *e
	saved.InteractionID, saved.Work.InteractionID = "", ""
	saved.WakeAt, saved.Work.WakeAt = nil, nil
	saved.Work.Deferred = true
	saved.UpdatedAt = time.Now().UTC()
	if line != "" {
		saved.Work.Line = line
	}
	s.work.mu.Unlock()
	if st := s.workDB(); st != nil {
		writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := st.PutSessionWork(writeCtx, saved)
		cancel()
		if err != nil {
			s.work.mu.Lock()
			if current := s.work.entries[key]; current != nil && current.InteractionID == ix.ID {
				s.work.cards[ix.ID] = ix
			}
			s.work.mu.Unlock()
			return fmt.Errorf("save deferred work card: %w", err)
		}
	}
	s.work.mu.Lock()
	if current := s.work.entries[key]; current != nil && current.TurnAt.Equal(saved.TurnAt) {
		// A tick can clear this card while the database write is in flight.
		// Merge the terminal decision into the same turn's latest state so its
		// queued write cannot remove Deferred; a genuinely new turn wins.
		current.InteractionID, current.Work.InteractionID = "", ""
		current.WakeAt, current.Work.WakeAt = nil, nil
		current.Work.Deferred = true
		if line != "" {
			current.Work.Line = line
		}
		if current.UpdatedAt.Before(saved.UpdatedAt) {
			current.UpdatedAt = saved.UpdatedAt
		}
	}
	delete(s.work.cards, ix.ID)
	delete(s.work.approved, key)
	s.work.mu.Unlock()
	s.publishSoon()
	return nil
}

// lockWorkWrite orders persistence for one session without blocking snapshots or
// the work of other sessions on a database round trip.
func (s *Service) lockWorkWrite(key [2]string) func() {
	s.work.mu.Lock()
	s.work.init()
	if s.work.writes == nil {
		s.work.writes = map[[2]string]*sync.Mutex{}
	}
	m := s.work.writes[key]
	if m == nil {
		m = &sync.Mutex{}
		s.work.writes[key] = m
	}
	s.work.mu.Unlock()
	m.Lock()
	return m.Unlock
}
