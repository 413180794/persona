package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/kb"
	"github.com/chyroc/jarvis/server/internal/lark"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// /agent anywhere in Feishu (docs/lark-agent.md): a message of the user's own that starts with the prefix ("/agent 看下
// 这个报错") starts an agent at once — a headless conversation like the assistant's, with the configured launcher — given
// the context of where it was asked (the chat or thread around it, the quoted message, the docs linked, what the
// knowledge base knows). The agent answers with lark_agent_reply; the hub posts it as a reply to the trigger, as the
// user, with a footer. A later /agent in the same thread goes on in the same conversation.

// LarkAgentConfig is hub.json's "lark_agent".
type LarkAgentConfig struct {
	Enabled bool `json:"enabled"`
	// Prefix starts a request (default "/agent", any case).
	Prefix string `json:"prefix,omitempty"`
	// Launcher runs the agent (the strongest model you have); empty: the assistant's.
	Launcher string `json:"launcher,omitempty"`
	// Machine runs it; empty: the assistant's machine.
	Machine string `json:"machine,omitempty"`
	// TimeoutMinutes without a reply ends the run with a short note in the chat (default 30).
	TimeoutMinutes int `json:"timeout_minutes,omitempty"`
	// ContextMessages: how many messages before the request go with it (default 40).
	ContextMessages int `json:"context_messages,omitempty"`
}

func (c LarkAgentConfig) prefix() string {
	if p := strings.TrimSpace(c.Prefix); p != "" {
		return p
	}
	return "/agent"
}

func (c LarkAgentConfig) timeout() time.Duration {
	if c.TimeoutMinutes > 0 {
		return time.Duration(c.TimeoutMinutes) * time.Minute
	}
	return 30 * time.Minute
}

func (c LarkAgentConfig) contextMessages() int {
	if c.ContextMessages > 0 {
		return min(c.ContextMessages, 100)
	}
	return 40
}

// larkAgentFooter ends every reply: whoever reads it knows an agent wrote it, and it never starts with the prefix.
const larkAgentFooter = "\n\n— 🤖 Jarvis Agent"

// LarkAgentRun is one request and what became of it; kept in settings (larkAgentRunsKey), the last few days.
type LarkAgentRun struct {
	MessageID string     `json:"message_id"`
	FeedID    string     `json:"feed_id"`
	ThreadID  string     `json:"thread_id,omitempty"`
	InThread  bool       `json:"in_thread,omitempty"` // reply into the thread
	Chat      string     `json:"chat,omitempty"`
	Task      string     `json:"task"`
	State     string     `json:"state"` // running, replied, failed
	MachineID string     `json:"machine_id,omitempty"`
	SessionID string     `json:"session_id,omitempty"`
	Error     string     `json:"error,omitempty"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}

const larkAgentRunsKey = "lark_agent.runs"

type larkAgents struct {
	mu     sync.Mutex
	loaded bool
	runs   []*LarkAgentRun
	since  time.Time // requests older than the hub's start are not answered (a restart replays nothing)
}

func (h *Hub) larkAgentCfg() LarkAgentConfig { return h.cfg.LarkAgent }

// larkAgentEvent looks at every message event for a request.
func (h *Hub) larkAgentEvent(ev lark.Event) {
	cfg := h.larkAgentCfg()
	if !cfg.Enabled || ev.Type != "message.upserted" || ev.Message == nil || len(ev.FeedIDs) == 0 {
		return
	}
	m := ev.Message
	task, ok := larkAgentTask(m, cfg.prefix())
	if !ok {
		return
	}
	since := h.larkAgent.since
	if m.CreatedAt.Before(since) || time.Since(m.CreatedAt) > 10*time.Minute {
		return // an old message edited, or one from before the hub started
	}
	if !h.claimLarkAgent(m.ID) {
		return
	}
	go h.runLarkAgent(context.Background(), cfg, ev.FeedIDs[0], *m, task)
}

var leadingMentions = regexp.MustCompile(`^(?:@\S+\s+)+`)

// larkAgentTask: the request in a message, if it is one — the user's own, a person's message (not a bot's, not a
// recalled one), starting with the prefix (after any @mentions), not one of the agent's own replies.
func larkAgentTask(m *lark.Message, prefix string) (string, bool) {
	if !m.IsMine || m.Recalled || m.Type == "SYSTEM" || (m.SenderType != "" && m.SenderType != "user") {
		return "", false
	}
	text := strings.TrimSpace(leadingMentions.ReplaceAllString(strings.TrimSpace(m.Text), ""))
	if strings.Contains(text, strings.TrimSpace(larkAgentFooter)) {
		return "", false
	}
	if len(text) < len(prefix) || !strings.EqualFold(text[:len(prefix)], prefix) {
		return "", false
	}
	rest := text[len(prefix):]
	if rest != "" && !strings.HasPrefix(rest, " ") && !strings.HasPrefix(rest, "\n") && !strings.HasPrefix(rest, "　") &&
		!strings.HasPrefix(rest, "：") && !strings.HasPrefix(rest, ":") {
		return "", false // "/agents …" is not "/agent …"
	}
	return strings.TrimSpace(strings.TrimLeft(rest, ":：")), true
}

func (h *Hub) loadLarkAgentRuns(ctx context.Context) {
	a := &h.larkAgent
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.loaded || h.store == nil {
		a.loaded = true
		return
	}
	var runs []*LarkAgentRun
	if _, err := h.store.Setting(ctx, larkAgentRunsKey, &runs); err != nil {
		h.logf("lark agent: load runs: %v", err)
	}
	a.runs, a.loaded = runs, true
}

func (h *Hub) saveLarkAgentRuns(ctx context.Context) {
	if h.store == nil {
		return
	}
	a := &h.larkAgent
	a.mu.Lock()
	cut := time.Now().Add(-72 * time.Hour)
	kept := a.runs[:0]
	for _, r := range a.runs {
		if r.State == "running" || r.StartedAt.After(cut) {
			kept = append(kept, r)
		}
	}
	a.runs = kept
	b, _ := json.Marshal(a.runs)
	a.mu.Unlock()
	if err := h.store.PutSetting(ctx, larkAgentRunsKey, json.RawMessage(b)); err != nil {
		h.logf("lark agent: save runs: %v", err)
	}
}

// claimLarkAgent makes each message answered once (lark-connector re-emits a message when it is edited or its thread grows).
func (h *Hub) claimLarkAgent(messageID string) bool {
	a := &h.larkAgent
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, r := range a.runs {
		if r.MessageID == messageID {
			return false
		}
	}
	a.runs = append(a.runs, &LarkAgentRun{MessageID: messageID, State: "running", StartedAt: time.Now().UTC()})
	return true
}

func (h *Hub) larkAgentRun(match func(*LarkAgentRun) bool) *LarkAgentRun {
	for i := len(h.larkAgent.runs) - 1; i >= 0; i-- {
		if r := h.larkAgent.runs[i]; match(r) {
			return r
		}
	}
	return nil
}

func (h *Hub) runLarkAgent(ctx context.Context, cfg LarkAgentConfig, feedID string, m lark.Message, task string) {
	inThread := m.ThreadKind != ""
	h.larkAgent.mu.Lock()
	run := h.larkAgentRun(func(r *LarkAgentRun) bool { return r.MessageID == m.ID })
	run.FeedID, run.ThreadID, run.InThread, run.Task = feedID, m.ThreadID, inThread, clip(task, 500)
	// The same thread asked before: go on in that conversation (it has everything said there so far).
	var resume, resumeMachine string
	if m.ThreadID != "" {
		if prev := h.larkAgentRun(func(r *LarkAgentRun) bool {
			return r != run && r.ThreadID == m.ThreadID && r.SessionID != ""
		}); prev != nil {
			resume, resumeMachine = prev.SessionID, prev.MachineID
		}
	}
	h.larkAgent.mu.Unlock()
	h.saveLarkAgentRuns(ctx)

	if err := h.lark.React(ctx, m.ID, "OnIt", false); err != nil { // "on it", like lark-connector's stand-in
		h.logf("lark agent: react: %v", err)
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	chat, contextText := h.larkAgentContext(cctx, cfg, feedID, m)
	prompt := larkAgentPrompt(h.ownerName(), chat, task, contextText, resume != "")
	machineID, sessionID, err := h.startLarkAgent(cctx, cfg, resumeMachine, resume, prompt)
	cancel()

	h.larkAgent.mu.Lock()
	run.Chat = chat
	if err != nil {
		run.State, run.Error = "failed", clip(oneLine(err.Error()), 300)
	} else {
		run.MachineID, run.SessionID = machineID, sessionID
	}
	h.larkAgent.mu.Unlock()
	h.saveLarkAgentRuns(ctx)
	if err != nil {
		h.logf("lark agent: start for %s: %v", m.ID, err)
		h.larkAgentPost(ctx, run, "这次没能开始处理（"+clip(oneLine(err.Error()), 80)+"）。")
	}
}

// startLarkAgent starts (or, with a session, continues) a headless conversation in the assistant's directory — the same
// tools: Feishu, mail, the knowledge base, Agents on every computer — with the configured launcher.
func (h *Hub) startLarkAgent(ctx context.Context, cfg LarkAgentConfig, machineID, sessionID, prompt string) (string, string, error) {
	ac := h.assistantConfig(ctx)
	switch {
	case machineID != "" && h.byID[machineID] != nil:
		ac.MachineID = machineID
	case h.byID[cfg.Machine] != nil:
		ac.MachineID = cfg.Machine
	}
	if cfg.Launcher != "" {
		ac.Launcher = cfg.Launcher
	}
	m := h.byID[ac.MachineID]
	if m == nil {
		return "", "", errors.New("no machine for the agent")
	}
	var prep struct {
		Cwd string `json:"cwd"`
	}
	if err := m.call(ctx, "assistant.prepare", map[string]any{"dir": assistantDir, "claude_md": h.assistantClaudeMD(ctx)}, &prep); err != nil {
		return "", "", err
	}
	var res struct {
		SessionID string `json:"session_id"`
	}
	params := map[string]any{"cwd": prep.Cwd, "launcher": ac.Launcher, "text": prompt}
	if sessionID != "" {
		params["session_id"] = sessionID
	}
	if err := m.call(ctx, "assistant.send", params, &res); err != nil {
		return "", "", err
	}
	return m.cfg.ID, res.SessionID, nil
}

// --- context: where the request was made ---

var docLink = regexp.MustCompile(`https?://[^\s<>"'）)]+/(?:docx|docs|wiki|sheets|base|mindnotes|file)/[A-Za-z0-9_-]+[^\s<>"'）)]*`)

// larkAgentContext gathers what the agent needs to work on a request made somewhere else than a herdr tab: the
// conversation (the thread, or the messages before the request), the quoted message, the docs linked there, and what
// the knowledge base knows about the chat and its projects. Returns the chat's name too.
func (h *Hub) larkAgentContext(ctx context.Context, cfg LarkAgentConfig, feedID string, m lark.Message) (string, string) {
	var b strings.Builder
	chat := feedID
	var kc *kb.Chat
	if e := h.kb.eng; e != nil {
		if c, ok := e.Store.Chat(kb.ChatID("lark", feedID)); ok {
			kc, chat = c, c.Name
		}
	}
	kind := "会话"
	if kc != nil {
		kind = map[string]string{"dm": "私聊", "group": "群聊", "bot": "机器人会话"}[kc.Kind]
	}
	if m.ThreadKind != "" {
		kind += "里的一个话题"
	}
	fmt.Fprintf(&b, "<jarvis-context kind=\"context\" title=\"%s\">\n位置：%s「%s」（feed_id %s）；请求消息 id %s，%s 发出。\n</jarvis-context>\n",
		xmlAttr("请求的位置"), kind, chat, feedID, m.ID, m.CreatedAt.Local().Format("2006-01-02 15:04"))

	// The conversation around it.
	var msgs []lark.Message
	if m.ThreadKind != "" {
		if p, err := h.lark.Thread(ctx, m.ID, "", cfg.contextMessages()); err == nil {
			msgs = p.Messages
		} else {
			h.logf("lark agent: thread of %s: %v", m.ID, err)
		}
	}
	if len(msgs) == 0 {
		if p, err := h.lark.Messages(ctx, feedID, lark.Window{Before: m.Position}, cfg.contextMessages(), false); err == nil {
			msgs = p.Messages
		} else {
			h.logf("lark agent: messages of %s: %v", feedID, err)
		}
	}
	if len(msgs) > 0 {
		title := "前面的聊天"
		if m.ThreadKind != "" {
			title = "这个话题里的消息"
		}
		fmt.Fprintf(&b, "<jarvis-context kind=\"context\" title=\"%s\">\n%s</jarvis-context>\n", xmlAttr(title), larkTranscript(msgs, m.ID, 30000))
	}
	if q := m.ReplyTo; q != nil && !q.Recalled {
		fmt.Fprintf(&b, "<jarvis-context kind=\"context\" title=\"%s\">\n%s：%s\n</jarvis-context>\n", xmlAttr("请求引用的消息"), q.SenderName, clipRunes(q.Text, 4000))
	}

	// Docs linked in the request, the quote, and the last messages.
	sources := []string{m.Text}
	if m.ReplyTo != nil {
		sources = append(sources, m.ReplyTo.Text)
	}
	for i := len(msgs) - 1; i >= 0 && i >= len(msgs)-10; i-- {
		sources = append(sources, msgs[i].Text)
	}
	seen := map[string]bool{}
	for _, s := range sources {
		for _, link := range docLink.FindAllString(s, -1) {
			if seen[link] || len(seen) >= 3 {
				continue
			}
			seen[link] = true
			title, md, err := h.lark.DocMarkdown(ctx, link)
			if err != nil {
				fmt.Fprintf(&b, "<jarvis-context kind=\"doc\" title=\"%s\" url=\"%s\">\n（读不到：%s）\n</jarvis-context>\n", xmlAttr("文档"), xmlAttr(link), clip(oneLine(err.Error()), 120))
				continue
			}
			fmt.Fprintf(&b, "<jarvis-context kind=\"doc\" title=\"%s\" url=\"%s\">\n%s\n</jarvis-context>\n", xmlAttr("文档 · "+title), xmlAttr(link), clipRunes(md, 8000))
		}
	}

	// What the knowledge base knows.
	if e := h.kb.eng; e != nil && kc != nil {
		if sum := e.Store.Summary(kb.KindChat, kc.ID); sum != "" {
			fmt.Fprintf(&b, "<jarvis-context kind=\"context\" title=\"%s\">\n%s\n</jarvis-context>\n", xmlAttr("会话档案 · "+kc.Name), clipRunes(sum, 4000))
		}
		for i, slug := range kc.Projects {
			if i >= 3 {
				break
			}
			if p, ok := e.Store.Project(slug); ok {
				fmt.Fprintf(&b, "<jarvis-context kind=\"context\" title=\"%s\">\n%s\n</jarvis-context>\n", xmlAttr("项目 · "+p.Name), clipRunes(e.Store.Summary(kb.KindProject, slug), 3000))
			}
		}
		fmt.Fprintf(&b, "\n知识库里这个会话的 id：%s（kb_get / kb_search 查更多）。\n", kc.ID)
	}
	return chat, b.String()
}

// larkTranscript: one line per message, "01-02 15:04 名字：文字", oldest first, the request marked; the newest that fit.
func larkTranscript(msgs []lark.Message, requestID string, limit int) string {
	sort.SliceStable(msgs, func(i, j int) bool { return msgs[i].CreatedAt.Before(msgs[j].CreatedAt) })
	lines := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if m.Type == "SYSTEM" {
			continue
		}
		text := strings.TrimSpace(m.Text)
		if m.Recalled {
			text = "[已撤回]"
		}
		if text == "" {
			continue
		}
		who := m.SenderName
		if m.IsMine {
			who += "（我）"
		}
		mark := ""
		if m.ID == requestID {
			mark = "  ← 请求"
		}
		lines = append(lines, fmt.Sprintf("%s %s：%s%s", m.CreatedAt.Local().Format("01-02 15:04"), who, clipRunes(text, 2000), mark))
	}
	var out []string
	n := 0
	for i := len(lines) - 1; i >= 0; i-- {
		n += len(lines[i]) + 1
		if n > limit {
			break
		}
		out = append([]string{lines[i]}, out...)
	}
	return strings.Join(out, "\n") + "\n"
}

func larkAgentPrompt(owner, chat, task, contextText string, resumed bool) string {
	if owner == "" {
		owner = "我"
	}
	var b strings.Builder
	if resumed {
		fmt.Fprintf(&b, "%s在同一个话题里又发了一条 /agent 请求（下面是最新的上下文）。\n\n", owner)
	} else {
		fmt.Fprintf(&b, "%s在飞书「%s」里用 /agent 叫你。这不是在电脑上开的会话，你只知道下面给出的上下文；不够就自己查（lark_*、kb_*、"+
			"读文档、看各台电脑上的 Agent）。\n\n", owner, chat)
	}
	if task == "" {
		task = "（没写具体要求：看上下文和引用的消息，判断他想让你做什么，比如回答问题、总结、给结论。）"
	}
	fmt.Fprintf(&b, "## 请求\n\n%s\n\n## 上下文\n\n%s\n", task, contextText)
	b.WriteString(`
## 怎么做

- 能做的直接做掉：查资料、读代码、看日志、跑命令、开 Agent 干活都可以。
- 做完用 lark_agent_reply 回复一次（只调一次，在最后）：hub 会以` + owner + `的身份回复那条请求（话题里就回在话题里），末尾自动加「🤖 Jarvis Agent」。
  回复写给在场的人看：直接给结论和要点，Markdown，中文，短一些；不要写「我查了一下」这类过程，不要贴大段日志。
- 别用 lark_send 或别的方式自己往飞书发消息，别 ask_user / notify_user 打扰` + owner + `：这里的回复就是交付。
- 有新结论（项目进展、谁负责什么、截止时间）用 kb_update 写回知识库。
`)
	return b.String()
}

// --- replying ---

var larkAgentReplyTool = proto.Tool{Name: "lark_agent_reply", Description: "Answer the Feishu /agent request this conversation " +
	"was started for: the hub posts `text` (Markdown) as the user's reply to the request, in its thread if it was asked in one, " +
	"with a footer saying an agent wrote it. Call it once, at the end, with the answer for the people in that chat.",
	InputSchema: schema(`{"type":"object","required":["text"],"properties":{"text":{"type":"string"}}}`)}

func (h *Hub) isLarkAgent(sessionID string) bool {
	h.larkAgent.mu.Lock()
	defer h.larkAgent.mu.Unlock()
	return h.larkAgentRun(func(r *LarkAgentRun) bool { return r.SessionID == sessionID }) != nil
}

func (h *Hub) toolLarkAgentReply(ctx context.Context, sessionID string, args json.RawMessage) (string, error) {
	var a struct{ Text string }
	if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.Text) == "" {
		return "", errors.New("text is required")
	}
	h.larkAgent.mu.Lock()
	run := h.larkAgentRun(func(r *LarkAgentRun) bool { return r.SessionID == sessionID && r.State == "running" })
	h.larkAgent.mu.Unlock()
	if run == nil {
		return "", errors.New("this conversation has no /agent request waiting for an answer (already answered, or not started for one)")
	}
	if err := h.larkAgentPost(ctx, run, strings.TrimSpace(a.Text)); err != nil {
		return "", err
	}
	return "Posted in the chat. You're done.", nil
}

// larkAgentPost replies to the request and ends the run.
func (h *Hub) larkAgentPost(ctx context.Context, run *LarkAgentRun, text string) error {
	err := h.lark.Reply(ctx, run.MessageID, clipRunes(text, 8000)+larkAgentFooter, run.InThread, true)
	h.larkAgent.mu.Lock()
	now := time.Now().UTC()
	if err != nil {
		run.Error = clip(oneLine(err.Error()), 300)
	} else {
		if run.State == "running" {
			run.State = "replied"
		}
		run.EndedAt = &now
	}
	h.larkAgent.mu.Unlock()
	h.saveLarkAgentRuns(ctx)
	if err != nil {
		h.logf("lark agent: reply to %s: %v", run.MessageID, err)
		return fmt.Errorf("posting the reply failed: %w", err)
	}
	_ = h.lark.React(ctx, run.MessageID, "OnIt", true)
	_ = h.lark.React(ctx, run.MessageID, "DONE", false)
	return nil
}

// larkAgentLoop sweeps running requests: a conversation that ended its turn without lark_agent_reply has its last
// answer posted instead; one past the timeout gets a short note.
func (h *Hub) larkAgentLoop(ctx context.Context) {
	h.larkAgent.since = time.Now().Add(-time.Minute)
	h.loadLarkAgentRuns(ctx)
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if !h.larkAgentCfg().Enabled {
			continue
		}
		h.sweepLarkAgents(ctx, time.Now())
	}
}

func (h *Hub) sweepLarkAgents(ctx context.Context, now time.Time) {
	cfg := h.larkAgentCfg()
	h.larkAgent.mu.Lock()
	var running []*LarkAgentRun
	for _, r := range h.larkAgent.runs {
		if r.State == "running" && r.SessionID != "" {
			running = append(running, r)
		}
	}
	h.larkAgent.mu.Unlock()
	for _, r := range running {
		m := h.byID[r.MachineID]
		if m == nil {
			continue
		}
		if now.Sub(r.StartedAt) > cfg.timeout() {
			h.larkAgent.mu.Lock()
			r.State = "failed"
			h.larkAgent.mu.Unlock()
			_ = h.larkAgentPost(ctx, r, fmt.Sprintf("这次 %d 分钟内没处理完，先停在这里。", int(cfg.timeout().Minutes())))
			continue
		}
		s, ok := m.session(r.SessionID)
		if !ok || s.Status == proto.StatusWorking || now.Sub(r.StartedAt) < 30*time.Second {
			continue
		}
		// The turn ended without lark_agent_reply: its last answer is the reply.
		var res struct {
			Items []proto.Item `json:"items"`
		}
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := m.call(cctx, "session.items", map[string]any{"session_id": r.SessionID, "turns": 1}, &res)
		cancel()
		if err != nil {
			continue
		}
		answer := ""
		for i := len(res.Items) - 1; i >= 0; i-- {
			if it := res.Items[i]; it.Kind == proto.KindText && it.Role == "assistant" && strings.TrimSpace(it.Text) != "" {
				answer = it.Text
				break
			}
		}
		h.larkAgent.mu.Lock()
		still := r.State == "running"
		h.larkAgent.mu.Unlock()
		if !still {
			continue // answered meanwhile
		}
		if answer == "" {
			answer = "这次没有得出结论。"
		}
		_ = h.larkAgentPost(ctx, r, answer)
	}
}
