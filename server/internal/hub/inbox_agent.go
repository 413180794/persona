package hub

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
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
	Changes []DocChange `json:"changes,omitempty"`
	Error   string      `json:"error,omitempty"`
	Of      string      `json:"of,omitempty"` // the item's Last the work answers
	// TodoID: the user's task the item is about (model 2's task_id, docs/unified-entry.md): the draft tells its
	// progress, and the task's conversation gets a card for it.
	TodoID    string     `json:"todo_id,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	DraftedAt *time.Time `json:"drafted_at,omitempty"`
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
	ctx = context.WithoutCancel(ctx) // like triageInbox: the pass outlives the caller
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
	tasks := h.inboxTasks() // before the inbox lock: it reads the todos and the machines' sessions
	h.inbox.mu.Lock()
	last, input := it.Last, prompts.RenderInboxJudgeInput(prompts.InboxJudgeInput{Summary: it.Summary, Item: promptItem(it), Tasks: tasks})
	h.inbox.mu.Unlock()
	system := prompts.RenderInboxJudge(prompts.InboxJudge{Owner: h.ownerName(), Rules: h.inboxAgentRules(ctx)})
	var out struct {
		Handle bool   `json:"handle"`
		Task   string `json:"task"`
		Reason string `json:"reason"`
		TaskID string `json:"task_id"`
	}
	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	err := h.judgeModel().JSON(cctx, system, input, &out)
	cancel()
	if err != nil {
		if ctx.Err() == nil && !ai.IsPaused(err) {
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
		for _, t := range tasks { // only a task it was shown
			if t.ID == strings.TrimSpace(out.TaskID) {
				it.Agent.TodoID = t.ID
			}
		}
	} else {
		it.Agent = &InboxAgent{State: AgentSkipped, Reason: clip(oneLine(out.Reason), 30), Of: last}
	}
	return true
}

// startInboxAgents starts queued items, newest first, while fewer than the configured number are running.
func (h *Hub) startInboxAgents(ctx context.Context, cfg InboxAgentConfig) {
	linked := map[string]*prompts.InboxTask{} // read before the inbox lock (todos and sessions)
	for _, t := range h.inboxTasks() {
		linked[t.ID] = &t
	}
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
		jobs = append(jobs, job{it, inboxAgentPromptFor(it, h.ownerName(), h.kbContextFor(it), linked[it.Agent.TodoID], max(h.inbox.larkAccount, 1))})
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

// startInboxAgent starts a headless conversation like the assistant's (same directory, so the same tools) on the
// inbox agent's model (模型配置 › inbox.agent): the assistant starts it (assistant.start), untitled — the draft in
// the inbox is its result.
func (h *Hub) startInboxAgent(ctx context.Context, cfg InboxAgentConfig, prompt string) (machineID, sessionID string, err error) {
	machine := h.assistantProfile().MachineID
	if h.localMachine(cfg.Machine) != nil {
		machine = cfg.Machine
	}
	if h.localMachine(machine) == nil {
		return "", "", errors.New("no machine for the inbox agent")
	}
	s, err := h.asst().Start(ctx, assistantwire.StartParams{Text: prompt, Purpose: "inbox.agent", MachineID: machine, Untitled: true})
	if err != nil {
		return "", "", err
	}
	return s.MachineID, s.SessionID, nil
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
		} else if m := h.localMachine(a.MachineID); m != nil && a.StartedAt != nil && now.Sub(*a.StartedAt) > time.Minute {
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
	return inboxAgentPromptFor(it, owner, kbc, nil, 1)
}

// inboxAgentPromptFor is inboxAgentPrompt for an item about one of the user's tasks (linked: its progress).
// larkDefault is lark.account: the Feishu account lark_messages reads unless the prompt names another.
func inboxAgentPromptFor(it *InboxItem, owner string, kbc *prompts.KBContext, linked *prompts.InboxTask, larkDefault int) string {
	a := prompts.InboxAgent{Owner: owner, Title: it.Title, Item: promptItem(it), Task: it.Agent.Task, KB: kbc, Linked: linked}
	switch {
	case it.Lark != nil:
		a.Lark = &prompts.InboxAgentLark{FeedID: it.Lark.FeedID}
		if account := larkItemAccount(it); account != larkDefault {
			a.Lark.Account = account
		}
	case it.Doc != nil:
		a.Doc = &prompts.InboxAgentDoc{URL: it.Doc.URL, Rows: len(it.Doc.Rows) > 0}
	case it.Moment != nil:
		a.Moment = &prompts.InboxAgentMoment{PostID: it.Moment.PostID}
	case it.Connector != nil:
		a.Connector = &prompts.InboxAgentConnector{Source: it.Source, URL: it.Connector.URL, ReplyTo: it.Connector.ReplyTo}
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

// inboxDraftWait bounds how long a draft waits for its agent's start to record the conversation (agentStarting).
var inboxDraftWait = 15 * time.Second

// agentStarting: an agent is being started and does not know its conversation yet. Callers hold b.mu.
func (b *inbox) agentStarting() bool {
	for _, it := range b.items {
		if it.Agent != nil && it.Agent.State == AgentRunning && it.Agent.SessionID == "" {
			return true
		}
	}
	return false
}

// isInboxAgent: the conversation is an inbox agent's (no "finished" push for it: the draft in the inbox is the result).
func (h *Hub) isInboxAgent(sessionID string) bool {
	h.inbox.mu.Lock()
	defer h.inbox.mu.Unlock()
	return h.inbox.itemOfAgent(sessionID) != nil
}

// inboxDraftCall is the inbox's inbox.draft for the assistant (its inbox_draft tool, whose rules are the assistant's):
// {session_id, text, note, changes} → {item_id, refused?}.
func (h *Hub) inboxDraftCall(ctx context.Context, params json.RawMessage) (any, error) {
	var a struct {
		SessionID string      `json:"session_id"`
		Text      string      `json:"text"`
		Note      string      `json:"note"`
		Changes   []DocChange `json:"changes"`
	}
	if err := json.Unmarshal(params, &a); err != nil {
		return nil, err
	}
	id, refused := h.inboxDraft(ctx, a.SessionID, a.Text, a.Note, a.Changes)
	out := map[string]any{"item_id": id}
	if refused != nil {
		out["refused"] = refused
	}
	return out, nil
}

// inboxDraft keeps the draft of the agent in conversation sessionID on its item, with the moves of a document's rows
// it proposes, and tells the assistant the item's message card. It is the item's id; "" when no item's agent runs
// there. A change the item does not offer (a row it lists, one of the column's values) is refused, and nothing kept.
func (h *Hub) inboxDraft(ctx context.Context, sessionID, text, note string, proposed []DocChange) (string, *DocChange) {
	h.inbox.mu.Lock()
	it := h.inbox.itemOfAgent(sessionID)
	// An agent that answers before its start returned (an agent runtime that is already running starts a turn at
	// once): its item learns the conversation's id a moment later.
	for wait := time.Now().Add(inboxDraftWait); it == nil && h.inbox.agentStarting() && time.Now().Before(wait); {
		h.inbox.mu.Unlock()
		time.Sleep(100 * time.Millisecond)
		h.inbox.mu.Lock()
		it = h.inbox.itemOfAgent(sessionID)
	}
	if it == nil {
		h.inbox.mu.Unlock()
		return "", nil
	}
	var changes []DocChange
	for _, ch := range proposed {
		if it.Doc == nil || !docOffers(*it.Doc, ch) {
			h.inbox.mu.Unlock()
			return it.ID, &ch
		}
		changes = append(changes, ch)
	}
	now := time.Now().UTC()
	it.Agent.State, it.Agent.Error, it.Agent.Changes = AgentDrafted, "", changes
	it.Agent.Draft, it.Agent.Note, it.Agent.DraftedAt = text, note, &now
	card := messageCardOf(it)
	if it.DoneBy == "replied" { // the user answered it while the agent worked: no 发送 for a reply already out
		card = nil
	}
	h.inbox.mu.Unlock()
	h.saveInbox(ctx, []*InboxItem{it})
	if card != nil {
		h.asst().InboxMessage(ctx, assistantwire.InboxMessage{ItemID: it.ID, Message: *card})
	}
	return it.ID, nil
}
