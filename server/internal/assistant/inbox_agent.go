package assistant

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/kb"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// The inbox's AI 分身 (docs/inbox.md): model 1 triages every new item (inbox_triage.go), model 2 decides which of
// the 需要你 items an agent should take on, and the agent (model 3, a headless Claude Code in the assistant's
// directory, with its tools: a conversation the assistant starts itself, untitled) looks into it and drafts the reply
// (inbox_draft). The draft waits in the inbox; nothing is sent until the user taps 发送 (POST /api/v1/inbox/{id}/reply).

// InboxAgentConfig is hub.json's inbox.agent (the hub hands it over in InboxConfig).
type InboxAgentConfig = assistantwire.InboxAgentConfig

func agentConcurrency(c InboxAgentConfig) int { return max(c.Concurrency, 1) }

func agentTimeout(c InboxAgentConfig) time.Duration {
	if c.TimeoutMinutes <= 0 {
		return 15 * time.Minute
	}
	return time.Duration(c.TimeoutMinutes) * time.Minute
}

func agentMaxAge(c InboxAgentConfig) time.Duration {
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

// inboxAgentStartWait bounds one assistant.send: the node builds the conversation before it answers, which can take
// a while on a waking machine, so a timeout leaves the result unknown (the session may exist on the node).
var inboxAgentStartWait = 110 * time.Second

// inboxAgentStartMaxAttempts bounds starts whose result was unknown: the first try plus two retries.
const inboxAgentStartMaxAttempts = 3

// inboxAgentStartBackoff is the wait before the second and third starts, indexed by attempts already made minus one.
// The waits are covered by the hub's inbox rounds (it starts queued agents on every round, a couple of minutes).
var inboxAgentStartBackoff = []time.Duration{time.Minute, 5 * time.Minute}

// errNoInboxMachine is the definite "no computer can run the inbox agent" refusal before any node call.
var errNoInboxMachine = errors.New("no machine for the inbox agent")

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
	// Attempts is how many starts were made; RetryAfter keeps a queued agent whose last start's result was unknown
	// out of the next rounds until the backoff passed.
	Attempts   int        `json:"attempts,omitempty"`
	RetryAfter *time.Time `json:"retry_after,omitempty"`
}

// startDue says a queued agent waits for no backoff (a fresh judge's agent has no RetryAfter).
func (a *InboxAgent) startDue(now time.Time) bool {
	return a.RetryAfter == nil || !a.RetryAfter.After(now)
}

// handInbox is one pass of models 2 and 3: settle agents that finished or hung, judge newly triaged 需要你 items,
// start agents for the queued ones. One pass at a time, in the background.
func (s *Service) handInbox(ctx context.Context) {
	cfg := s.inboxConfig(ctx).Agent
	b := &s.inbox
	if !cfg.Enabled || !b.handing.TryLock() {
		return
	}
	ctx = context.WithoutCancel(ctx) // like triageInbox: the pass outlives the caller
	go func() {
		defer b.handing.Unlock()
		s.sweepInboxAgents(ctx, cfg, time.Now())
		for _, it := range b.toJudge(time.Now().Add(-agentMaxAge(cfg))) {
			if s.judgeOne(ctx, it) {
				s.saveInbox(ctx, []*InboxItem{it})
			}
		}
		s.startInboxAgents(ctx, cfg)
	}()
}

// toJudge: open 需要你 items triaged on their current content and not judged on it yet, newest first.
func (b *inboxBook) toJudge(since time.Time) []*InboxItem {
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
func (s *Service) judgeOne(ctx context.Context, it *InboxItem) bool {
	b := &s.inbox
	tasks := s.activeTasks(ctx) // before the inbox lock: it reads the todos and the machines' sessions
	b.mu.Lock()
	last, input := it.Last, prompts.RenderInboxJudgeInput(prompts.InboxJudgeInput{Summary: it.Summary, Item: b.promptItem(it), Tasks: tasks})
	b.mu.Unlock()
	system := prompts.RenderInboxJudge(prompts.InboxJudge{Owner: s.owner(ctx), Rules: s.inboxConfig(ctx).AgentRules})
	var out struct {
		Handle bool   `json:"handle"`
		Task   string `json:"task"`
		Reason string `json:"reason"`
		TaskID string `json:"task_id"`
	}
	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	err := s.inboxModelJSON(cctx, purposeJudge, system, input, 0, &out)
	cancel()
	if err != nil {
		if paused, _ := modelPaused(err); ctx.Err() == nil && !paused {
			s.logf("inbox: judge %s: %v", it.ID, err)
		}
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
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
func (s *Service) startInboxAgents(ctx context.Context, cfg InboxAgentConfig) {
	b := &s.inbox
	linked := map[string]*prompts.InboxTask{} // read before the inbox lock (todos and sessions)
	for _, t := range s.activeTasks(ctx) {
		linked[t.ID] = &t
	}
	owner := s.owner(ctx)
	now := time.Now().UTC()
	b.mu.Lock()
	running := 0
	var queued []*InboxItem
	for _, it := range b.items {
		if it.Agent == nil {
			continue
		}
		switch {
		case it.Agent.State == AgentRunning:
			running++
		case it.Agent.State == AgentQueued && it.open() && it.Agent.startDue(now):
			queued = append(queued, it)
		}
	}
	sort.Slice(queued, func(i, j int) bool { return queued[i].UpdatedAt.After(queued[j].UpdatedAt) })
	queued = queued[:min(len(queued), max(agentConcurrency(cfg)-running, 0))]
	type job struct {
		it     *InboxItem
		chat   string
		prompt func(*prompts.KBContext) string
	}
	jobs := make([]job, 0, len(queued))
	larkDefault := max(b.larkAccount, 1)
	for _, it := range queued {
		it.Agent.State, it.Agent.StartedAt, it.Agent.Attempts, it.Agent.RetryAfter = AgentRunning, &now, it.Agent.Attempts+1, nil
		a := b.inboxAgentFor(it, owner, linked[it.Agent.TodoID], larkDefault)
		jobs = append(jobs, job{it, kbChatOf(it), func(kbc *prompts.KBContext) string { a.KB = kbc; return prompts.RenderInboxAgent(a) }})
	}
	b.mu.Unlock()
	for _, j := range jobs {
		var kbc *prompts.KBContext
		if j.chat != "" {
			kbc = s.kbChatContext(ctx, j.chat)
		}
		cctx, cancel := context.WithTimeout(ctx, inboxAgentStartWait)
		machineID, sessionID, err := s.startInboxAgent(cctx, cfg, j.it, j.prompt(kbc))
		cancel()
		b.mu.Lock()
		if a := j.it.Agent; a != nil && a.State == AgentRunning && a.SessionID == "" {
			if err != nil {
				// The item may have been replied to or filed while the start was in flight: it never queues again.
				s.requeueAgentStart(ctx, a, err, time.Now().UTC(), j.it.open())
				s.logf("inbox: agent for %s: %v", j.it.ID, err)
			} else {
				a.MachineID, a.SessionID = machineID, sessionID
			}
		}
		b.mu.Unlock()
		s.saveInbox(ctx, []*InboxItem{j.it})
	}
	if len(jobs) > 0 {
		s.publish(ctx) // the hub knows the agents' conversations (no finished push for them)
	}
}

// kbChatOf is the knowledge base's chat of an item: its Feishu chat, or the mail's correspondent; "" for none.
func kbChatOf(it *InboxItem) string {
	switch {
	case it.Lark != nil:
		return kb.ChatID("lark", it.Lark.FeedID)
	case it.Mail != nil:
		return kb.ChatID("mail", it.Mail.AccountID+"-"+strings.ToLower(it.Mail.From.Email))
	}
	return ""
}

// startInboxAgent starts a headless conversation like the assistant's (same directory, so the same tools) on the
// inbox agent's model (模型配置 › inbox.agent), untitled: the draft in the inbox is its result.
func (s *Service) startInboxAgent(ctx context.Context, cfg InboxAgentConfig, it *InboxItem, prompt string) (machineID, sessionID string, err error) {
	machine := s.settings(ctx).MachineID
	if _, ok := s.device(ctx, cfg.Machine); ok && cfg.Machine != "" {
		machine = cfg.Machine
	}
	if _, ok := s.device(ctx, machine); !ok || machine == "" {
		return "", "", errNoInboxMachine
	}
	sess, err := s.Start(ctx, StartParams{Text: prompt, Purpose: purposeAgent, MachineID: machine, Untitled: true, Origin: OriginInbox,
		OriginRef: it.ID})
	if err != nil {
		return "", "", err
	}
	// 「MMDD | 收件箱 | the item's title」 from the start, not its prompt's first line (#3264).
	s.titleFixed(sess.MachineID, sess.SessionID, time.Now(), titleInbox, it.Title)
	return sess.MachineID, sess.SessionID, nil
}

// definiteStartError says a start refusal means nothing began on the node and a retry cannot help: no computer
// (before the call or the hub's no_device answer), a computer still pending confirmation, Claude Code or the
// launcher missing, or a node that must be updated first. Anything else (a timeout, a dropped RPC, an offline or
// waking machine, an unknown error) leaves the result unknown and is retried.
func definiteStartError(err error) bool {
	if errors.Is(err, errNoInboxMachine) || errors.Is(err, ErrNoDevice) {
		return true
	}
	var re *rpc.Error
	if errors.As(err, &re) {
		switch re.Code {
		case proto.CodeNoMachine, proto.CodeMachinePending, proto.CodeClaudeMissing,
			proto.CodeLauncherMissing, proto.CodeNodeOutdated:
			return true
		}
	}
	return false
}

// requeueAgentStart settles an agent whose start answered without a conversation. A definite refusal fails it at
// once; an unknown result queues it again with backoff until the attempts are spent, then fails it. canRetry is
// false for an item settled (replied or filed) while the start was in flight: it must not come back queued. Either
// way the item keeps a localized plain sentence, never the error's own words. The caller holds the inbox lock.
func (s *Service) requeueAgentStart(ctx context.Context, a *InboxAgent, err error, now time.Time, canRetry bool) {
	definite := definiteStartError(err)
	a.MachineID, a.SessionID, a.StartedAt, a.Error = "", "", nil, ""
	if !definite && canRetry && a.Attempts < inboxAgentStartMaxAttempts {
		i := min(a.Attempts-1, len(inboxAgentStartBackoff)-1)
		at := now.Add(inboxAgentStartBackoff[i])
		a.State, a.RetryAfter = AgentQueued, &at
		return
	}
	a.RetryAfter = nil
	a.State = AgentFailed
	if !definite && canRetry { // retried until the attempts were spent
		a.Error = i18n.T(s.lang(ctx), "assistant-inbox_agent.start_gave_up")
	} else {
		a.Error = i18n.T(s.lang(ctx), "assistant-inbox_agent.start_failed")
	}
}

// sweepInboxAgents fails agents that ended their turn without a draft, or ran past the timeout.
func (s *Service) sweepInboxAgents(ctx context.Context, cfg InboxAgentConfig, now time.Time) {
	b := &s.inbox
	b.mu.Lock()
	var running []*InboxItem
	for _, it := range b.items {
		if it.Agent != nil && it.Agent.State == AgentRunning && it.Agent.SessionID != "" {
			running = append(running, it)
		}
	}
	b.mu.Unlock()
	var changed []*InboxItem
	for _, it := range running {
		b.mu.Lock()
		a := *it.Agent
		b.mu.Unlock()
		reason := ""
		if a.StartedAt != nil && now.Sub(*a.StartedAt) > agentTimeout(cfg) {
			reason = i18n.T(s.lang(ctx), "assistant-inbox_agent.timeout")
		} else if a.StartedAt != nil && now.Sub(*a.StartedAt) > time.Minute {
			if sess, ok := s.session(ctx, a.MachineID, a.SessionID); ok && sess.Status != proto.StatusWorking {
				reason = i18n.T(s.lang(ctx), "assistant-inbox_agent.no_draft")
			}
		}
		if reason == "" {
			continue
		}
		b.mu.Lock()
		if it.Agent != nil && it.Agent.State == AgentRunning && it.Agent.SessionID == a.SessionID {
			it.Agent.State, it.Agent.Error = AgentFailed, reason
			changed = append(changed, it)
		}
		b.mu.Unlock()
	}
	s.saveInbox(ctx, changed)
}

// inboxAgentPrompt is the AI 分身's first message for an item (prompts/inbox/agent.md).
func (b *inboxBook) inboxAgentPrompt(it *InboxItem, owner string, kbc *prompts.KBContext) string {
	return b.inboxAgentPromptFor(it, owner, kbc, nil, 1)
}

// inboxAgentPromptFor is inboxAgentPrompt for an item about one of the user's tasks (linked: its progress).
// larkDefault is lark.account: the Feishu account lark_messages reads unless the prompt names another.
func (b *inboxBook) inboxAgentPromptFor(it *InboxItem, owner string, kbc *prompts.KBContext, linked *prompts.InboxTask, larkDefault int) string {
	a := b.inboxAgentFor(it, owner, linked, larkDefault)
	a.KB = kbc
	return prompts.RenderInboxAgent(a)
}

// inboxAgentFor is what the AI 分身's first message is made of, but the knowledge base's part.
func (b *inboxBook) inboxAgentFor(it *InboxItem, owner string, linked *prompts.InboxTask, larkDefault int) prompts.InboxAgent {
	a := prompts.InboxAgent{Owner: owner, Title: it.Title, Item: b.promptItem(it), Task: it.Agent.Task, Linked: linked}
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
	return a
}

// itemOfAgent is the item whose agent runs in conversation sessionID. Under b.mu.
func (b *inboxBook) itemOfAgent(sessionID string) *InboxItem {
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
func (b *inboxBook) agentStarting() bool {
	for _, it := range b.items {
		if it.Agent != nil && it.Agent.State == AgentRunning && it.Agent.SessionID == "" {
			return true
		}
	}
	return false
}

// inboxDraft keeps the draft of the agent in conversation sessionID on its item, with the moves of a document's rows
// it proposes, and puts the item's message card in the task's conversation. It is the item's id; "" when no item's
// agent runs there. A change the item does not offer (a row it lists, one of the column's values) is refused, and
// nothing kept.
func (s *Service) inboxDraft(ctx context.Context, sessionID, text, note string, proposed []DocChange) (string, *DocChange) {
	if s.inboxReady(ctx) != nil {
		return "", nil
	}
	b := &s.inbox
	b.mu.Lock()
	it := b.itemOfAgent(sessionID)
	// An agent that answers before its start returned (an agent runtime that is already running starts a turn at
	// once): its item learns the conversation's id a moment later.
	for wait := time.Now().Add(inboxDraftWait); it == nil && b.agentStarting() && time.Now().Before(wait); {
		b.mu.Unlock()
		time.Sleep(100 * time.Millisecond)
		b.mu.Lock()
		it = b.itemOfAgent(sessionID)
	}
	if it == nil {
		b.mu.Unlock()
		return "", nil
	}
	var changes []DocChange
	for _, ch := range proposed {
		if it.Doc == nil || !docOffers(*it.Doc, ch) {
			b.mu.Unlock()
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
	b.mu.Unlock()
	s.saveInbox(ctx, []*InboxItem{it})
	if card != nil {
		s.messageCard(ctx, it.ID, *card)
	}
	return it.ID, nil
}

// messageCardOf is the card for an item whose draft is about a task; nil when it is about none. Under b.mu.
func messageCardOf(it *InboxItem) *MessageCard {
	if it.Agent == nil || it.Agent.TodoID == "" || it.Agent.Draft == "" {
		return nil
	}
	return &MessageCard{TodoID: it.Agent.TodoID, Source: it.Source, Account: it.AccountName, Chat: it.Title, Sender: it.Sender,
		Text: clip(strings.TrimSpace(it.Preview), 400), Draft: it.Agent.Draft}
}

// inboxRepliedAt is when those of the inbox items ids the user replied to were replied to (the message cards say
// sent by it).
func (s *Service) inboxRepliedAt(ctx context.Context, ids []string) map[string]time.Time {
	b := &s.inbox
	out := map[string]time.Time{}
	if s.inboxReady(ctx) != nil {
		return out
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, id := range ids {
		if it := b.items[id]; it != nil && it.DoneBy == "replied" && it.DoneAt != nil {
			out[id] = *it.DoneAt
		}
	}
	return out
}

// ActiveTasks are the tasks going on, as the inbox's judge and AI 分身 link messages to them.
func (s *Service) ActiveTasks(ctx context.Context) []prompts.InboxTask { return s.activeTasks(ctx) }
