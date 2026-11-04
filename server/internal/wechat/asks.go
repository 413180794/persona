package wechat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/wechat/ilink"
)

// WeChat has no cards: what an agent the owner drives from WeChat asks (a question, a permission, a plan, a terminal
// dialog) is sent as text, and the owner's next message answers it — a number, 允许 / 拒绝, 批准, or words. The
// latest one asked is the one a reply answers. One answered somewhere else (the app) is let go: the reply is then an
// ordinary message.

type ask struct {
	ID        string           `json:"id"`
	Machine   string           `json:"machine"`
	Session   string           `json:"session"`
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

type needsYou struct {
	MachineID     string `json:"machine_id"`
	SessionID     string `json:"session_id"`
	InteractionID string `json:"interaction_id"`
}

// Hear takes the hub's events: an agent asks something, or a turn ended.
func (b *Bot) Hear(ctx context.Context, event string, params json.RawMessage) {
	switch event {
	case "agent.finished":
		b.poke()
	case "agent.needs_you":
		var n needsYou
		if json.Unmarshal(params, &n) == nil && n.InteractionID != "" {
			b.asked(ctx, n)
		}
	}
}

// drives says whether WeChat drives the session: a turn waits on it, or it is where the owner's messages go.
func (b *Bot) drives(ctx context.Context, machine, session string) bool {
	b.mu.Lock()
	bound := b.st.Creds != nil
	t := b.st.Target
	waits := slices.ContainsFunc(b.st.Runs, func(r *run) bool { return r.Machine == machine && r.Session == session })
	b.mu.Unlock()
	switch {
	case !bound:
		return false
	case waits || t.Machine == machine && t.Session == session:
		return true
	case t.main():
		m, main, err := b.opt.Hub.Assistant(ctx)
		return err == nil && m == machine && main == session
	}
	return false
}

func (b *Bot) asked(ctx context.Context, n needsYou) {
	if err := b.load(ctx); err != nil || !b.drives(ctx, n.MachineID, n.SessionID) {
		return
	}
	pending, err := b.opt.Hub.Interactions(ctx)
	if err != nil {
		return
	}
	i := slices.IndexFunc(pending, func(ix proto.Interaction) bool { return ix.ID == n.InteractionID })
	if i < 0 {
		return
	}
	ix := pending[i]
	a := &ask{ID: ix.ID, Machine: ix.MachineID, Session: ix.SessionID, Kind: ix.Kind, Title: ix.SessionTitle, Questions: ix.Questions,
		At: b.opt.Now()}
	text, ok := promptFor(&ix, a)
	if !ok {
		b.tell(ctx, text)
		return
	}
	b.mu.Lock()
	known := slices.ContainsFunc(b.st.Asks, func(x *ask) bool { return x.ID == a.ID })
	b.mu.Unlock()
	if known || !b.tell(ctx, text) {
		return // not asked in WeChat (no chat to send it to yet): the app has it
	}
	b.mu.Lock()
	if !slices.ContainsFunc(b.st.Asks, func(x *ask) bool { return x.ID == a.ID }) {
		b.st.Asks = append(b.st.Asks, a)
		b.saveLocked(ctx)
	}
	b.mu.Unlock()
}

// promptFor is the text asking ix in WeChat; false when it cannot be answered here (the text then says where).
func promptFor(ix *proto.Interaction, a *ask) (string, bool) {
	head := ""
	if a.Title != "" {
		head = "【" + a.Title + "】"
	}
	switch {
	case len(ix.Fields) > 0:
		return head + "有一步要在 App 里填写后才能批准：" + firstLine(ix.Summary), false
	case ix.Kind == proto.IxPermission:
		what := strings.TrimSpace(ix.Summary)
		if what == "" {
			what = toolBrief(ix.ToolInput)
		}
		if ix.Reason != "" {
			what += "\n原因：" + ix.Reason
		}
		return head + "要你批准「" + toolName(ix) + "」：\n" + clip(what, 1500) + "\n\n回复「允许」让它做这一次，「拒绝」不让它做。", true
	case ix.Kind == proto.IxPlan:
		return head + "Agent 写好了计划：\n\n" + clip(ix.Plan, 3000) +
			"\n\n回复 1 批准并自动执行，2 批准、改文件时自动同意，「拒绝」打回；也可以直接写要改什么，会打回并带上你的话。", true
	case ix.Kind == proto.IxQuestion && len(ix.Questions) > 0:
		return head + questionText(a, 0), true
	case ix.Kind == proto.IxScreen && ix.Dialog != nil && len(ix.Dialog.Options) > 0 && !ix.Dialog.Multi:
		var lines []string
		for i, o := range ix.Dialog.Options {
			a.Options = append(a.Options, o.Key)
			lines = append(lines, fmt.Sprintf("%d. %s", i+1, o.Label))
		}
		q := strings.TrimSpace(ix.Dialog.Question)
		if ix.Dialog.Detail != "" {
			q = strings.TrimSpace(ix.Dialog.Detail) + "\n" + q
		}
		return head + "终端里在问：\n" + clip(q, 1500) + "\n" + strings.Join(lines, "\n") + "\n\n回复序号。", true
	}
	return head + "有 Agent 在等你处理：" + firstLine(ix.Summary) + "\n这种要在 App 里处理。", false
}

func questionText(a *ask, i int) string {
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

// answerAsk takes the owner's message as the answer to the latest thing asked; false when nothing waits (or what
// waited was answered elsewhere): the message is then an ordinary one.
func (b *Bot) answerAsk(ctx context.Context, c *ilink.Client, creds ilink.Credentials, m ilink.Message, text string) bool {
	if text == "" {
		return false
	}
	b.mu.Lock()
	waiting := len(b.st.Asks) > 0
	b.mu.Unlock()
	if !waiting {
		return false
	}
	pending, err := b.opt.Hub.Interactions(ctx)
	if err != nil {
		return false
	}
	b.mu.Lock()
	b.st.Asks = slices.DeleteFunc(b.st.Asks, func(a *ask) bool {
		return !slices.ContainsFunc(pending, func(ix proto.Interaction) bool { return ix.ID == a.ID })
	})
	if len(b.st.Asks) == 0 {
		b.saveLocked(ctx)
		b.mu.Unlock()
		return false
	}
	a := b.st.Asks[len(b.st.Asks)-1]
	b.mu.Unlock()
	reply := func(s string) { b.send(ctx, c, m.From, m.ContextToken, s) }

	r, done, again := resolution(a, text)
	if again != "" {
		reply(again)
		b.save(ctx)
		return true
	}
	if !done {
		b.save(ctx) // a question answered, the next one asked
		reply(questionText(a, a.Step))
		return true
	}
	err = b.opt.Hub.Resolve(ctx, a.ID, r)
	b.mu.Lock()
	b.st.Asks = slices.DeleteFunc(b.st.Asks, func(x *ask) bool { return x.ID == a.ID })
	left := len(b.st.Asks)
	b.saveLocked(ctx)
	b.mu.Unlock()
	switch {
	case errors.Is(err, errGone):
		reply("这个已经在别处处理过了。")
	case err != nil:
		reply("没答上：" + err.Error())
	default:
		reply(answered(a, r))
	}
	if left > 0 {
		reply(fmt.Sprintf("还有 %d 个在等你回复，最近的那个是上面倒数第二条。", left))
	}
	b.poke()
	return true
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

// resolution reads text as the answer to a: the resolution once complete (done), or what to say when text does not
// answer it (again). A question of several takes one answer per message: a.Step moves on.
func resolution(a *ask, text string) (r proto.Resolution, done bool, again string) {
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

func answered(a *ask, r proto.Resolution) string {
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

func toolName(ix *proto.Interaction) string {
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
	return clip(string(input), 300)
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return clip(line, 200)
}

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
