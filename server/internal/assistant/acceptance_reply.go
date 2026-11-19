package assistant

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/lazyre"
)

// passWords accepts bare approval, optionally prefixed with the Chinese word for "verify" and followed by one or two
// Chinese completion particles (as in "ok" plus a particle) or a stop. Question particles stay out, and
// both full-width and ASCII question marks are neither particles nor stops, so questions still go to the agent, and
// negations ("not passed", "can't pass", "no good") never match because the regex anchors on the approval word.
var passWords = lazyre.New(`(?i)^\s*(验收)?(通过|没问题|可以|好的?|ok|okay|lgtm|pass(ed)?|approved?|👍)(?:[了啦呀咯喽呗哒啊]{1,2})?\s*[。.!！]?\s*$`)

// acceptReply answers a reply to a "Please verify" card or a conclusion's (true: it was one): "Pass" marks it verified (a
// decision: takes the option recommended), anything else is the user's change (a decision: their choice), written into the
// todo's decisions and sent to its agent. media: the reply also carried pictures or files; only the words go on, and the
// answer says the rest did not (#5107).
func (s *Service) acceptReply(ctx context.Context, messageID, text string, media bool, ids ...string) bool {
	id, earlier := s.acceptFor(ids...)
	if id == "" {
		return false
	}
	l := s.botLang(ctx)
	text = strings.TrimSpace(text)
	if text == "" {
		if media {
			s.botNote(ctx, messageID, i18n.T(l, "assistant-reports.accept_media_dropped"))
		}
		return true
	}
	if earlier { // a card of a round before the one asking now: answered, it neither accepts nor changes this round
		s.botNote(ctx, messageID, i18n.T(l, "assistant-feishu_cards.already_handled"))
		return true
	}
	t, ok := s.todoNow(id)
	ask, _ := s.askCopy(id)
	parent := slices.IndexFunc(ids, func(mid string) bool { return mid != "" && slices.Contains(ask.Messages, mid) })
	if parent < 0 { // superseded between looking up the reply and reading the round
		s.botNote(ctx, messageID, i18n.T(l, "assistant-feishu_cards.already_handled"))
		return true
	}
	if decideAsk(&ask) && !ask.AcceptedAt.IsZero() { // taken, or its review moved on: over
		s.botNote(ctx, messageID, i18n.T(l, "assistant-feishu_cards.already_handled"))
		return true
	}
	if decideAsk(&ask) && (!ask.RecheckAt.IsZero() || ok && ownerDecisionReturned(t.Supervision)) {
		s.botNote(ctx, messageID, i18n.T(l, "assistant-feishu_cards.already_handled"))
		return true
	}
	// "OK" to a decision with nothing recommended takes nothing: it is their answer, and goes on as one.
	if passWords.MatchString(text) && !(decideAsk(&ask) && ask.Recommend == "") {
		// Approval on a pre-reopen card is answered but neither accepts nor reopens; a gone todo keeps old behavior.
		if ok {
			if a, _ := s.acceptAskLocked(id); a != nil && acceptAskStale(a, t) { // acceptFor read the asks
				s.botNote(ctx, messageID, i18n.T(l, "assistant-feishu_cards.already_handled"))
				return true
			}
		}
		if result, err := s.acceptTry(ctx, id, ids[parent]); errors.Is(err, errAcceptStore) {
			s.botNote(ctx, messageID, i18n.T(l, "http-status.unavailable"))
		} else if err != nil { // the recommendation did not reach the agent: nothing recorded, "OK" can be said again
			s.botNote(ctx, messageID, i18n.T(l, "assistant-reports.accept_not_taken", i18n.Args{"error": clip(oneLine(i18n.Localize(err, l)), 120)}))
		} else if result.undelivered != nil {
			why := clip(oneLine(i18n.Localize(result.undelivered, l)), 120)
			s.botNote(ctx, messageID, i18n.T(l, "assistant-reports.accept_decided_not_sent", i18n.Args{"error": why}))
		} else if result.taken {
			s.botNote(ctx, messageID, acceptedNote(l, &ask))
		} else { // not this round that waits: answered as its button answers (feishu_card_actions.go), not swallowed
			s.botNote(ctx, messageID, i18n.T(l, "assistant-feishu_cards.already_handled"))
		}
		return true
	}
	if !ok {
		s.botNote(ctx, messageID, i18n.T(l, "assistant-reports.accept_gone"))
		return true
	}
	if decideAsk(&ask) {
		result, err := s.answerDecisionInWords(ctx, id, ask.AskedAt.Format(timeKeyLayout), ids[parent], text)
		note := i18n.T(l, "assistant-reports.accept_sent")
		switch {
		case err != nil:
			note = i18n.T(l, "http-status.unavailable")
		case result.undelivered != nil:
			note = i18n.T(l, "assistant-reports.accept_decided_not_sent", i18n.Args{"error": clip(oneLine(i18n.Localize(result.undelivered, l)), 120)})
		case !result.taken:
			note = i18n.T(l, "assistant-feishu_cards.already_handled")
		}
		s.botNote(ctx, messageID, withMediaDropped(l, note, media))
		return true
	}
	if a, _ := s.acceptAskLocked(id); a != nil && acceptAskStale(a, t) { // old round's change is answered, not routed
		s.botNote(ctx, messageID, i18n.T(l, "assistant-feishu_cards.already_handled"))
		return true
	}
	question, msg := i18n.T(l, "assistant-reports.accept_question"), i18n.T(l, "assistant-reports.accept_to_agent", i18n.Args{"feedback": text})
	if ask.Conclusion {
		msg = i18n.T(l, "assistant-reports.verdict_to_agent", i18n.Args{"feedback": text})
	}
	if st := s.todoDB(); st != nil {
		at := decisionNow()
		if ok, err := s.insertDecision(ctx, st, db.TodoDecisionRow{ID: "dec_" + randomHex(8), TodoID: id,
			Question: question, Choice: decisionText(text), Source: db.DecisionManual,
			SourceMessage: "feishu-" + messageID, CreatedAt: at}); err != nil {
			s.logf("task %s: acceptance decision: %v", id, err)
		} else if ok {
			s.decisionsChanged(ctx, id, at)
		}
	}
	// Not accepted: the work goes on. A task the hub (or the user) closed meanwhile is open again before its agent is
	// told, so the turn the change starts is followed (receipt, judgment, the next "Please verify") and the task shows as in
	// progress, not done (#3457). It stays open when the send fails: the change is still to be made.
	if t.StartError == "" {
		if err := s.reopenTodo(ctx, id); err != nil {
			s.logf("task %s: acceptance reopen: %v", id, err)
		}
	}
	if _, err := s.relayToTask(ctx, t.Origin, id, msg, nil); err != nil {
		s.logf("task %s: acceptance to agent: %v", id, err)
		why := clip(oneLine(err.Error()), 120)
		s.botNote(ctx, messageID, withMediaDropped(l, i18n.T(l, "assistant-reports.accept_not_sent", i18n.Args{"error": why}), media))
		return true
	}
	b := &s.accept
	b.mu.Lock()
	if a := b.asks[id]; a != nil && a.AskedAt.Equal(ask.AskedAt) {
		delete(b.asks, id) // changed again: the next deploy asks again
		s.saveAcceptLocked(ctx)
	}
	b.mu.Unlock()
	s.botNote(ctx, messageID, withMediaDropped(l, i18n.T(l, "assistant-reports.accept_sent"), media))
	return true
}

// answerDecisionInWords handles both Feishu text replies and form submissions. The task's action lock
// serializes them with option buttons and default decisions, and the round owns one decision row.
func (s *Service) answerDecisionInWords(ctx context.Context, id, round, card, text string) (acceptResult, error) {
	s.reviewDismissals.actions.acquire(id)
	defer s.reviewDismissals.actions.release(id)
	if card == "" || round == "" {
		return acceptResult{}, nil
	}
	a, err := s.askRound(id, round)
	if err != nil || a == nil {
		return acceptResult{}, err
	}
	if card != a.MessageID && !slices.Contains(a.Messages, card) {
		return acceptResult{}, nil
	}
	t, _ := s.todoNow(id)
	completeDismissal, err := s.prepareDismissalForAnswer(ctx, t)
	if err != nil {
		return acceptResult{}, err
	}
	l := s.botLang(ctx)
	choice := decisionText(text)
	if st := s.todoDB(); st != nil {
		at := decisionNow()
		inserted, err := s.insertDecision(ctx, st, db.TodoDecisionRow{ID: "dec_" + randomHex(8), TodoID: id,
			Question: cmp.Or(a.Reason, i18n.T(l, "assistant-reports.verdict_decide")), Choice: choice,
			Source: db.DecisionManual, SourceMessage: "feishu-decision-" + a.AskedAt.Format(timeKeyLayout), CreatedAt: at})
		if err != nil || !inserted {
			return acceptResult{}, err
		}
		s.decisionsChanged(ctx, id, at)
	}
	s.deadlineAnswered(ctx, id, a.AskedAt)
	msg := i18n.T(l, "assistant-reports.verdict_decided_to_agent", i18n.Args{"decision": choice})
	_, sendErr := s.relayToTask(ctx, t.Origin, id, msg, func(cur Todo) bool {
		return !acceptAskStale(a, cur) && !acceptAskEnded(a, cur)
	})
	// Agent delivery can outlive its caller. The recorded answer still has to finish and redraw.
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer cancel()
	result := acceptSucceeded()
	if sendErr != nil {
		s.logf("task %s: decision to agent: %v", id, sendErr)
		result = s.decisionNotDelivered(finish, id, a.AskedAt, choice, sendErr)
	} else {
		s.userDecided(finish, id)
	}
	s.endAskWithAnswer(finish, id, a.AskedAt, "", choice, true)
	completeDismissal()
	return result, nil
}

// withMediaDropped is note with, when the reply carried pictures or files, the line saying they did not go to the agent.
func withMediaDropped(l i18n.Lang, note string, media bool) string {
	if !media {
		return note
	}
	return note + "\n" + i18n.T(l, "assistant-reports.accept_media_dropped")
}

// acceptResult is what came of one accept operation. The zero value means the round was not taken. undelivered is
// non-nil only for a decision that was recorded and handed to its supervisor because no Agent could receive it.
type acceptResult struct {
	taken       bool
	undelivered error
}

func acceptSucceeded() acceptResult { return acceptResult{taken: true} }

// decisionNotDelivered finishes a recorded decision whose Agent could not take it. The supervisor gets one handoff per
// ask round; the hub does not resend it. The caller owns the decision row and the ask's answered marker.
func (s *Service) decisionNotDelivered(ctx context.Context, id string, askedAt time.Time, choice string, cause error) acceptResult {
	l := s.botLang(ctx)
	s.userDecided(ctx, id)
	s.signal(superviseSignal{Todo: id, Kind: sigIdle, Key: "undelivered|" + askedAt.UTC().Format(time.RFC3339Nano),
		Detail: i18n.T(l, "assistant-reports.decision_not_delivered", i18n.Args{"decision": decisionText(choice),
			"error": clip(oneLine(i18n.Localize(cause, l)), 120)})})
	return acceptResult{taken: true, undelivered: cause}
}

// acceptTry marks task id verified; result.taken is false when already handled, never asked, or asked before the latest
// card-less ask (NoCard) can be accepted too, from the web app. card is the message pressed ("" from a reply, which
// acceptFor already matched, from the web app, or when Feishu does not say): a card that is not this round's (an
// earlier round's, once the next one has its own) accepts nothing (#4853). A decision (a conclusion that needs the
// owner) is accepted by taking the option it recommends, which goes to the task's agent (takeRecommendation); one with
// nothing recommended, or no longer waiting, takes nothing. The error is a decision's whose recommendation did not
// reach the task's agent, or errAcceptStore when the asks cannot be read or the acceptance cannot be saved: either way
// nothing was taken, the ask still waits, and the owner can try again.
func (s *Service) acceptTry(ctx context.Context, id, card string) (acceptResult, error) {
	return s.acceptTryAs(ctx, id, card, false)
}

// acceptTryAs is acceptTry; byDefault: nobody answered decision id by its deadline and the hub takes its recommendation
// (sweepTaskDeadlines), which only a reversible decision past its deadline allows.
func (s *Service) acceptTryAs(ctx context.Context, id, card string, byDefault bool) (acceptResult, error) {
	s.reviewDismissals.actions.acquire(id)
	defer s.reviewDismissals.actions.release(id)
	return s.acceptTryAsDismissalLocked(ctx, id, card, byDefault)
}

// acceptTryAsDismissalLocked holds only this task's action lock across its send, not the book mutex.
func (s *Service) acceptTryAsDismissalLocked(ctx context.Context, id, card string, byDefault bool) (acceptResult, error) {
	t, todoOK := s.todoNow(id)
	if todoOK && byDefault { // Hidden is not answered: no default is taken for a dismissed round
		if dismissed, err := s.decisionDismissed(ctx, t); err != nil || dismissed {
			return acceptResult{}, err
		}
	}
	b := &s.accept
	b.mu.Lock()
	if !s.loadAcceptLocked(ctx) {
		b.mu.Unlock()
		return acceptResult{}, errAcceptStore
	}
	a, ok := b.asks[id]
	if !ok || a.AskedAt.IsZero() || !a.AcceptedAt.IsZero() || (todoOK && acceptAskStale(a, t)) || (card != "" && card != a.MessageID && !slices.Contains(a.Messages, card)) ||
		decideAsk(a) && (a.Recommend == "" || todoOK && acceptAskEnded(a, t) || !a.RecheckAt.IsZero() || todoOK && ownerDecisionReturned(t.Supervision)) ||
		byDefault && !(decideAsk(a) && a.Reversible && !a.Deadline.IsZero() && a.DueAt.IsZero() && !time.Now().Before(a.Deadline)) {
		b.mu.Unlock()
		return acceptResult{}, nil
	}
	completeDismissal := func() {}
	if todoOK && !byDefault {
		var err error
		completeDismissal, err = s.prepareDismissalForAnswer(ctx, t)
		if err != nil {
			b.mu.Unlock()
			return acceptResult{}, err
		}
	}
	previousDueAt := a.DueAt
	a.AcceptedAt = time.Now().UTC()
	if byDefault {
		a.ByDefault, a.DueAt = true, a.AcceptedAt
	}
	if err := s.saveAcceptErrLocked(ctx); err != nil {
		// Kept only in memory, it would wait again after a restart: not accepted, and nothing that follows one runs.
		a.AcceptedAt, a.ByDefault, a.DueAt = time.Time{}, false, previousDueAt
		b.mu.Unlock()
		s.logf("task %s: acceptance: save: %v", id, err)
		return acceptResult{}, fmt.Errorf("%w: %w", errAcceptStore, err)
	}
	cp := *a
	b.mu.Unlock()
	s.askChanged(ctx) // it no longer waits
	if !decideAsk(&cp) && !cp.Conclusion {
		completeDismissal()
		s.taskVerified(ctx, id, cp)
		return acceptSucceeded(), nil
	}
	result := acceptSucceeded()
	switch {
	case decideAsk(&cp):
		var err error
		result, err = s.takeRecommendation(ctx, id, cp)
		if err != nil {
			// Not taken after all: the owner can take it again, and is told why it did not go.
			b.mu.Lock()
			if a := b.asks[id]; a != nil && a.AskedAt.Equal(cp.AskedAt) {
				// A default that did not go is not tried again (it may have reached the agent after all): its deadline stays
				// handled (DueAt) and the sweep reminds the owner instead.
				a.AcceptedAt, a.ByDefault = time.Time{}, false
				s.saveAcceptLocked(ctx)
			}
			b.mu.Unlock()
			s.askChanged(ctx)
			return acceptResult{}, err
		}
	case cp.Conclusion:
		// Seen: the review already closed the task and told where it came from; "verified" is for deployed work.
	}
	completeDismissal()
	s.updateAskCards(ctx, id, cp)
	return result, nil
}

// taskVerified is what follows deployed task id's "Please verify" a passed (by the owner or its supervisor): the task is
// verified, its todo's chat thread and where the request came from hear it, and its cards say so.
func (s *Service) taskVerified(ctx context.Context, id string, a acceptAsk) {
	s.setTaskStageKind(ctx, id, "verify", "", "", true)
	s.threadPost(id, threadEvent{kind: threadTodoAccepted}) // and the todo's chat thread
	s.sourceReportSoon(id, sourceReportWait)                // and where the request came from (todo_source.go)
	s.updateAskCards(ctx, id, a)
}

// updateAskCards redraws every card of task id's ask a as it is now.
func (s *Service) updateAskCards(ctx context.Context, id string, a acceptAsk) {
	t, ok := s.todoNow(id)
	if !ok {
		return
	}
	for _, mid := range a.Messages {
		if err := s.botUpdateCard(ctx, mid, s.acceptCard(ctx, t, a, false)); err != nil {
			s.logf("task %s: acceptance card: %v", id, err)
		}
	}
}
