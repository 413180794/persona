package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// interaction_answer, the assistant's tool for answering another agent's question for the user (docs/assistant.md):
// the hub's to run, on its interaction queue (app.tool of the interactions app, assistantLink.appTool).

// toolInteractionAnswer answers another agent's question for the user (interaction_answer, docs/assistant.md). Only a
// question may be answered here: a permission, a plan and a terminal dialog are the user's to decide, and the
// assistant's own conversation is never answered by itself.
func (h *Hub) toolInteractionAnswer(ctx context.Context, m *Machine, sessionID, machine, agentID, interactionID string, answers json.RawMessage) (string, error) {
	am, s, err := h.findAgent(machine, agentID)
	if err != nil {
		return "", err
	}
	if s.Assistant || s.SessionID == sessionID {
		return "", fmt.Errorf("that interaction belongs to an assistant conversation; only the user answers it")
	}
	h.mu.Lock()
	p := h.interactions[strings.TrimSpace(interactionID)]
	var ix proto.Interaction
	if p != nil {
		ix = p.ix
	}
	h.mu.Unlock()
	if p == nil {
		return "", h.answeredInteractionError(ctx, interactionID)
	}
	if ix.Kind != proto.IxQuestion {
		return "", fmt.Errorf("interaction %s is a %s (approval), only the user can answer it in the app", ix.ID, ix.Kind)
	}
	if ix.MachineID != am.cfg.ID || ix.SessionID != s.SessionID {
		return "", fmt.Errorf("interaction %s belongs to another agent: machine=%s session_id=%s", ix.ID, ix.MachineID, ix.SessionID)
	}
	if time.Now().After(ix.ExpiresAt) {
		return "", fmt.Errorf("interaction %s expired", ix.ID)
	}
	res, err := interactionAnswers(ix, answers)
	if err != nil {
		return "", err
	}
	res.AnsweredBy = proto.AnsweredByAssistant
	if err := h.resolve(ix.ID, res); err != nil {
		if errors.Is(err, errNotPending) {
			return "", h.answeredInteractionError(ctx, ix.ID)
		}
		return "", err
	}
	var b strings.Builder
	b.WriteString("Answered for the user:\n")
	for _, q := range ix.Questions {
		fmt.Fprintf(&b, "- %s → %s\n", q.Question, res.Answers[q.Question])
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// answeredInteractionError says how an interaction that is no longer pending ended: from the history, or that there
// is none.
func (h *Hub) answeredInteractionError(ctx context.Context, id string) error {
	ix := strings.TrimSpace(id)
	if h.store != nil {
		if status, _, err := h.store.InteractionOutcome(context.Background(), ix); err == nil {
			switch status {
			case "resolved":
				return fmt.Errorf("interaction %s was already answered", ix)
			case "dismissed":
				return fmt.Errorf("interaction %s was dismissed", ix)
			case "expired":
				return fmt.Errorf("interaction %s expired", ix)
			case "cancelled":
				return fmt.Errorf("interaction %s was cancelled", ix)
			}
		}
	}
	return fmt.Errorf("no pending interaction %s (use agents_list)", ix)
}

// interactionAnswers checks a question's answers against the questions themselves and builds the resolution: every
// question answered, by the wording it was asked, with options it offers. A multi-select takes several labels and
// becomes the app's own form, "A, B". Free text is refused: the user answers that in the app, or through agent_send.
func interactionAnswers(ix proto.Interaction, args json.RawMessage) (proto.Resolution, error) {
	var raw map[string]json.RawMessage
	if len(args) == 0 || json.Unmarshal(args, &raw) != nil {
		return proto.Resolution{}, fmt.Errorf("answers is required: an object of question → option label")
	}
	asked := map[string]bool{}
	for _, q := range ix.Questions {
		asked[q.Question] = true
	}
	for k := range raw {
		if !asked[k] {
			return proto.Resolution{}, fmt.Errorf("%q is not a question this interaction asks; it asks: %s", k, askedQuestions(ix))
		}
	}
	answers := make(map[string]string, len(ix.Questions))
	for _, q := range ix.Questions {
		v, ok := raw[q.Question]
		if !ok {
			return proto.Resolution{}, fmt.Errorf("every question needs an answer, %q has none (options: %s)", q.Question, optionLabels(q))
		}
		var one string
		var many []string
		if json.Unmarshal(v, &many) != nil {
			if json.Unmarshal(v, &one) != nil {
				return proto.Resolution{}, fmt.Errorf("%q takes an option label or an array of them (options: %s)", q.Question, optionLabels(q))
			}
			if !q.MultiSelect {
				answers[q.Question] = one
			} else {
				many = []string{one}
			}
		}
		if many != nil {
			if !q.MultiSelect && len(many) > 1 {
				return proto.Resolution{}, fmt.Errorf("%q takes one option; pick one of: %s", q.Question, optionLabels(q))
			}
			answers[q.Question] = strings.Join(many, ", ")
		}
		if err := checkOptions(q, answers[q.Question]); err != nil {
			return proto.Resolution{}, err
		}
	}
	return proto.Resolution{Answers: answers}, nil
}

// checkOptions refuses an answer whose labels are not the question's options, listing them.
func checkOptions(q proto.Question, answer string) error {
	offered := map[string]bool{}
	for _, o := range q.Options {
		offered[o.Label] = true
	}
	picked := 0
	for _, label := range strings.Split(answer, ", ") {
		if !offered[label] {
			return fmt.Errorf("%q is not one of %q's options: %s", label, q.Question, optionLabels(q))
		}
		picked++
	}
	if picked == 0 {
		return fmt.Errorf("%q needs an option: %s", q.Question, optionLabels(q))
	}
	if !q.MultiSelect && picked > 1 {
		return fmt.Errorf("%q takes one option; pick one of: %s", q.Question, optionLabels(q))
	}
	return nil
}

func optionLabels(q proto.Question) string {
	labels := make([]string, len(q.Options))
	for i, o := range q.Options {
		labels[i] = o.Label
	}
	return strings.Join(labels, ", ")
}

// questionSummary is every question of an interaction with its options, so agents_list carries what answering one
// needs without a second call: 发吗？（发 / 不发）.
func questionSummary(ix proto.Interaction) string {
	parts := make([]string, 0, len(ix.Questions))
	for _, q := range ix.Questions {
		part := q.Question
		if len(q.Options) > 0 {
			part += "（" + optionLabels(q) + "）"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " / ")
}

func askedQuestions(ix proto.Interaction) string {
	qs := make([]string, len(ix.Questions))
	for i, q := range ix.Questions {
		qs[i] = q.Question
	}
	return strings.Join(qs, "; ")
}
