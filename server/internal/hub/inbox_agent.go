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
	"github.com/chyroc/jarvis/server/internal/prompts"
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
	State     string `json:"state"`
	Task      string `json:"task,omitempty"`   // model 2's instruction to the agent
	Reason    string `json:"reason,omitempty"` // model 2's why, short
	MachineID string `json:"machine_id,omitempty"`
	SessionID string `json:"session_id,omitempty"` // the agent's conversation (对话 tab): how it got there
	Draft     string `json:"draft,omitempty"`
	Note      string `json:"note,omitempty"` // what the agent found, for the user to check the draft against
	// Changes are the status moves the agent proposes with its draft (document items); 发送 makes them.
	Changes   []DocChange `json:"changes,omitempty"`
	Error     string      `json:"error,omitempty"`
	Of        string      `json:"of,omitempty"` // the item's Last the work answers
	StartedAt *time.Time  `json:"started_at,omitempty"`
	DraftedAt *time.Time  `json:"drafted_at,omitempty"`
}

func (h *Hub) judgeModel() *ai.Client {
	return h.purposeModel("inbox.judge")
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

// judgeOne asks model 2 about one item. A model failure leaves it unjudged, so the next pass tries again.
func (h *Hub) judgeOne(ctx context.Context, it *InboxItem) bool {
	h.inbox.mu.Lock()
	last, input := it.Last, prompts.RenderInboxJudgeInput(prompts.InboxJudgeInput{Summary: it.Summary, Item: promptItem(it)})
	h.inbox.mu.Unlock()
	system := prompts.RenderInboxJudge(prompts.InboxJudge{Owner: h.ownerName(), Rules: h.inboxAgentRules(ctx)})
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
		jobs = append(jobs, job{it, inboxAgentPrompt(it, h.ownerName(), h.kbContextFor(it))})
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
	params := map[string]any{"cwd": prep.Cwd, "text": prompt}
	if err := h.applyModel(ctx, "inbox.agent", params); err != nil {
		return "", "", err
	}
	if err := m.call(ctx, "assistant.send", params, &res); err != nil {
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

// inboxAgentPrompt is the AI 分身's first message for an item (prompts/inbox/agent.md).
func inboxAgentPrompt(it *InboxItem, owner string, kbc *prompts.KBContext) string {
	a := prompts.InboxAgent{Owner: owner, Title: it.Title, Item: promptItem(it), Task: it.Agent.Task, KB: kbc}
	switch {
	case it.Lark != nil:
		a.Lark = &prompts.InboxAgentLark{FeedID: it.Lark.FeedID}
	case it.Doc != nil:
		a.Doc = &prompts.InboxAgentDoc{URL: it.Doc.URL, Rows: len(it.Doc.Rows) > 0}
	case it.Moment != nil:
		a.Moment = &prompts.InboxAgentMoment{PostID: it.Moment.PostID}
	case it.Mail != nil && len(it.Mail.UIDs) > 0:
		a.Mail = &prompts.InboxAgentMail{Account: it.Mail.AccountID, Mailbox: it.Mail.Mailbox, UID: it.Mail.UIDs[len(it.Mail.UIDs)-1]}
	}
	return prompts.RenderInboxAgent(a)
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

// toolInboxDraft stores the agent's draft on its item.
func (h *Hub) toolInboxDraft(ctx context.Context, sessionID string, args json.RawMessage) (string, error) {
	var a struct {
		Text, Note string
		Changes    []DocChange
	}
	if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.Text) == "" {
		return "", errors.New("text is required")
	}
	h.inbox.mu.Lock()
	it := h.inbox.itemOfAgent(sessionID)
	if it == nil {
		h.inbox.mu.Unlock()
		return "", errors.New("this conversation was not started for an inbox item")
	}
	// Only moves the item offers (a row it lists, one of the column's values).
	var changes []DocChange
	for _, ch := range a.Changes {
		if it.Doc != nil && docOffers(*it.Doc, ch) {
			changes = append(changes, ch)
		} else {
			h.inbox.mu.Unlock()
			return "", fmt.Errorf("change %+v is not one the item lists (ref, field and one of its values)", ch)
		}
	}
	now := time.Now().UTC()
	it.Agent.State, it.Agent.Error, it.Agent.Changes = AgentDrafted, "", changes
	it.Agent.Draft, it.Agent.Note, it.Agent.DraftedAt = strings.TrimSpace(a.Text), clip(strings.TrimSpace(a.Note), 600), &now
	h.inbox.mu.Unlock()
	h.saveInbox(ctx, []*InboxItem{it})
	return "The draft is in the user's inbox; they will send it. You're done.", nil
}
