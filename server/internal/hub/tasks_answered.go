package hub

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/proto"
)

// answeredBody is the conversation's record of a task's card the user dealt with (feed kind "answered"): a line for
// the collapsed row, and every question with its answer in full for when the row is opened (#1231). Question and
// Answer stay one clipped line each: older apps draw only them, and older records have nothing else.
type answeredBody struct {
	Question string         `json:"question"`
	Answer   string         `json:"answer"`
	Items    []answeredItem `json:"items,omitempty"`
}

// answeredItem is one question of a card and what the user answered, newlines kept. Answer is empty when there is
// nothing to say beyond the summary (a dismissed card's questions say 忽略了 themselves).
type answeredItem struct {
	Question string `json:"question"`
	Answer   string `json:"answer,omitempty"`
}

const (
	answeredLineMax  = 120   // the summary line
	answeredTextMax  = 4000  // one question or answer
	answeredTotalMax = 12000 // all of a record's items together
	answeredItemsMax = 20
)

// answeredSecret is what stands for a value typed into a password field: the hub keeps those nowhere.
const answeredSecret = "（已填写，不保存）"

// taskAnswered: the user dealt with a card of one of the task's agents; the conversation keeps a record of it.
func (h *Hub) taskAnswered(ix proto.Interaction, r proto.Resolution, status string) {
	t, ok := h.taskOf(ix.MachineID, ix.SessionID)
	if !ok || status == "failed" {
		return
	}
	body := answeredBody{Question: interactionQuestion(ix), Answer: resolutionAnswer(ix, r), Items: answeredItems(ix, r)}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := h.appendFeed(ctx, t.Origin, feedAnswered, t.ID, body); err != nil {
		h.logf("task %s: answered: %v", t.ID, err)
	}
}

// interactionQuestion is what a card asked, in a line.
func interactionQuestion(ix proto.Interaction) string {
	switch {
	case len(ix.Questions) > 0:
		return clip(oneLine(ix.Questions[0].Question), answeredLineMax)
	case ix.Kind == proto.IxPlan:
		return "批准计划"
	case ix.Summary != "":
		return clip(oneLine(ix.Summary), answeredLineMax)
	case ix.Kind == proto.IxElicitation && ix.Elicitation != nil && ix.Elicitation.Message != "":
		return clip(oneLine(ix.Elicitation.Message), answeredLineMax)
	case ix.ToolName != "":
		return "使用 " + ix.ToolName
	}
	return "需要你处理"
}

// resolutionAnswer is the user's answer, in a line (never a typed secret: the hub keeps those nowhere).
func resolutionAnswer(ix proto.Interaction, r proto.Resolution) string {
	switch {
	case r.Dismiss:
		return "忽略了"
	case len(r.Answers) > 0:
		var parts []string
		for _, it := range questionAnswers(ix, r) {
			parts = append(parts, it.Answer)
		}
		return clip(oneLine(strings.Join(parts, "；")), answeredLineMax)
	case r.Approve != nil && *r.Approve, r.Allow != nil && *r.Allow:
		if r.Always {
			return "批准了 · 以后同类不再问"
		}
		return "批准了"
	case r.Approve != nil, r.Allow != nil:
		if r.Feedback != "" || r.Message != "" {
			return clip(oneLine("拒绝了："+r.Feedback+r.Message), answeredLineMax)
		}
		return "拒绝了"
	case r.Option != "" || len(r.Options) > 0:
		if a := screenAnswerText(ix, r); a != "选了" {
			return clip(oneLine("选了 "+a), answeredLineMax)
		}
		return "选了"
	case r.Action != "":
		return elicitationAction(r.Action)
	}
	return "回答了"
}

// questionAnswers pairs a question card's questions with the answers, in the card's order; answers keyed by a
// question the card does not have (an older app's) follow, sorted.
func questionAnswers(ix proto.Interaction, r proto.Resolution) []answeredItem {
	var out []answeredItem
	seen := map[string]bool{}
	for _, q := range ix.Questions {
		if a, ok := r.Answers[q.Question]; ok {
			out = append(out, answeredItem{Question: q.Question, Answer: a})
			seen[q.Question] = true
		}
	}
	var rest []string
	for q := range r.Answers {
		if !seen[q] {
			rest = append(rest, q)
		}
	}
	sort.Strings(rest)
	for _, q := range rest {
		out = append(out, answeredItem{Question: q, Answer: r.Answers[q]})
	}
	return out
}

// answeredItems is the record in full: each question with its answer, newlines kept, within the length limits.
func answeredItems(ix proto.Interaction, r proto.Resolution) []answeredItem {
	var items []answeredItem
	switch {
	case len(ix.Questions) > 0 || len(r.Answers) > 0:
		answers := questionAnswers(ix, r)
		got := map[string]string{}
		for _, a := range answers {
			got[a.Question] = a.Answer
		}
		for _, q := range ix.Questions {
			a, ok := got[q.Question]
			switch {
			case r.Dismiss:
				a = "忽略了"
			case !ok:
				a = "没回答"
			}
			items = append(items, answeredItem{Question: q.Question, Answer: a})
		}
		for _, a := range answers[min(len(answers), countAnswered(ix, got)):] {
			items = append(items, a) // answers to questions the card does not list
		}
	case ix.Kind == proto.IxPlan:
		q := "批准计划"
		if p := strings.TrimSpace(ix.Plan); p != "" {
			q = "批准这份计划？\n\n" + p
		}
		items = append(items, answeredItem{Question: q, Answer: decisionAnswer(ix, r)})
	case ix.Kind == proto.IxScreen:
		items = append(items, answeredItem{Question: screenQuestion(ix), Answer: screenAnswerText(ix, r)})
	case ix.Kind == proto.IxElicitation:
		items = append(items, answeredItem{Question: elicitationQuestion(ix), Answer: elicitationAnswer(ix, r)})
	default: // a permission, or anything else a card asks
		items = append(items, answeredItem{Question: permissionQuestion(ix), Answer: decisionAnswer(ix, r)})
	}
	return fitAnswered(items)
}

// countAnswered is how many of the card's own questions have an answer (they come first in questionAnswers).
func countAnswered(ix proto.Interaction, got map[string]string) int {
	n := 0
	for _, q := range ix.Questions {
		if _, ok := got[q.Question]; ok {
			n++
		}
	}
	return n
}

// decisionAnswer is an approve / deny in full: the reason given, the plan's mode, the card's fields.
func decisionAnswer(ix proto.Interaction, r proto.Resolution) string {
	if r.Dismiss {
		return "忽略了"
	}
	var lines []string
	approved := r.Approve != nil && *r.Approve || r.Allow != nil && *r.Allow
	denied := !approved && (r.Approve != nil || r.Allow != nil)
	switch {
	case approved:
		head := "批准了"
		if label := planModes[r.Mode]; ix.Kind == proto.IxPlan && label != "" {
			head += " · " + label
		}
		if r.Always {
			head += " · 以后同类不再问"
		}
		lines = append(lines, head)
	case denied:
		lines = append(lines, "拒绝了")
	default:
		lines = append(lines, resolutionAnswer(ix, r))
	}
	if why := strings.TrimSpace(strings.TrimSpace(r.Feedback) + "\n" + strings.TrimSpace(r.Message)); why != "" {
		if denied {
			lines[0] = "拒绝了："
			lines = append(lines, why)
		} else {
			lines = append(lines, "附言："+why)
		}
	}
	if len(r.Values) > 0 {
		lines = append(lines, fieldValues(ix, r)...)
	}
	return strings.Join(lines, "\n")
}

// planModes names how an approved plan goes on (Claude Code's permission modes).
var planModes = map[string]string{
	"acceptEdits":       "自动接受修改",
	"default":           "每步都问",
	"bypassPermissions": "全部放行",
	"plan":              "继续出方案",
}

// fieldValues are a card's filled-in fields, label：value, a password field only said to be filled in.
func fieldValues(ix proto.Interaction, r proto.Resolution) []string {
	var lines []string
	known := map[string]bool{}
	for _, f := range ix.Fields {
		known[f.Key] = true
		v, ok := r.Values[f.Key]
		if !ok || v == "" {
			continue
		}
		label := f.Label
		if label == "" {
			label = f.Key
		}
		if f.Secret || secretName.MatchString(f.Key) {
			v = answeredSecret
		}
		lines = append(lines, label+"："+v)
	}
	var rest []string
	for k := range r.Values {
		if !known[k] && r.Values[k] != "" {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range rest { // a field the card does not describe: kept only by name
		lines = append(lines, k+"："+answeredSecret)
	}
	return lines
}

// secretName is a field or property name that holds a credential whatever the card says about it.
var secretName = regexp.MustCompile(`(?i)pass|secret|token|credential|api[_-]?key|private|otp|pin\b`)

// permissionQuestion is what a permission card asked in full: the tool, what it would do, why the agent asks.
func permissionQuestion(ix proto.Interaction) string {
	var lines []string
	if ix.ToolName != "" {
		lines = append(lines, "使用 "+ix.ToolName)
	}
	if s := strings.TrimSpace(ix.Summary); s != "" {
		lines = append(lines, s)
	}
	if s := strings.TrimSpace(ix.Reason); s != "" {
		lines = append(lines, "原因："+s)
	}
	if len(lines) == 0 {
		return interactionQuestion(ix)
	}
	return strings.Join(lines, "\n")
}

// screenQuestion is a terminal dialog's question with the lines that belong to it (the command asked about).
func screenQuestion(ix proto.Interaction) string {
	if d := ix.Dialog; d != nil && strings.TrimSpace(d.Question) != "" {
		var lines []string
		for _, s := range []string{d.Warning, d.Detail, d.Question} {
			if s = strings.TrimSpace(s); s != "" {
				lines = append(lines, s)
			}
		}
		return strings.Join(lines, "\n")
	}
	if s := strings.TrimSpace(ix.Summary); s != "" {
		return s
	}
	return interactionQuestion(ix)
}

// screenAnswerText is the options picked on a terminal dialog, with the text typed into one — never what was typed at a
// password prompt, nor keys sent straight to the terminal.
func screenAnswerText(ix proto.Interaction, r proto.Resolution) string {
	switch {
	case r.Dismiss:
		return "忽略了"
	case r.Option != "" || len(r.Options) > 0:
		lines := []string{}
		if a := pickedOptions(ix, r); a != "" {
			lines = append(lines, a)
		}
		if t := strings.TrimSpace(r.Text); t != "" && !ix.Secret {
			lines = append(lines, t)
		}
		if len(lines) == 0 {
			return "选了"
		}
		return strings.Join(lines, "\n")
	case ix.Secret && (r.Text != "" || len(r.Keys) > 0):
		return "输入了密码（不保存）"
	}
	return resolutionAnswer(ix, r)
}

// pickedOptions names the options a terminal dialog's answer picked, by their labels.
func pickedOptions(ix proto.Interaction, r proto.Resolution) string {
	keys := r.Options
	if r.Option != "" {
		keys = []string{r.Option}
	}
	var labels []string
	for _, k := range keys {
		label := k
		if ix.Dialog != nil {
			for _, o := range ix.Dialog.Options {
				if o.Key == k && o.Label != "" {
					label = o.Label
					if o.Kind == "type_something" {
						label = "" // its text follows on its own line
					}
				}
			}
		}
		if label != "" {
			labels = append(labels, label)
		}
	}
	return strings.Join(labels, "、")
}

func elicitationQuestion(ix proto.Interaction) string {
	if e := ix.Elicitation; e != nil {
		var lines []string
		for _, s := range []string{e.Message, e.URL} {
			if s = strings.TrimSpace(s); s != "" {
				lines = append(lines, s)
			}
		}
		if len(lines) > 0 {
			return strings.Join(lines, "\n")
		}
	}
	return interactionQuestion(ix)
}

func elicitationAction(action string) string {
	switch action {
	case "accept":
		return "提交了"
	case "decline":
		return "拒绝了"
	case "cancel":
		return "取消了"
	}
	return "回答了"
}

// elicitationAnswer is what was submitted to an MCP server's form: its fields by their titles, a password-like one
// only said to be filled in.
func elicitationAnswer(ix proto.Interaction, r proto.Resolution) string {
	if r.Dismiss {
		return "忽略了"
	}
	lines := []string{elicitationAction(r.Action)}
	if r.Action != "accept" || len(r.Content) == 0 {
		return lines[0]
	}
	var schema struct {
		Properties map[string]struct {
			Title     string `json:"title"`
			Format    string `json:"format"`
			WriteOnly bool   `json:"writeOnly"`
		} `json:"properties"`
	}
	if ix.Elicitation != nil {
		_ = json.Unmarshal(ix.Elicitation.RequestedSchema, &schema)
	}
	keys := make([]string, 0, len(r.Content))
	for k := range r.Content {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		p := schema.Properties[k]
		label := k
		if p.Title != "" {
			label = p.Title
		}
		var v string
		switch x := r.Content[k].(type) {
		case string:
			v = x
		case nil:
			continue
		default:
			b, _ := json.Marshal(x)
			v = string(b)
		}
		if p.Format == "password" || p.WriteOnly || secretName.MatchString(k) {
			v = answeredSecret
		}
		lines = append(lines, label+"："+v)
	}
	return strings.Join(lines, "\n")
}

// fitAnswered keeps a record within its limits: each text clipped, the items stopping where the total runs out.
func fitAnswered(items []answeredItem) []answeredItem {
	left := answeredTotalMax
	var out []answeredItem
	for _, it := range items {
		if left <= 0 || len(out) == answeredItemsMax {
			break
		}
		q := clip(strings.TrimSpace(it.Question), min(answeredTextMax, left))
		left -= utf8.RuneCountInString(q)
		a := clip(strings.TrimSpace(it.Answer), max(0, min(answeredTextMax, left)))
		if a == "…" {
			a = ""
		}
		left -= utf8.RuneCountInString(a)
		out = append(out, answeredItem{Question: q, Answer: a})
	}
	return out
}
