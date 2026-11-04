package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/kb"
	"github.com/nwverse/persona/server/internal/proto"
)

// The tools on what the hub keeps (docs/assistant.md「和 hub 的边界」): its knowledge base (kb_*), its interaction
// queue (interaction_answer) and its inbox (inbox_draft). The hub offers them as apps that take and return data
// (app.call kb / interactions / inbox); the arguments, the rules and what the model reads are here.

// ---- kb_* (prompts/mcp/kb) ----

var errKBOff = errors.New("the knowledge base is off (hub.json kb.enabled)")

// kbCall is one of the knowledge base's methods; errKBOff when the hub has no knowledge base.
func (s *Service) kbCall(ctx context.Context, method string, params, out any) error {
	err := s.appCall(ctx, AppKB, method, params, out)
	if err != nil {
		var st struct {
			Ready bool `json:"ready"`
		}
		if s.appCall(ctx, AppKB, KBStatus, map[string]any{}, &st) == nil && !st.Ready {
			return errKBOff
		}
	}
	return err
}

func (s *Service) toolKB(ctx context.Context, name string, args json.RawMessage) (string, error) {
	switch name {
	case "kb_search":
		a := struct {
			Query    string `json:"query"`
			Messages *bool  `json:"messages"`
			Limit    int    `json:"limit"`
		}{}
		_ = json.Unmarshal(args, &a)
		if a.Limit <= 0 {
			a.Limit = 40
		}
		var res struct {
			Dir  string   `json:"dir"`
			Hits []kb.Hit `json:"hits"`
		}
		if err := s.kbCall(ctx, assistantwire.KBSearch, map[string]any{"query": a.Query, "messages": a.Messages == nil || *a.Messages,
			"limit": min(a.Limit, 200)}, &res); err != nil {
			return "", err
		}
		if len(res.Hits) == 0 {
			return "No match in " + res.Dir + ".", nil
		}
		var b strings.Builder
		for _, hit := range res.Hits {
			fmt.Fprintf(&b, "%s: %s\n", hit.Path, hit.Line)
		}
		return b.String(), nil
	case "kb_list":
		var a struct {
			Kind, Tag string
			Limit     int
		}
		_ = json.Unmarshal(args, &a)
		if a.Limit <= 0 {
			a.Limit = 30
		}
		var res struct {
			Items []kb.Entry `json:"items"`
		}
		if err := s.kbCall(ctx, assistantwire.KBList, map[string]any{"kind": a.Kind, "tag": a.Tag, "limit": a.Limit}, &res); err != nil {
			return "", err
		}
		if len(res.Items) == 0 {
			return "Nothing yet.", nil
		}
		var b strings.Builder
		for _, x := range res.Items {
			fmt.Fprintf(&b, "- %s · %s [%s 相关%d] %s %s%s\n", x.ID, x.Name, orDash(x.Importance), x.Relevance,
				strings.Join(x.Tags, ","), x.Extra, prefix(" — ", x.OneLine))
		}
		return b.String(), nil
	case "kb_get":
		var a struct{ Kind, ID string }
		_ = json.Unmarshal(args, &a)
		var res struct {
			Detail kb.Detail `json:"detail"`
		}
		if err := s.kbCall(ctx, assistantwire.KBGet, map[string]any{"kind": a.Kind, "id": a.ID, "recent": 12000}, &res); err != nil {
			if errors.Is(err, errKBOff) {
				return "", err
			}
			return "", fmt.Errorf("%s/%s: %w", a.Kind, a.ID, err)
		}
		d := res.Detail
		// meta.json as the file has it (the bus carries it compact; one that is not JSON comes as a string of its text).
		meta := d.Meta
		var indented bytes.Buffer
		var text string
		if json.Unmarshal(d.Meta, &text) == nil {
			meta = []byte(text)
		} else if json.Indent(&indented, d.Meta, "", "  ") == nil {
			meta = append(indented.Bytes(), '\n')
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Path: %s\n\nmeta.json:\n%s\n", d.Path, meta)
		if d.Summary != "" {
			fmt.Fprintf(&b, "\nsummary.md:\n%s\n", d.Summary)
		}
		if len(d.Log) > 0 {
			fmt.Fprintf(&b, "\nlog.md (latest):\n%s\n", strings.Join(d.Log, "\n"))
		}
		if len(d.Moments) > 0 {
			loc := s.location(ctx)
			b.WriteString("\n公司圈 posts about it (full text: moments/posts/<id>.md):\n")
			for _, m := range d.Moments {
				fmt.Fprintf(&b, "- %s %s · %d comments · %s — %s\n", m.ID, m.At.In(loc).Format("2006-01-02"), m.Comments, m.Note, m.Text)
			}
		}
		if d.Recent != "" {
			fmt.Fprintf(&b, "\nMessages, last 7 days:\n%s\n", d.Recent)
		}
		return b.String(), nil
	case "kb_update":
		var a struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
			kb.Update
		}
		if err := json.Unmarshal(args, &a); err != nil {
			return "", err
		}
		var res struct {
			ID string `json:"id"`
		}
		if err := s.kbCall(ctx, assistantwire.KBUpdate, map[string]any{"kind": a.Kind, "id": a.ID, "update": a.Update}, &res); err != nil {
			return "", err
		}
		return "Saved " + a.Kind + "/" + res.ID + ".", nil
	}
	return "", fmt.Errorf("unknown tool %s", name)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func prefix(p, s string) string {
	if s == "" {
		return ""
	}
	return p + s
}

// ---- interaction_answer (prompts/mcp/agents) ----

// toolInteractionAnswer answers another agent's question for the user (docs/assistant.md). Only a question may be
// answered here: a permission, a plan and a terminal dialog are the user's to decide, and an assistant conversation
// (this one among them) is never answered by the assistant.
func (s *Service) toolInteractionAnswer(ctx context.Context, self string, args json.RawMessage) (string, error) {
	var a struct {
		Machine       string          `json:"machine"`
		SessionID     string          `json:"session_id"`
		InteractionID string          `json:"interaction_id"`
		Answers       json.RawMessage `json:"answers"`
	}
	_ = json.Unmarshal(args, &a)
	am, c, err := s.findAgent(ctx, a.Machine, a.SessionID)
	if err != nil {
		return "", err
	}
	if c.Assistant || c.SessionID == self {
		return "", errors.New("that interaction belongs to an assistant conversation; only the user answers it")
	}
	id := strings.TrimSpace(a.InteractionID)
	var got struct {
		Interaction proto.Interaction `json:"interaction"`
		OK          bool              `json:"ok"`
		Status      string            `json:"status"`
	}
	if err := s.appCall(ctx, AppInteractions, assistantwire.InteractionsGet, map[string]any{"interaction_id": id}, &got); err != nil {
		return "", err
	}
	if !got.OK {
		return "", interactionEnded(id, got.Status)
	}
	ix := got.Interaction
	if ix.Kind != proto.IxQuestion {
		return "", fmt.Errorf("interaction %s is a %s (approval), only the user can answer it in the app", ix.ID, ix.Kind)
	}
	if ix.MachineID != am.ID || ix.SessionID != c.SessionID {
		return "", fmt.Errorf("interaction %s belongs to another agent: machine=%s session_id=%s", ix.ID, ix.MachineID, ix.SessionID)
	}
	if time.Now().After(ix.ExpiresAt) {
		return "", fmt.Errorf("interaction %s expired", ix.ID)
	}
	answers, err := interactionAnswers(ix, a.Answers)
	if err != nil {
		return "", err
	}
	var res struct {
		OK     bool   `json:"ok"`
		Status string `json:"status"`
	}
	if err := s.appCall(ctx, AppInteractions, assistantwire.InteractionsAnswer, map[string]any{"machine_id": ix.MachineID,
		"session_id": ix.SessionID, "interaction_id": ix.ID, "answers": answers}, &res); err != nil {
		return "", err
	}
	if !res.OK {
		return "", interactionEnded(ix.ID, res.Status)
	}
	var b strings.Builder
	b.WriteString("Answered for the user:\n")
	for _, q := range ix.Questions {
		fmt.Fprintf(&b, "- %s → %s\n", q.Question, answers[q.Question])
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// interactionEnded says how an interaction that is no longer pending ended (status, from the hub's history), or that
// there is none.
func interactionEnded(id, status string) error {
	switch status {
	case "resolved":
		return fmt.Errorf("interaction %s was already answered", id)
	case "dismissed":
		return fmt.Errorf("interaction %s was dismissed", id)
	case "expired":
		return fmt.Errorf("interaction %s expired", id)
	case "cancelled":
		return fmt.Errorf("interaction %s was cancelled", id)
	}
	return fmt.Errorf("no pending interaction %s (use agents_list)", id)
}

// interactionAnswers checks a question's answers against the questions themselves: every question answered, by the
// wording it was asked, with options it offers. A multi-select takes several labels and becomes the app's own form,
// "A, B". Free text is refused: the user answers that in the app, or through agent_send.
func interactionAnswers(ix proto.Interaction, args json.RawMessage) (map[string]string, error) {
	var raw map[string]json.RawMessage
	if len(args) == 0 || json.Unmarshal(args, &raw) != nil {
		return nil, fmt.Errorf("answers is required: an object of question → option label")
	}
	asked := map[string]bool{}
	for _, q := range ix.Questions {
		asked[q.Question] = true
	}
	for k := range raw {
		if !asked[k] {
			return nil, fmt.Errorf("%q is not a question this interaction asks; it asks: %s", k, askedQuestions(ix))
		}
	}
	answers := make(map[string]string, len(ix.Questions))
	for _, q := range ix.Questions {
		v, ok := raw[q.Question]
		if !ok {
			return nil, fmt.Errorf("every question needs an answer, %q has none (options: %s)", q.Question, optionLabels(q))
		}
		var one string
		var many []string
		if json.Unmarshal(v, &many) != nil {
			if json.Unmarshal(v, &one) != nil {
				return nil, fmt.Errorf("%q takes an option label or an array of them (options: %s)", q.Question, optionLabels(q))
			}
			if !q.MultiSelect {
				answers[q.Question] = one
			} else {
				many = []string{one}
			}
		}
		if many != nil {
			if !q.MultiSelect && len(many) > 1 {
				return nil, fmt.Errorf("%q takes one option; pick one of: %s", q.Question, optionLabels(q))
			}
			answers[q.Question] = strings.Join(many, ", ")
		}
		if err := checkOptions(q, answers[q.Question]); err != nil {
			return nil, err
		}
	}
	return answers, nil
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

func askedQuestions(ix proto.Interaction) string {
	qs := make([]string, len(ix.Questions))
	for i, q := range ix.Questions {
		qs[i] = q.Question
	}
	return strings.Join(qs, "; ")
}

// ---- inbox_draft (prompts/mcp/inbox) ----

// placeholderRE: a blank a draft leaves for the user to fill in — 【…】, {{…}}.
var placeholderRE = regexp.MustCompile(`【[^】\n]*】|\{\{[^}\n]*\}\}`)

// draftPlaceholder is the first blank in a draft left to fill in; empty when it has none.
func draftPlaceholder(text string) string {
	return placeholderRE.FindString(text)
}

// docChange moves one row's status of a document item (the inbox's DocChange): the AI 分身's proposal.
type docChange struct {
	Ref   string `json:"ref"`
	Field string `json:"field"`
	Value string `json:"value"`
}

// toolInboxDraft gives the inbox the draft of the AI 分身 working in conversation self (docs/inbox.md): it waits on
// the item for the user to send.
func (s *Service) toolInboxDraft(ctx context.Context, self string, args json.RawMessage) (string, error) {
	var a struct {
		Text, Note string
		Changes    []docChange
	}
	if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.Text) == "" {
		return "", errors.New("text is required")
	}
	// The user sends it with one tap: a blank left to fill in (【预计几点前】) would go out as it is.
	if p := draftPlaceholder(a.Text); p != "" {
		return "", fmt.Errorf("the draft leaves %s to fill in, and it goes out as it is: leave out what you could not find (say so in note) and submit again", p)
	}
	var res struct {
		ItemID  string     `json:"item_id"`
		Refused *docChange `json:"refused"`
	}
	if err := s.appCall(ctx, AppInbox, assistantwire.InboxDraft, map[string]any{"session_id": self, "text": strings.TrimSpace(a.Text),
		"note": clip(strings.TrimSpace(a.Note), 600), "changes": a.Changes}, &res); err != nil {
		return "", err
	}
	if res.ItemID == "" {
		return "", errors.New("this conversation was not started for an inbox item")
	}
	// Only moves the item offers (a row it lists, one of the column's values).
	if res.Refused != nil {
		return "", fmt.Errorf("change %+v is not one the item lists (ref, field and one of its values)", *res.Refused)
	}
	return "The draft is in the user's inbox; they will send it. You're done.", nil
}
