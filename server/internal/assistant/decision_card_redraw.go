package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/nwverse/persona/server/internal/feishubot"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Feishu can lock a JSON 2.0 card during its callback. Leave the full response
// window before PATCHing when the owner used the form.
const decisionCardCallbackWindow = 3 * time.Second

// Bound automatic retries for messages that may have been recalled or expired.
const decisionCardRedrawWindow = 14 * 24 * time.Hour

func askCardIDs(a acceptAsk) []string {
	var ids []string
	for _, mid := range slices.Concat([]string{a.MessageID}, a.Messages) {
		if mid != "" && !slices.Contains(ids, mid) {
			ids = append(ids, mid)
		}
	}
	return ids
}

func (s *Service) answeredDecisionCard(ctx context.Context, id, round, mid string) *feishubot.CardPayload {
	a, ok := s.askCopy(id)
	t, found := s.todoNow(id)
	if !ok || !found || !a.Custom || a.AcceptedAt.IsZero() ||
		a.AskedAt.Format(timeKeyLayout) != round || !a.hasCard(mid) {
		return nil
	}
	return s.acceptCard(ctx, t, a, false).Raw()
}

// retryDecisionCardRedraws also picks up custom answers persisted before redraws
// had receipts. It only updates existing cards and never relays a decision.
func (s *Service) retryDecisionCardRedraws(ctx context.Context, now time.Time) {
	b := &s.accept
	b.mu.Lock()
	if !s.loadAcceptLocked(ctx) {
		b.mu.Unlock()
		return
	}
	due := make(map[string]time.Time)
	for id, a := range b.asks {
		if a.Custom && !a.AcceptedAt.IsZero() && !now.Before(a.AcceptedAt.Add(decisionCardCallbackWindow)) &&
			now.Before(maxTime(a.AskedAt, a.RemindedAt).Add(decisionCardRedrawWindow)) {
			for _, mid := range askCardIDs(*a) {
				if !slices.Contains(a.DecisionCardsRedrawn, mid) {
					due[id] = a.AskedAt
					break
				}
			}
		}
	}
	b.mu.Unlock()
	for id, asked := range due {
		if ctx.Err() != nil {
			return
		}
		s.reviewDismissals.actions.acquire(id)
		s.redrawDecisionCards(ctx, id, asked)
		s.reviewDismissals.actions.release(id)
	}
}

// redrawDecisionCards is called under the task's action lock.
func (s *Service) redrawDecisionCards(ctx context.Context, id string, asked time.Time) {
	a, ok := s.askCopy(id)
	t, found := s.todoNow(id)
	if !ok || !found || !a.AskedAt.Equal(asked) || !a.Custom || a.AcceptedAt.IsZero() {
		return
	}
	card := s.workbenchStamp(ctx, s.acceptCard(ctx, t, a, false)).JSON()
	b := &s.accept
	for _, mid := range askCardIDs(a) {
		if slices.Contains(a.DecisionCardsRedrawn, mid) {
			continue
		}
		round := asked.Format(timeKeyLayout)
		s.logf("task %s: decision card: round=%s message=%s PATCH started", id, round, mid)
		call, cancel := context.WithTimeout(ctx, 10*time.Second)
		var receipt feishubot.CardUpdateReceipt
		err := s.appCall(call, AppFeishu, FeishuUpdateCard, map[string]string{"message_id": mid, "card": card}, &receipt)
		cancel()
		if err != nil {
			var re *rpc.Error
			if errors.As(err, &re) && re.Code == CodeFeishuAPI {
				_ = json.Unmarshal(re.Data, &receipt)
			}
		}
		s.logf("task %s: decision card: round=%s message=%s PATCH ok=%t http=%d code=%d request_id=%q",
			id, round, mid, err == nil, receipt.HTTPStatus, receipt.Code, receipt.RequestID)
		if err == nil {
			b.mu.Lock()
			if cur := b.asks[id]; cur != nil && cur.AskedAt.Equal(asked) {
				cur.DecisionCardsRedrawn = append(slices.Clone(cur.DecisionCardsRedrawn), mid)
				s.saveAcceptLocked(ctx)
			}
			b.mu.Unlock()
		}
	}
}
