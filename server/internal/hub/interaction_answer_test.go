package hub

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

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

// A question another agent asks is answered for the user, and the history says the assistant answered it.
func TestInteractionAnswerAnswersAQuestion(t *testing.T) {
	h, m, ix := interactionAnswerHub(t)
	p := h.interactions[ix.ID]
	out, err := h.runAssistantTool(context.Background(), m, "conv-assistant", "interaction_answer", answerArgs(t, map[string]any{
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

// Only questions may be answered for the user; an approval is theirs alone.
func TestInteractionAnswerRefusesApprovals(t *testing.T) {
	for _, tc := range []struct {
		kind string
		ix   proto.Interaction
	}{{
		kind: "permission",
		ix:   proto.Interaction{Kind: proto.IxPermission, ToolName: "Bash", Summary: "rm -rf"},
	}, {
		kind: "plan",
		ix:   proto.Interaction{Kind: proto.IxPlan, Plan: "1. do it"},
	}, {
		kind: "screen",
		ix:   proto.Interaction{Kind: proto.IxScreen},
	}} {
		h, m, _ := interactionAnswerHub(t)
		ix := tc.ix
		ix.ID, ix.MachineID, ix.SessionID = "ix_"+tc.kind, "mac", "abcdef12-agent"
		ix.CreatedAt, ix.ExpiresAt = time.Now().UTC(), time.Now().Add(time.Hour).UTC()
		h.interactions[ix.ID] = &pending{ix: ix, done: make(chan proto.Resolution, 1)}
		_, err := h.runAssistantTool(context.Background(), m, "conv-assistant", "interaction_answer", answerArgs(t, map[string]any{
			"machine": "mac", "session_id": "abcdef12", "interaction_id": ix.ID,
			"answers": map[string]string{"any": "yes"},
		}))
		if err == nil || !strings.Contains(err.Error(), tc.kind) || !strings.Contains(err.Error(), "only the user") {
			t.Fatalf("%s: %v", tc.kind, err)
		}
	}
}

// The assistant never answers its own conversation's question.
func TestInteractionAnswerRefusesAssistantConversation(t *testing.T) {
	h, m, ix := interactionAnswerHub(t)
	if _, err := h.runAssistantTool(context.Background(), m, ix.SessionID, "interaction_answer", answerArgs(t, map[string]any{
		"machine": "mac", "session_id": "abcdef12", "interaction_id": ix.ID,
		"answers": map[string]string{"发吗？": "发"},
	})); err == nil || !strings.Contains(err.Error(), "assistant conversation") {
		t.Fatalf("own conversation: %v", err)
	}
	own := proto.Interaction{
		ID: "ix_ask_user", MachineID: "mac", SessionID: "conv-assistant", Kind: proto.IxQuestion,
		Questions: []proto.Question{{Question: "发吗？", Options: []proto.QuestionOption{{Label: "发"}}}},
		CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Hour).UTC(),
	}
	h.interactions[own.ID] = &pending{ix: own, done: make(chan proto.Resolution, 1)}
	m.sessions = append(m.sessions, proto.Session{SessionID: "conv-assistant", Title: "Persona", Assistant: true, UpdatedAt: time.Now().UTC()})
	if _, err := h.runAssistantTool(context.Background(), m, "conv-assistant", "interaction_answer", answerArgs(t, map[string]any{
		"machine": "mac", "session_id": "conv-assist", "interaction_id": own.ID,
		"answers": map[string]string{"发吗？": "发"},
	})); err == nil || !strings.Contains(err.Error(), "assistant conversation") {
		t.Fatalf("ask_user: %v", err)
	}
}

// The answers are checked against the question before anything is resolved.
func TestInteractionAnswerValidatesAnswers(t *testing.T) {
	for _, tc := range []struct {
		name    string
		answers map[string]any
		want    string
	}{
		{"a question left unanswered", map[string]any{}, "every question"},
		{"a key that is not the question", map[string]any{"another question": "发"}, "not a question"},
		{"an option that is not offered", map[string]any{"发吗？": "也许"}, "发, 不发"},
		{"an answer with no option at all", map[string]any{"发吗？": ""}, "发, 不发"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, m, ix := interactionAnswerHub(t)
			_, err := h.runAssistantTool(context.Background(), m, "conv-assistant", "interaction_answer", answerArgs(t, map[string]any{
				"machine": "mac", "session_id": "abcdef12", "interaction_id": ix.ID, "answers": tc.answers,
			}))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%v", err)
			}
			if _, still := h.interactions[ix.ID]; !still {
				t.Fatal("a refused answer resolved the interaction")
			}
		})
	}
}

// A single-choice question takes one label; a multi-select takes the labels the app shows, "A, B".
func TestInteractionAnswerSingleAndMultiSelect(t *testing.T) {
	h, m, ix := interactionAnswerHub(t)
	multi := proto.Interaction{
		ID: "ix_multi", MachineID: "mac", SessionID: "abcdef12-agent", Kind: proto.IxQuestion,
		Questions: []proto.Question{{
			Question: "哪几个？", MultiSelect: true,
			Options: []proto.QuestionOption{{Label: "A"}, {Label: "B"}, {Label: "C"}},
		}},
		CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Hour).UTC(),
	}
	p := &pending{ix: multi, done: make(chan proto.Resolution, 1)}
	h.interactions[multi.ID] = p
	if _, err := h.runAssistantTool(context.Background(), m, "conv-assistant", "interaction_answer", answerArgs(t, map[string]any{
		"machine": "mac", "session_id": "abcdef12", "interaction_id": multi.ID,
		"answers": map[string]any{"哪几个？": []string{"A", "C"}},
	})); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-p.done:
		if r.Answers["哪几个？"] != "A, C" {
			t.Fatalf("multi-select answer: %q", r.Answers["哪几个？"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the hook got no answer")
	}

	single := h.interactions[ix.ID]
	if _, err := h.runAssistantTool(context.Background(), m, "conv-assistant", "interaction_answer", answerArgs(t, map[string]any{
		"machine": "mac", "session_id": "abcdef12", "interaction_id": ix.ID,
		"answers": map[string]any{"发吗？": []string{"发", "不发"}},
	})); err == nil || !strings.Contains(err.Error(), "one option") {
		t.Fatalf("two labels on a single-choice question: %v", err)
	}
	if _, still := h.interactions[single.ix.ID]; !still {
		t.Fatal("a refused answer resolved the interaction")
	}
}

// An expired question, one answered already and one that never existed each say which.
func TestInteractionAnswerExpiredAnsweredAndUnknown(t *testing.T) {
	h, m, _ := interactionAnswerHub(t)
	expired := proto.Interaction{
		ID: "ix_expired", MachineID: "mac", SessionID: "abcdef12-agent", Kind: proto.IxQuestion,
		Questions: []proto.Question{{Question: "发吗？", Options: []proto.QuestionOption{{Label: "发"}}}},
		CreatedAt: time.Now().Add(-2 * time.Hour).UTC(), ExpiresAt: time.Now().Add(-time.Hour).UTC(),
	}
	h.interactions[expired.ID] = &pending{ix: expired, done: make(chan proto.Resolution, 1)}
	if _, err := h.runAssistantTool(context.Background(), m, "conv-assistant", "interaction_answer", answerArgs(t, map[string]any{
		"machine": "mac", "session_id": "abcdef12", "interaction_id": expired.ID,
		"answers": map[string]string{"发吗？": "发"},
	})); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired: %v", err)
	}

	// Answered elsewhere: only the history knows it, and it says how it ended.
	h.store.(*memStore).status["ix_gone"] = "resolved"
	h.store.(*memStore).res["ix_gone"] = &proto.Resolution{Answers: map[string]string{"发吗？": "发"}}
	if _, err := h.runAssistantTool(context.Background(), m, "conv-assistant", "interaction_answer", answerArgs(t, map[string]any{
		"machine": "mac", "session_id": "abcdef12", "interaction_id": "ix_gone",
		"answers": map[string]string{"发吗？": "发"},
	})); err == nil || !strings.Contains(err.Error(), "already answered") {
		t.Fatalf("already answered: %v", err)
	}

	if _, err := h.runAssistantTool(context.Background(), m, "conv-assistant", "interaction_answer", answerArgs(t, map[string]any{
		"machine": "mac", "session_id": "abcdef12", "interaction_id": "ix_never",
		"answers": map[string]string{"发吗？": "发"},
	})); err == nil || !strings.Contains(err.Error(), "no pending interaction") {
		t.Fatalf("never existed: %v", err)
	}
}

// The machine and session named must be the interaction's own.
func TestInteractionAnswerKeepsToItsTarget(t *testing.T) {
	h, m, ix := interactionAnswerHub(t)
	if _, err := h.runAssistantTool(context.Background(), m, "conv-assistant", "interaction_answer", answerArgs(t, map[string]any{
		"machine": "another", "session_id": "abcdef12", "interaction_id": ix.ID,
		"answers": map[string]string{"发吗？": "发"},
	})); err == nil {
		t.Fatal("a different machine was accepted")
	}
	other := newMachine(h, MachineConfig{ID: "box", Name: "Box", Kind: "local"})
	h.machines = append(h.machines, other)
	h.byID["box"] = other
	other.sessions = []proto.Session{{SessionID: "99999999-agent", Title: "别的", Cwd: "/code/other", UpdatedAt: time.Now().UTC()}}
	if _, err := h.runAssistantTool(context.Background(), m, "conv-assistant", "interaction_answer", answerArgs(t, map[string]any{
		"machine": "mac", "session_id": "99999999", "interaction_id": ix.ID,
		"answers": map[string]string{"发吗？": "发"},
	})); err == nil {
		t.Fatal("a session that is not the interaction's was accepted")
	}
	if _, still := h.interactions[ix.ID]; !still {
		t.Fatal("a refused answer resolved the interaction")
	}
}

// agents_list carries what answering needs: the interaction's id and each question's options.
func TestAgentsListShowsInteractionAndOptions(t *testing.T) {
	h, _, ix := interactionAnswerHub(t)
	out := h.describeAgents("needs_you", "", "")
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
			app := &client{out: make(chan []byte, 64)}
			h.clients[app] = struct{}{}
			if tc.by == "" {
				if err := h.resolve(ix.ID, proto.Resolution{Answers: map[string]string{"发吗？": "发"}}); err != nil {
					t.Fatal(err)
				}
			} else if _, err := h.runAssistantTool(context.Background(), m, "conv-assistant", "interaction_answer", answerArgs(t, map[string]any{
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
