package hub

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// interactionAnswerHub is a hub with one machine and one agent session whose question waits for an answer.
func interactionAnswerHub(t *testing.T) (*Hub, *Machine, proto.Interaction) {
	t.Helper()
	h := newTestHub(t)
	h.store = newMemStore()
	m := newMachine(h, MachineConfig{ID: "mac", Name: "Mac", Kind: "local"})
	h.machines, h.byID["mac"] = []*Machine{m}, m
	m.sessions = []proto.Session{{SessionID: "abcdef12-agent", Title: "修登录", Cwd: "/code/app", Status: proto.StatusWorking, UpdatedAt: time.Now().UTC()}}
	ix := proto.Interaction{
		ID: "ix_ask1", MachineID: "mac", SessionID: "abcdef12-agent", Kind: proto.IxQuestion,
		Questions: []proto.Question{{
			Question: "发吗？",
			Options:  []proto.QuestionOption{{Label: "发"}, {Label: "不发"}},
		}},
		CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Hour).UTC(),
	}
	done := make(chan proto.Resolution, 1)
	h.interactions[ix.ID] = &pending{ix: ix, done: done}
	if err := h.store.InsertInteraction(context.Background(), ix); err != nil {
		t.Fatal(err)
	}
	return h, m, ix
}

func answerArgs(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A question another agent asks is answered for the user (interaction_answer, the assistant's, through
// interactions.get and interactions.answer), and the history says the assistant answered it. What may be answered and
// how is the assistant's to check (assistant.TestInteractionAnswer…).
func TestInteractionAnswerAnswersAQuestion(t *testing.T) {
	h, m, ix := interactionAnswerHub(t)
	p := h.interactions[ix.ID]
	connectAssistant(t, h)
	out, err := answerTool(h, context.Background(), m, "conv-assistant", "interaction_answer", answerArgs(t, map[string]any{
		"machine": "mac", "session_id": "abcdef12", "interaction_id": ix.ID,
		"answers": map[string]string{"发吗？": "发"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "发吗？ → 发") {
		t.Fatalf("result: %s", out)
	}
	select {
	case r := <-p.done:
		if r.Answers["发吗？"] != "发" || r.AnsweredBy != proto.AnsweredByAssistant {
			t.Fatalf("resolution: %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the hook got no answer")
	}
	status, res, err := h.store.InteractionOutcome(context.Background(), ix.ID)
	if err != nil || status != "resolved" || res == nil || res.AnsweredBy != proto.AnsweredByAssistant {
		t.Fatalf("history: %q %+v %v", status, res, err)
	}
}

// agents_list carries what answering needs: the interaction's id and each question's options.
func TestAgentsListShowsInteractionAndOptions(t *testing.T) {
	h, m, ix := interactionAnswerHub(t)
	connectAssistant(t, h) // agents_list is the assistant's
	out := textOf(h.callAssistantTool(context.Background(), m, "conv-assistant", "agents_list", json.RawMessage(`{"state":"needs_you"}`)))
	for _, want := range []string{ix.ID, "发吗？", "发", "不发"} {
		if !strings.Contains(out, want) {
			t.Fatalf("agents_list has no %q:\n%s", want, out)
		}
	}
}

// The broadcast says the assistant answered, so the app can mark it; an answer from the app carries no mark.
func TestInteractionResolvedSaysWhoAnswered(t *testing.T) {
	for _, tc := range []struct {
		name string
		by   string
	}{{"assistant", proto.AnsweredByAssistant}, {"app", ""}} {
		t.Run(tc.name, func(t *testing.T) {
			h, m, ix := interactionAnswerHub(t)
			connectAssistant(t, h)
			app := &client{out: make(chan []byte, 64)}
			h.clients[app] = struct{}{}
			if tc.by == "" {
				if err := h.resolve(ix.ID, proto.Resolution{Answers: map[string]string{"发吗？": "发"}}); err != nil {
					t.Fatal(err)
				}
			} else if _, err := answerTool(h, context.Background(), m, "conv-assistant", "interaction_answer", answerArgs(t, map[string]any{
				"machine": "mac", "session_id": "abcdef12", "interaction_id": ix.ID,
				"answers": map[string]string{"发吗？": "发"},
			})); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				select {
				case raw := <-app.out:
					var ev struct {
						Type       string `json:"type"`
						ID         string `json:"id"`
						AnsweredBy string `json:"answered_by"`
					}
					if json.Unmarshal(raw, &ev) != nil || ev.Type != "interaction_resolved" || ev.ID != ix.ID {
						continue
					}
					if ev.AnsweredBy != tc.by {
						t.Fatalf("answered_by = %q, want %q", ev.AnsweredBy, tc.by)
					}
					return
				case <-time.After(50 * time.Millisecond):
				}
			}
			t.Fatal("no interaction_resolved broadcast")
		})
	}
}

// answerTool runs one of the assistant's tools (connectAssistant) for conversation sessionID on m.
func answerTool(h *Hub, ctx context.Context, m *Machine, sessionID, name string, args json.RawMessage) (string, error) {
	r := h.callAssistantTool(ctx, m, sessionID, name, args)
	if r.IsError {
		return "", errors.New(textOf(r))
	}
	return textOf(r), nil
}

// interactionsCallFor is app.call interactions.<method> as the assistant makes it.
func interactionsCallFor(t *testing.T, h *Hub, method string, params map[string]any) (map[string]any, error) {
	t.Helper()
	raw, _ := json.Marshal(params)
	res, err := assistantLink{h}.AppCall(context.Background(), assistantwire.AppCall{AppID: assistantwire.AppInteractions, Method: method, Params: raw})
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(res, &out); err != nil {
		t.Fatal(err)
	}
	return out, nil
}

// interactions.get is the interaction while it is pending, else how it ended; interactions.answer resolves only a
// pending question of the session named, as the assistant's answer.
func TestInteractionsApp(t *testing.T) {
	h, _, ix := interactionAnswerHub(t)
	out, err := interactionsCallFor(t, h, assistantwire.InteractionsGet, map[string]any{"interaction_id": " " + ix.ID + " "})
	if err != nil || out["ok"] != true || out["interaction"].(map[string]any)["id"] != ix.ID {
		t.Fatalf("get, pending: %v %v", out, err)
	}
	h.store.(*memStore).status["ix_gone"] = "dismissed"
	if out, err := interactionsCallFor(t, h, assistantwire.InteractionsGet, map[string]any{"interaction_id": "ix_gone"}); err != nil ||
		out["ok"] != false || out["status"] != "dismissed" {
		t.Fatalf("get, ended: %v %v", out, err)
	}
	if out, err := interactionsCallFor(t, h, assistantwire.InteractionsGet, map[string]any{"interaction_id": "ix_never"}); err != nil ||
		out["ok"] != false || out["status"] != "" {
		t.Fatalf("get, never was: %v %v", out, err)
	}

	perm := proto.Interaction{ID: "ix_perm", MachineID: "mac", SessionID: "abcdef12-agent", Kind: proto.IxPermission, ToolName: "Bash",
		CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Hour).UTC()}
	h.interactions[perm.ID] = &pending{ix: perm, done: make(chan proto.Resolution, 1)}
	for _, params := range []map[string]any{
		{"machine_id": "mac", "session_id": "abcdef12-agent", "interaction_id": perm.ID, "answers": map[string]string{"x": "y"}},
		{"machine_id": "mac", "session_id": "99999999-agent", "interaction_id": ix.ID, "answers": map[string]string{"发吗？": "发"}},
		{"machine_id": "box", "session_id": "abcdef12-agent", "interaction_id": ix.ID, "answers": map[string]string{"发吗？": "发"}},
	} {
		if _, err := interactionsCallFor(t, h, assistantwire.InteractionsAnswer, params); err == nil {
			t.Fatalf("answered %v", params)
		}
	}
	if _, still := h.interactions[ix.ID]; !still {
		t.Fatal("a refused answer resolved the interaction")
	}

	h.store.(*memStore).status["ix_gone"] = "resolved"
	if out, err := interactionsCallFor(t, h, assistantwire.InteractionsAnswer, map[string]any{"machine_id": "mac", "session_id": "abcdef12-agent",
		"interaction_id": "ix_gone", "answers": map[string]string{"发吗？": "发"}}); err != nil || out["ok"] != false || out["status"] != "resolved" {
		t.Fatalf("answer, no longer pending: %v %v", out, err)
	}
	p := h.interactions[ix.ID]
	if out, err := interactionsCallFor(t, h, assistantwire.InteractionsAnswer, map[string]any{"machine_id": "mac", "session_id": "abcdef12-agent",
		"interaction_id": ix.ID, "answers": map[string]string{"发吗？": "发"}}); err != nil || out["ok"] != true {
		t.Fatalf("answer: %v %v", out, err)
	}
	select {
	case r := <-p.done:
		if r.Answers["发吗？"] != "发" || r.AnsweredBy != proto.AnsweredByAssistant {
			t.Fatalf("resolution: %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the hook got no answer")
	}
}
