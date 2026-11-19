package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

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
	for _, method := range []string{"form", "reply", "concurrent"} {
		t.Run(method, func(t *testing.T) {
			database := todoTestDB(t)
			s, f, todo, posts, _ := conclusionServiceOn(t, database, true)
			ctx := context.Background()
			waitsForReview(t, s, f, todo.ID, "Please check the two options.")
			reviewTask(t, s, todo.ID, dropTableDecision())
			ask := askOf(s, todo.ID)
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
			f.answerApp(AppFeishu, FeishuUpdateCard, func(p json.RawMessage) (any, error) {
				var fields map[string]string
				err := json.Unmarshal(p, &fields)
				updates <- fields
				return map[string]any{}, err
			})
			form := func(round, card, text string) string {
				raw, err := json.Marshal(map[string]any{
					"action": map[string]any{
						"value":      map[string]string{"a": "accept", "id": todo.ID, "asked_at": round, "free": "1"},
						"form_value": map[string]string{"custom": text},
					},
					"context": map[string]string{"open_message_id": card},
				})
				if err != nil {
					t.Error(err)
					return ""
				}
				response, err := s.FeishuAction(ctx, FeishuAction{Event: raw})
				if err != nil || response == nil || response.Toast == nil {
					t.Errorf("Feishu callback = %+v, %v", response, err)
					return ""
				}
				return response.Toast.Type
			}
			before := len(sentToAgent(f))
			round := ask.AskedAt.Format(timeKeyLayout)
			const instruction = "只给同事看一份草稿"
			if form(round, ask.MessageID, "  ") != "warning" ||
				form(ask.AskedAt.Add(-time.Second).Format(timeKeyLayout), ask.MessageID, instruction) != "info" ||
				form(round, "om_forwarded", instruction) != "info" ||
				!askOf(s, todo.ID).AcceptedAt.IsZero() || len(sentToAgent(f)) != before {
				t.Fatal("a blank or stale callback consumed the decision")
			}
			reply := func() {
				if !s.acceptReply(ctx, "om_words", instruction, false, ask.MessageID) {
					t.Error("reply not recognized")
				}
			}
			switch method {
			case "form":
				if got := form(round, ask.MessageID, "  "+instruction+"  "); got != "success" {
					t.Fatalf("submit: %q", got)
				}
			case "reply":
				reply()
			case "concurrent":
				var wg sync.WaitGroup
				wg.Go(reply)
				wg.Go(func() { form(round, ask.MessageID, instruction) })
				wg.Wait()
			}
			select {
			case update := <-updates:
				card := update["card"]
				if update["message_id"] != ask.MessageID || !strings.Contains(card, i18n.T(l, "assistant-reports.decision_custom_noted", i18n.Args{"instruction": instruction})) {
					t.Fatalf("unexpected card update: %v", update)
				}
				for _, absent := range []string{`"tag":"input"`, i18n.T(l, "assistant-reports.verdict_decide"),
					i18n.T(l, "assistant-reports.decision_never_default"), ask.CostOfWaiting, ask.Options[0].LeadsTo} {
					if strings.Contains(card, absent) {
						t.Errorf("answered card still shows %q", absent)
					}
				}
			case <-time.After(3 * time.Second):
				t.Fatal("original card was not updated")
			}
			if form(round, ask.MessageID, "different text") != "info" {
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
			if got := askOf(s, todo.ID); got.AcceptedAt.IsZero() || !got.Custom || got.Chosen != instruction {
				t.Fatalf("ask = %+v", got)
			}
			if len(updates) != 0 {
				t.Fatal("duplicate card redraw")
			}
		})
	}
}
