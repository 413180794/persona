package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/chyroc/jarvis/server/internal/ai"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// The inbox's AI 分身 (docs/inbox.md): model 1 triages every new item (inbox_triage.go), model 2 decides which of
// the 需要你 items an agent should take on, and the agent (model 3, a headless Claude Code in the assistant's
// directory, with its tools) looks into it and drafts the reply. The draft waits in the inbox; nothing is sent until
// the user taps 发送 (POST /api/v1/inbox/{id}/reply).

// InboxAgentConfig is hub.json's inbox.agent.
type InboxAgentConfig struct {
	Enabled bool `json:"enabled"`
	// Machine and Launcher run the agent; default: the assistant's (assistant.go).
	Machine  string `json:"machine,omitempty"`
	Launcher string `json:"launcher,omitempty"`
	// Concurrency is how many agents work at once (default 1).
	Concurrency int `json:"concurrency,omitempty"`
	// TimeoutMinutes gives up on an agent that has not drafted by then (default 15).
	TimeoutMinutes int `json:"timeout_minutes,omitempty"`
	// MaxAgeHours: items whose newest message is older are not handed over (default 24), so turning this on does
	// not start an agent for every old item.
	MaxAgeHours int `json:"max_age_hours,omitempty"`
	// Rules is the user's own guidance for model 2 ("anything about the release goes to the agent"…).
	Rules string `json:"rules,omitempty"`
}

func (c InboxAgentConfig) concurrency() int { return max(c.Concurrency, 1) }

func (c InboxAgentConfig) timeout() time.Duration {
	if c.TimeoutMinutes <= 0 {
		return 15 * time.Minute
	}
	return time.Duration(c.TimeoutMinutes) * time.Minute
}

func (c InboxAgentConfig) maxAge() time.Duration {
	if c.MaxAgeHours <= 0 {
		return 24 * time.Hour
	}
	return time.Duration(c.MaxAgeHours) * time.Hour
}

const (
	AgentSkipped = "skipped" // model 2 kept it for the user
	AgentQueued  = "queued"
	AgentRunning = "running"
	AgentDrafted = "drafted"
	AgentFailed  = "failed"
)

// InboxAgent is the AI 分身's work on one item.
type InboxAgent struct {
	State     string     `json:"state"`
	Task      string     `json:"task,omitempty"`   // model 2's instruction to the agent
	Reason    string     `json:"reason,omitempty"` // model 2's why, short
	MachineID string     `json:"machine_id,omitempty"`
	SessionID string     `json:"session_id,omitempty"` // the agent's conversation (对话 tab): how it got there
	Draft     string     `json:"draft,omitempty"`
	Note      string     `json:"note,omitempty"` // what the agent found, for the user to check the draft against
	Error     string     `json:"error,omitempty"`
	Of        string     `json:"of,omitempty"` // the item's Last the work answers
	StartedAt *time.Time `json:"started_at,omitempty"`
	DraftedAt *time.Time `json:"drafted_at,omitempty"`
}

func (h *Hub) judgeModel() *ai.Client {
	if h.judgeAI != nil {
		return h.judgeAI
	}
	return h.ai
}

// handInbox is one pass of models 2 and 3: settle agents that finished or hung, judge newly triaged 需要你 items,
// start agents for the queued ones. One pass at a time, in the background.
func (h *Hub) handInbox(ctx context.Context) {
	cfg := h.cfg.Inbox.Agent
	if !cfg.Enabled || h.judgeModel() == nil || !h.inbox.handing.TryLock() {
		return
	}
	go func() {
		defer h.inbox.handing.Unlock()
		h.sweepInboxAgents(ctx, cfg, time.Now())
		for _, it := range h.inbox.toJudge(time.Now().Add(-cfg.maxAge())) {
			if h.judgeOne(ctx, it) {
				h.saveInbox(ctx, []*InboxItem{it})
			}
		}
		h.startInboxAgents(ctx, cfg)
	}()
}

// toJudge: open 需要你 items triaged on their current content and not judged on it yet, newest first.
func (b *inbox) toJudge(since time.Time) []*InboxItem {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []*InboxItem
	for _, it := range b.items {
		if it.open() && it.Tier == TierAction && it.TriagedAt == it.Last && it.JudgedAt != it.Last && it.Context != "" &&
			it.UpdatedAt.After(since) && (it.Agent == nil || it.Agent.State != AgentRunning) {
			out = append(out, it)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

const judgePrompt = `你是用户（称呼：%s）的 AI 分身的调度员。收件箱里的这件事已经判定为「需要用户处理」。判断要不要先交给 AI 分身：
一个能用工具的 Agent，能读飞书会话、文档、日程，搜索飞书，查邮件，看用户各台电脑上的 Agent 和代码，上网。它查清楚以后起草一条回复，
用户看过点一下就发出去。

- 交给它（handle=true）：对方在等用户回复或给个结果，而且 Agent 能出力：问的是查得到的事实（进度、结论、文档 / 代码 / 数据里的东西）、
  要材料或链接、要一个确认或回执、要基于资料写一段说明；需要用户拍板的事也可以交，Agent 会把要用户定的地方标出来。
- 不交（handle=false）：不需要回复（通知、公告、讨论已经结束、对方只是告知）；内容太少看不出对方要什么；需要用户本人线下去做的事。

task：交给它时写给 Agent 的具体指令，一两句：要查什么、回复要回答什么。比如「查 1013 分支是否已合入 codex 的改动，回复对方结论和合入的 MR」。
reason：不超过 12 个字，为什么这么定。

回复 JSON：{"handle":true,"task":"…","reason":"…"}`

// judgeOne asks model 2 about one item. A model failure leaves it unjudged, so the next pass tries again.
func (h *Hub) judgeOne(ctx context.Context, it *InboxItem) bool {
	h.inbox.mu.Lock()
	last, input := it.Last, triageInput(it)
	if it.Summary != "" {
		input = "摘要：" + it.Summary + "\n" + input
	}
	h.inbox.mu.Unlock()
	system := fmt.Sprintf(judgePrompt, h.ownerName())
	if r := strings.TrimSpace(h.cfg.Inbox.Agent.Rules); r != "" {
		system += "\n\n用户自己定的规则（优先于上面的默认判断）：\n" + r
	}
	var out struct {
		Handle bool   `json:"handle"`
		Task   string `json:"task"`
		Reason string `json:"reason"`
	}
	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	err := h.judgeModel().JSON(cctx, system, input, &out)
	cancel()
	if err != nil {
		if ctx.Err() == nil {
			h.logf("inbox: judge %s: %v", it.ID, err)
		}
		return false
	}
	h.inbox.mu.Lock()
	defer h.inbox.mu.Unlock()
	if it.Last != last || !it.open() {
		return false
	}
	it.JudgedAt = last
	task := strings.TrimSpace(out.Task)
	if out.Handle && task == "" {
		task = "看清楚对方要什么，查需要的信息，起草回复"
	}
	if out.Handle {
		it.Agent = &InboxAgent{State: AgentQueued, Task: clip(oneLine(task), 300), Reason: clip(oneLine(out.Reason), 30), Of: last}
	} else {
		it.Agent = &InboxAgent{State: AgentSkipped, Reason: clip(oneLine(out.Reason), 30), Of: last}
	}
	return true
}

// startInboxAgents starts queued items, newest first, while fewer than the configured number are running.
func (h *Hub) startInboxAgents(ctx context.Context, cfg InboxAgentConfig) {
	h.inbox.mu.Lock()
	running := 0
	var queued []*InboxItem
	for _, it := range h.inbox.items {
		if it.Agent == nil {
			continue
		}
		switch {
		case it.Agent.State == AgentRunning:
			running++
		case it.Agent.State == AgentQueued && it.open():
			queued = append(queued, it)
		}
	}
	sort.Slice(queued, func(i, j int) bool { return queued[i].UpdatedAt.After(queued[j].UpdatedAt) })
	queued = queued[:min(len(queued), max(cfg.concurrency()-running, 0))]
	type job struct {
		it     *InboxItem
		prompt string
	}
	jobs := make([]job, 0, len(queued))
	now := time.Now().UTC()
	for _, it := range queued {
		it.Agent.State, it.Agent.StartedAt = AgentRunning, &now
		jobs = append(jobs, job{it, inboxAgentPrompt(it, h.ownerName()) + h.kbContextFor(it)})
	}
	h.inbox.mu.Unlock()
	for _, j := range jobs {
		cctx, cancel := context.WithTimeout(ctx, 110*time.Second)
		machineID, sessionID, err := h.startInboxAgent(cctx, cfg, j.prompt)
		cancel()
		h.inbox.mu.Lock()
		if a := j.it.Agent; a != nil && a.State == AgentRunning && a.SessionID == "" {
			if err != nil {
				a.State, a.Error = AgentFailed, clip(oneLine(err.Error()), 200)
				h.logf("inbox: agent for %s: %v", j.it.ID, err)
			} else {
				a.MachineID, a.SessionID = machineID, sessionID
			}
		}
		h.inbox.mu.Unlock()
		h.saveInbox(ctx, []*InboxItem{j.it})
	}
}

// startInboxAgent starts a headless conversation like the assistant's (same directory, so the same tools) with
// the inbox agent's launcher.
func (h *Hub) startInboxAgent(ctx context.Context, cfg InboxAgentConfig, prompt string) (machineID, sessionID string, err error) {
	ac := h.assistantConfig(ctx)
	if h.byID[cfg.Machine] != nil {
		ac.MachineID = cfg.Machine
	}
	if cfg.Launcher != "" {
		ac.Launcher = cfg.Launcher
	}
	m := h.byID[ac.MachineID]
	if m == nil {
		return "", "", errors.New("no machine for the inbox agent")
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
	if err := m.call(ctx, "assistant.send", map[string]any{"cwd": prep.Cwd, "launcher": ac.Launcher, "text": prompt}, &res); err != nil {
		return "", "", err
	}
	return m.cfg.ID, res.SessionID, nil
}

// sweepInboxAgents fails agents that ended their turn without a draft, or ran past the timeout.
func (h *Hub) sweepInboxAgents(ctx context.Context, cfg InboxAgentConfig, now time.Time) {
	h.inbox.mu.Lock()
	var running []*InboxItem
	for _, it := range h.inbox.items {
		if it.Agent != nil && it.Agent.State == AgentRunning && it.Agent.SessionID != "" {
			running = append(running, it)
		}
	}
	h.inbox.mu.Unlock()
	var changed []*InboxItem
	for _, it := range running {
		h.inbox.mu.Lock()
		a := *it.Agent
		h.inbox.mu.Unlock()
		reason := ""
		if a.StartedAt != nil && now.Sub(*a.StartedAt) > cfg.timeout() {
			reason = "超时，没有给出草稿"
		} else if m := h.byID[a.MachineID]; m != nil && a.StartedAt != nil && now.Sub(*a.StartedAt) > time.Minute {
			if s, ok := m.session(a.SessionID); ok && s.Status != proto.StatusWorking {
				reason = "做完了但没有给出草稿"
			}
		}
		if reason == "" {
			continue
		}
		h.inbox.mu.Lock()
		if it.Agent != nil && it.Agent.State == AgentRunning && it.Agent.SessionID == a.SessionID {
			it.Agent.State, it.Agent.Error = AgentFailed, reason
			changed = append(changed, it)
		}
		h.inbox.mu.Unlock()
	}
	h.saveInbox(ctx, changed)
}

func inboxAgentPrompt(it *InboxItem, owner string) string {
	title, ref := it.Title, ""
	switch {
	case it.Lark != nil:
		title = "飞书 · " + it.Title
		ref = "飞书会话 id（lark_messages 用）：" + it.Lark.FeedID + "\n"
	case it.Mail != nil && len(it.Mail.UIDs) > 0:
		title = "邮件 · " + it.Title
		ref = fmt.Sprintf("邮箱账号 %s，文件夹 %s，最新一封 uid %d（mail_* 工具用）\n", it.Mail.AccountID, it.Mail.Mailbox, it.Mail.UIDs[len(it.Mail.UIDs)-1])
	}
	return fmt.Sprintf(`这是收件箱里的一件事，你作为%[1]s的 AI 分身先处理它，最后起草一条回复，%[1]s在手机上看过、点一下就会发出去。

<jarvis-context kind="context" title="%[2]s">
%[3]s%[4]s
</jarvis-context>

要做的：%[5]s

- 先用工具把需要的信息查清楚（lark_messages 读更早的上下文、lark_search / lark_api 查文档和日程、agents_list / agent_read 看各台电脑上的 Agent、
  读代码、mail_* 读邮件、上网）。只做只读的调查，不改任何东西。
- **不要自己发送**（不调用 lark_send、mail_send、agent_send），也不要用 ask_user / notify_user 打扰%[1]s。
- 最后**必须**调用 inbox_draft 提交：text 是以%[1]s口吻写的回复正文（第一人称；飞书口语、短，不要客套；邮件按来信的语言和正式程度）；
  查不到或要%[1]s本人定的地方用【】标出来，不要编；note 是一两句你查到了什么、依据在哪，给%[1]s核对用。
- 提交以后就结束，不用再总结。`, owner, xmlAttr(title), triageInput(it), "\n"+ref, it.Agent.Task)
}

func xmlAttr(s string) string {
	return strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func (h *Hub) ownerName() string {
	if o := strings.TrimSpace(h.cfg.Assistant.Owner); o != "" {
		return o
	}
	return "用户"
}

// inboxItemOfAgent is the item whose agent runs in conversation sessionID.
func (b *inbox) itemOfAgent(sessionID string) *InboxItem {
	if sessionID == "" {
		return nil
	}
	for _, it := range b.items {
		if it.Agent != nil && it.Agent.SessionID == sessionID {
			return it
		}
	}
	return nil
}

// isInboxAgent: the conversation is an inbox agent's (no "finished" push for it: the draft in the inbox is the result).
func (h *Hub) isInboxAgent(sessionID string) bool {
	h.inbox.mu.Lock()
	defer h.inbox.mu.Unlock()
	return h.inbox.itemOfAgent(sessionID) != nil
}

var inboxDraftTool = proto.Tool{Name: "inbox_draft", Description: "Submit the reply you drafted for the inbox item this conversation " +
	"was started for. It waits in the user's inbox; they read it and send it with one tap (you never send it yourself). `text` is the " +
	"reply in the user's voice; `note` is one or two sentences on what you found and where, for the user to check the draft against. " +
	"Call it once, at the end.",
	InputSchema: schema(`{"type":"object","required":["text"],"properties":{"text":{"type":"string"},"note":{"type":"string"}}}`)}

// toolInboxDraft stores the agent's draft on its item.
func (h *Hub) toolInboxDraft(ctx context.Context, sessionID string, args json.RawMessage) (string, error) {
	var a struct{ Text, Note string }
	if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.Text) == "" {
		return "", errors.New("text is required")
	}
	h.inbox.mu.Lock()
	it := h.inbox.itemOfAgent(sessionID)
	if it == nil {
		h.inbox.mu.Unlock()
		return "", errors.New("this conversation was not started for an inbox item")
	}
	now := time.Now().UTC()
	it.Agent.State, it.Agent.Error = AgentDrafted, ""
	it.Agent.Draft, it.Agent.Note, it.Agent.DraftedAt = strings.TrimSpace(a.Text), clip(strings.TrimSpace(a.Note), 600), &now
	h.inbox.mu.Unlock()
	h.saveInbox(ctx, []*InboxItem{it})
	return "The draft is in the user's inbox; they will send it. You're done.", nil
}
