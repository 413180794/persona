package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/feishubot"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/prompts"
)

func TestDecisionReasonIsShortAndComplete(t *testing.T) {
	s, f, todo, posts, _ := conclusionService(t, true)
	waitsForReview(t, s, f, todo.ID, "Please check the two options.")
	args := rolloutDecision()
	reason := strings.Repeat("字", 119) + "？"
	args["reason"] = reason + "字"
	if _, err := reviewErr(s, todo.ID, args); err == nil || !strings.Contains(err.Error(), "120 characters") {
		t.Fatalf("overlong reason: %v", err)
	}
	if len(posts()) != 0 {
		t.Fatal("a refused reason sent a card")
	}
	args["reason"] = reason
	reviewTask(t, s, todo.ID, args)
	if got := askOf(s, todo.ID).Reason; got != reason {
		t.Fatalf("reason = %q", got)
	}
	if list := posts(); len(cardsWith(list, reason)) != 1 {
		t.Fatalf("complete reason missing from card: %v", list)
	}
}

func TestDecisionPromptUsesEverydayLanguage(t *testing.T) {
	tool := prompts.Tool("task_review")
	for name, prompt := range map[string]string{
		"supervision": prompts.RenderSupervisionInstructions(),
		"tool":        tool.Description + string(tool.InputSchema),
	} {
		for _, phrase := range []string{"everyday language", "120 characters", "leads_to", "cost_of_waiting", "internal IDs", "log timestamps", "log wording", "jargon"} {
			if !strings.Contains(prompt, phrase) {
				t.Errorf("%s prompt is missing %q", name, phrase)
			}
		}
	}
}

func TestFeishuDecisionInWordsRedrawsOriginalCard(t *testing.T) {
	for _, method := range []string{"form", "slow form", "reply", "concurrent", "legacy primary only with retry"} {
		t.Run(method, func(t *testing.T) {
			database := todoTestDB(t)
			s, f, todo, posts, _ := conclusionServiceOn(t, database, true)
			ctx := context.Background()
			waitsForReview(t, s, f, todo.ID, "Please check the two options.")
			reviewTask(t, s, todo.ID, dropTableDecision())
			ask := askOf(s, todo.ID)
			legacy := method == "legacy primary only with retry"
			if legacy {
				s.accept.mu.Lock()
				s.accept.asks[todo.ID].Messages = nil
				s.saveAcceptLocked(ctx)
				s.accept.mu.Unlock()
			}
			l := s.botLang(ctx)
			cards := posts()
			if len(cards) != 1 {
				t.Fatalf("expected one card, got %v", cards)
			}
			for _, want := range []string{`"schema":"2.0"`, `"tag":"form"`, `"tag":"input"`, `"name":"custom"`,
				`"form_action_type":"submit"`, i18n.T(l, "assistant-reports.decision_never_default")} {
				if !strings.Contains(cards[0], want) {
					t.Fatalf("pending card missing %q: %s", want, cards[0])
				}
			}
			updates := make(chan map[string]string, 4)
			failUpdate := legacy
			f.answerApp(AppFeishu, FeishuUpdateCard, func(p json.RawMessage) (any, error) {
				var fields map[string]string
				err := json.Unmarshal(p, &fields)
				updates <- fields
				if failUpdate {
					return nil, errors.New("card action is lock")
				}
				return map[string]any{}, err
			})
			form := func(round, card, text string) *feishubot.CardResponse {
				raw, err := json.Marshal(map[string]any{
					"action": map[string]any{
						"value":      map[string]string{"a": "accept", "id": todo.ID, "asked_at": round, "free": "1"},
						"form_value": map[string]string{"custom": text},
					},
					"context": map[string]string{"open_message_id": card},
				})
				if err != nil {
					t.Error(err)
					return &feishubot.CardResponse{Toast: &feishubot.Toast{}}
				}
				response, err := s.FeishuAction(ctx, FeishuAction{Event: raw})
				if err != nil || response == nil || response.Toast == nil {
					t.Errorf("Feishu callback = %+v, %v", response, err)
					return &feishubot.CardResponse{Toast: &feishubot.Toast{}}
				}
				return response
			}
			before := len(sentToAgent(f))
			round := ask.AskedAt.Format(timeKeyLayout)
			const instruction = "只给同事看一份草稿"
			if form(round, ask.MessageID, "  ").Toast.Type != "warning" ||
				form(ask.AskedAt.Add(-time.Second).Format(timeKeyLayout), ask.MessageID, instruction).Toast.Type != "info" ||
				form(round, "om_forwarded", instruction).Toast.Type != "info" ||
				!askOf(s, todo.ID).AcceptedAt.IsZero() || len(sentToAgent(f)) != before {
				t.Fatal("a blank or stale callback consumed the decision")
			}
			reply := func() {
				if !s.acceptReply(ctx, "om_words", instruction, false, ask.MessageID) {
					t.Error("reply not recognized")
				}
			}
			checkCard := func(card string) {
				t.Helper()
				if !strings.Contains(card, i18n.T(l, "assistant-reports.decision_custom_noted", i18n.Args{"instruction": instruction})) {
					t.Errorf("answered card is missing the instruction: %s", card)
				}
				for _, absent := range []string{`"tag":"input"`, i18n.T(l, "assistant-reports.verdict_decide"),
					i18n.T(l, "assistant-reports.decision_never_default"), ask.CostOfWaiting, ask.Options[0].LeadsTo} {
					if strings.Contains(card, absent) {
						t.Errorf("answered card still shows %q", absent)
					}
				}
			}
			switch method {
			case "form", "legacy primary only with retry":
				got := form(round, ask.MessageID, "  "+instruction+"  ")
				if got.Toast.Type != "success" || got.Card == nil || got.Card.Type != "raw" {
					t.Fatalf("submit: %+v", got)
				}
				card, ok := got.Card.Data.(*feishubot.Card)
				if !ok || card.Schema != "2.0" {
					t.Fatalf("callback replacement: %+v", got.Card)
				}
				checkCard(card.JSON())
				s.retryDecisionCardRedraws(ctx, askOf(s, todo.ID).AcceptedAt)
				if len(updates) != 0 {
					t.Fatal("PATCH ran before the callback window had elapsed")
				}
			case "slow form":
				wait := cardAnswerWait
				cardAnswerWait = 50 * time.Millisecond
				defer func() { cardAnswerWait = wait }()
				hold, release := heldAnswer(t)
				f.mu.Lock()
				f.onSend = func(*fakeHub, map[string]any) { hold() }
				f.mu.Unlock()
				got := form(round, ask.MessageID, instruction)
				release()
				if got.Toast.Type != "info" || got.Card != nil || len(updates) != 0 {
					t.Fatalf("slow submit's initial callback: %+v", got)
				}
				waitFor(t, "the answer saved for redraw", func() bool { return askOf(s, todo.ID).Custom })
			case "reply":
				reply()
			case "concurrent":
				var wg sync.WaitGroup
				wg.Go(reply)
				wg.Go(func() { form(round, ask.MessageID, instruction) })
				wg.Wait()
			}
			sweepAt := askOf(s, todo.ID).AcceptedAt.Add(decisionCardCallbackWindow)
			s.retryDecisionCardRedraws(ctx, sweepAt)
			if legacy {
				select {
				case <-updates: // Feishu refused the first PATCH.
				case <-time.After(3 * time.Second):
					t.Fatal("primary-only card was not selected for redraw")
				}
				if len(askOf(s, todo.ID).DecisionCardsRedrawn) != 0 {
					t.Fatal("a failed PATCH was recorded as successful")
				}
				failUpdate = false
				s.accept.mu.Lock()
				s.accept.asks, s.accept.loaded = nil, false // reload the persisted answer as on restart
				s.accept.mu.Unlock()
				s.retryDecisionCardRedraws(ctx, sweepAt)
			}
			select {
			case update := <-updates:
				if update["message_id"] != ask.MessageID {
					t.Fatalf("unexpected card update: %v", update)
				}
				checkCard(update["card"])
			case <-time.After(3 * time.Second):
				t.Fatal("original card was not updated")
			}
			if form(round, ask.MessageID, "different text").Toast.Type != "info" {
				t.Fatal("duplicate submission was accepted")
			}
			reply()
			if got := sentToAgent(f)[before:]; len(got) != 1 || !strings.Contains(got[0], instruction) {
				t.Fatalf("expected one delivery of the instruction, got %q", got)
			}
			rows, err := database.ListTodoDecisions(ctx, todo.ID, true)
			if err != nil || len(rows) != 1 || rows[0].Choice != instruction {
				t.Fatalf("expected one saved choice, got %v, %v", rows, err)
			}
			if got := askOf(s, todo.ID); got.AcceptedAt.IsZero() || !got.Custom || got.Chosen != instruction ||
				!slices.Equal(got.DecisionCardsRedrawn, []string{ask.MessageID}) {
				t.Fatalf("ask = %+v", got)
			}
			s.retryDecisionCardRedraws(ctx, sweepAt)
			if len(updates) != 0 {
				t.Fatal("duplicate card redraw")
			}
			s.accept.mu.Lock()
			next := s.accept.asks[todo.ID]
			next.AskedAt = next.AskedAt.Add(time.Second)
			next.MessageID, next.Messages, next.DecisionCardsRedrawn = "om_new", []string{"om_new"}, nil
			s.accept.mu.Unlock()
			s.reviewDismissals.actions.acquire(todo.ID)
			s.redrawDecisionCards(ctx, todo.ID, ask.AskedAt)
			s.reviewDismissals.actions.release(todo.ID)
			if len(updates) != 0 {
				t.Fatal("a retry for the old round redrew the new round's card")
			}
		})
	}
}
