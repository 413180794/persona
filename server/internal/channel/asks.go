package channel

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/nwverse/persona/server/internal/proto"
)

// A channel without buttons (asks: text) gets what an agent asks — a question, a permission, a plan, a terminal
// dialog — as text, and the owner's next message answers it: a number, 允许 / 拒绝, 批准, or words. The latest one
// asked is the one a reply answers; one answered somewhere else (the app) is let go, and the reply is then an
// ordinary message (docs/channels.md「提问和批准」).

// Ask is something asked in a channel as text, waiting for the owner's reply.
type Ask struct {
	ID        string           `json:"id"`
	Channel   string           `json:"channel"`
	MachineID string           `json:"machine_id"`
	SessionID string           `json:"session_id"`
	Kind      string           `json:"kind"`
	Title     string           `json:"title,omitempty"`
	Questions []proto.Question `json:"questions,omitempty"`
	// Answers so far, a question at a time (Step is the one asked now).
	Answers map[string]string `json:"answers,omitempty"`
	Step    int               `json:"step,omitempty"`
	// Options are a terminal dialog's option keys, in the order they were numbered.
	Options []string  `json:"options,omitempty"`
	At      time.Time `json:"at"`
}

// NewAsk is ix as something to ask in channel.
func NewAsk(channel string, ix proto.Interaction, now time.Time) *Ask {
	return &Ask{ID: ix.ID, Channel: channel, MachineID: ix.MachineID, SessionID: ix.SessionID, Kind: ix.Kind, Title: ix.SessionTitle,
		Questions: ix.Questions, At: now}
}

// AskText is the text asking ix (it fills in a's options); false when it cannot be answered in a channel (the text
// then says where).
func AskText(ix proto.Interaction, a *Ask) (string, bool) {
	head := ""
	if a.Title != "" {
		head = "【" + a.Title + "】"
	}
	switch {
	case len(ix.Fields) > 0:
		return head + "有一步要在 App 里填写后才能批准：" + FirstLine(ix.Summary), false
	case ix.Kind == proto.IxPermission:
		what := strings.TrimSpace(ix.Summary)
		if what == "" {
			what = toolBrief(ix.ToolInput)
		}
		if ix.Reason != "" {
			what += "\n原因：" + ix.Reason
		}
		return head + "要你批准「" + toolName(ix) + "」：\n" + Clip(what, 1500) + "\n\n回复「允许」让它做这一次，「拒绝」不让它做。", true
	case ix.Kind == proto.IxPlan:
		return head + "Agent 写好了计划：\n\n" + Clip(ix.Plan, 3000) +
			"\n\n回复 1 批准并自动执行，2 批准、改文件时自动同意，「拒绝」打回；也可以直接写要改什么，会打回并带上你的话。", true
	case ix.Kind == proto.IxQuestion && len(ix.Questions) > 0:
		return head + QuestionText(a, 0), true
	case ix.Kind == proto.IxScreen && ix.Dialog != nil && len(ix.Dialog.Options) > 0 && !ix.Dialog.Multi:
		a.Options = nil
		var lines []string
		for i, o := range ix.Dialog.Options {
			a.Options = append(a.Options, o.Key)
			lines = append(lines, fmt.Sprintf("%d. %s", i+1, o.Label))
		}
		q := strings.TrimSpace(ix.Dialog.Question)
		if ix.Dialog.Detail != "" {
			q = strings.TrimSpace(ix.Dialog.Detail) + "\n" + q
		}
		return head + "终端里在问：\n" + Clip(q, 1500) + "\n" + strings.Join(lines, "\n") + "\n\n回复序号。", true
	}
	return head + "有 Agent 在等你处理：" + FirstLine(ix.Summary) + "\n这种要在 App 里处理。", false
}

// QuestionText asks a's question i.
func QuestionText(a *Ask, i int) string {
	q := a.Questions[i]
	var b strings.Builder
	if len(a.Questions) > 1 {
		fmt.Fprintf(&b, "（%d/%d）", i+1, len(a.Questions))
	}
	b.WriteString("Agent 问你：")
	if q.Header != "" && q.Header != q.Question {
		b.WriteString(q.Header + "：")
	}
	b.WriteString(q.Question)
	for j, o := range q.Options {
		fmt.Fprintf(&b, "\n%d. %s", j+1, o.Label)
		if o.Description != "" {
			b.WriteString(" — " + o.Description)
		}
	}
	switch {
	case len(q.Options) > 0 && q.MultiSelect:
		b.WriteString("\n\n回复序号（可以多选，用空格隔开），或者直接写回答。")
	case len(q.Options) > 0:
		b.WriteString("\n\n回复序号，或者直接写回答。")
	default:
		b.WriteString("\n\n直接回复你的回答。")
	}
	return b.String()
}

var (
	yes   = []string{"允许", "同意", "确认", "可以", "好", "好的", "行", "y", "yes", "ok", "allow"}
	no    = []string{"拒绝", "不允许", "不同意", "不行", "不要", "取消", "n", "no", "deny"}
	split = func(r rune) bool { return unicode.IsSpace(r) || r == ',' || r == '，' || r == '、' }
)

func is(text string, words []string) bool {
	t := strings.ToLower(strings.TrimRight(strings.TrimSpace(text), "。.!！"))
	return slices.Contains(words, t)
}

// ReadAnswer reads text as the answer to a: the resolution once complete (done), or what to say when text does not
// answer it (again). A question of several takes one answer per message: a.Step moves on, and the caller asks
// QuestionText(a, a.Step) next.
func ReadAnswer(a *Ask, text string) (r proto.Resolution, done bool, again string) {
	t, f := true, false
	switch a.Kind {
	case proto.IxPermission:
		switch {
		case is(text, yes):
			r.Allow = &t
		case is(text, no):
			r.Allow = &f
		default:
			return r, false, "回复「允许」或「拒绝」。"
		}
		return r, true, ""
	case proto.IxPlan:
		switch {
		case text == "1" || is(text, []string{"批准并自动执行", "自动执行"}):
			r.Approve, r.Mode = &t, "bypassPermissions"
		case text == "2" || is(text, append([]string{"批准"}, yes...)):
			r.Approve, r.Mode = &t, "acceptEdits"
		case is(text, no):
			r.Approve = &f
		default:
			r.Approve, r.Feedback = &f, text
		}
		return r, true, ""
	case proto.IxScreen:
		n, err := strconv.Atoi(strings.TrimSpace(text))
		if err != nil || n < 1 || n > len(a.Options) {
			return r, false, fmt.Sprintf("回复 1 到 %d 的序号。", len(a.Options))
		}
		r.Option = a.Options[n-1]
		return r, true, ""
	case proto.IxQuestion:
		if a.Step >= len(a.Questions) {
			return r, false, "这个问题对不上了，到 App 里回答。"
		}
		q := a.Questions[a.Step]
		if a.Answers == nil {
			a.Answers = map[string]string{}
		}
		a.Answers[q.Question] = optionAnswer(q, text)
		a.Step++
		if a.Step < len(a.Questions) {
			return r, false, ""
		}
		r.Answers = a.Answers
		return r, true, ""
	}
	return r, false, "这个要在 App 里处理。"
}

// optionAnswer is text as an answer to q: option numbers become their labels (several for a multi-select), anything
// else is the answer itself.
func optionAnswer(q proto.Question, text string) string {
	fields := strings.FieldsFunc(text, split)
	var labels []string
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 1 || n > len(q.Options) {
			return strings.TrimSpace(text)
		}
		labels = append(labels, q.Options[n-1].Label)
	}
	if len(labels) == 0 || len(labels) > 1 && !q.MultiSelect {
		return strings.TrimSpace(text)
	}
	return strings.Join(labels, ", ")
}

// Answered is the line saying what a reply did.
func Answered(r proto.Resolution) string {
	switch {
	case r.Allow != nil && *r.Allow:
		return "已允许。"
	case r.Allow != nil:
		return "已拒绝。"
	case r.Approve != nil && *r.Approve:
		return "已批准。"
	case r.Approve != nil && r.Feedback != "":
		return "已打回，带上了你的话。"
	case r.Approve != nil:
		return "已打回。"
	}
	return "已回答。"
}

func toolName(ix proto.Interaction) string {
	if ix.ToolName != "" {
		return ix.ToolName
	}
	return "一个操作"
}

// toolBrief is a tool call's input in a line or two: its command, file or URL when it has one.
func toolBrief(input json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(input, &m) != nil {
		return ""
	}
	for _, k := range []string{"command", "file_path", "path", "url", "pattern", "query"} {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return Clip(string(input), 300)
}

// FirstLine is s's first line, clipped.
func FirstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return Clip(line, 200)
}

// Clip is s cut to n runes, with an ellipsis when cut.
func Clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
