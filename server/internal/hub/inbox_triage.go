package hub

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/mail"
	"github.com/nwverse/persona/server/internal/prompts"
)

// InboxConfig tunes the inbox (hub.json "inbox").
type InboxConfig struct {
	// Rules is extra guidance for triage, in the user's words ("anything from my manager needs me", "alerts from
	// group X are noise"…), appended to the prompt.
	Rules string `json:"rules,omitempty"`
	// TriageAI (model 1) sorts and summarizes new items; JudgeAI (model 2) decides which ones the agent handles.
	// Empty fields fall back to hub.json's "ai"; without them both use "ai".
	TriageAI *ai.Config `json:"triage_ai,omitempty"`
	JudgeAI  *ai.Config `json:"judge_ai,omitempty"`
	// Agent (model 3) works on the items the judge hands over and drafts the reply (inbox_agent.go).
	Agent InboxAgentConfig `json:"agent,omitempty"`
}

func (h *Hub) triageModel() *ai.Client {
	return h.purposeModel("inbox.triage")
}

const triageConcurrency = 4

// triageInbox sorts open items whose content changed since they were last looked at into 需要你 / 知悉即可 / 噪音
// with a one-line summary (the hub's small-task model, ai.go). It runs in the background; one pass at a time.
func (h *Hub) triageInbox(ctx context.Context) {
	if h.triageModel() == nil || !h.inbox.triaging.TryLock() {
		return
	}
	h.inbox.mu.Lock()
	var todo []*InboxItem
	for _, it := range h.inbox.items {
		if it.open() && it.TriagedAt != it.Last && it.Context != "" {
			todo = append(todo, it)
		}
	}
	h.inbox.mu.Unlock()
	if len(todo) == 0 {
		h.inbox.triaging.Unlock()
		h.handInbox(ctx)
		return
	}
	// The pass outlives the caller: a pull to refresh answers before it is done.
	ctx = context.WithoutCancel(ctx)
	go func() {
		defer h.inbox.triaging.Unlock()
		sem := make(chan struct{}, triageConcurrency)
		var wg sync.WaitGroup
		for _, it := range todo {
			wg.Add(1)
			sem <- struct{}{}
			go func(it *InboxItem) {
				defer wg.Done()
				defer func() { <-sem }()
				if h.triageOne(ctx, it) {
					h.saveInbox(ctx, []*InboxItem{it})
				}
			}(it)
		}
		wg.Wait()
		h.handInbox(ctx)
	}()
}

// triageOne asks the model about an item in the list whose content changed; when the model fails the item gets its
// fallback tier (and no summary). When its connection is held the item stays untriaged. An item without a fallback
// (a connector's thing its rules leave to the model) keeps what it shows until a pass gets an answer.
func (h *Hub) triageOne(ctx context.Context, it *InboxItem) bool {
	last, v := h.askTriage(ctx, it, 90*time.Second)
	if ctx.Err() != nil {
		return false
	}
	h.inbox.mu.Lock()
	defer h.inbox.mu.Unlock()
	if it.Last != last { // new messages arrived meanwhile: the next pass sees them
		return false
	}
	fallback := ruleTier(it)
	switch {
	case v.state == triagePaused:
		// The model's connection is held (quota used up, busy): not triaged, so a pass after it is back asks again.
		// Meanwhile an untriaged item sits in its rule tier rather than in 需要你.
		if it.Tier == "" && fallback != "" {
			it.Tier, it.Reason = fallback, "按规则"
			return true
		}
		return false
	case v.state == "":
		it.Tier, it.Summary, it.Reason = v.tier, v.summary, v.reason
	case fallback == "":
		return false
	default:
		if v.err != nil {
			h.logf("inbox: triage %s: %v", it.ID, v.err)
		}
		it.Tier, it.Reason = fallback, "按规则"
	}
	it.TriagedAt = last
	// Triage can settle an item read elsewhere while it was still untriaged.
	if it.Read && it.Tier != TierAction {
		it.done(time.Now(), "read")
	}
	return true
}

// Why the model gave no tier (a triage verdict's state, GET /api/v1/inbox triage.state), the most telling first.
const (
	triageUnconfigured = "unconfigured" // no model is set up for inbox.triage
	triagePaused       = "paused"       // its connection is held: quota used up, or busy
	triageFailed       = "error"        // it failed or answered without a tier
	triageTimeout      = "timeout"      // too slow
	triageBehind       = "behind"       // not asked: the round had more new items than it triages (admitInbox)
)

var triageStates = []string{triageUnconfigured, triagePaused, triageFailed, triageTimeout, triageBehind}

// triageVerdict is the model's answer about one item, or (state) why there is none.
type triageVerdict struct {
	tier, summary, reason string
	state                 string // "" when answered
	why                   string // the state for people: 模型没有额度
	err                   error
}

// askTriage asks the model about one item, under the item's own rules when its source sent some (they replace
// inbox.rules; the default criteria stay underneath). last is the Last it asked about.
func (h *Hub) askTriage(ctx context.Context, it *InboxItem, timeout time.Duration) (last string, v triageVerdict) {
	h.inbox.mu.Lock()
	last, input, rules := it.Last, triageInput(it), strings.TrimSpace(it.Rules)
	h.inbox.mu.Unlock()
	if rules == "" {
		rules = h.inboxRules(ctx)
	}
	system := prompts.RenderInboxTriage(prompts.InboxTriage{Owner: h.cfg.Assistant.Owner, Rules: rules})
	var out struct {
		Tier    string `json:"tier"`
		Summary string `json:"summary"`
		Reason  string `json:"reason"`
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	err := h.triageModel().JSON(cctx, system, input, &out)
	slow := errors.Is(cctx.Err(), context.DeadlineExceeded) && ctx.Err() == nil
	cancel()
	var paused *ai.PausedError
	switch {
	case errors.As(err, &paused):
		v.state, v.why = triagePaused, "模型并发已满"
		if paused.Reason == ai.ReasonQuota {
			v.why = "模型没有额度"
		}
	case out.Tier == TierAction || out.Tier == TierFYI || out.Tier == TierNoise:
		v.tier, v.summary, v.reason = out.Tier, clip(oneLine(out.Summary), 80), clip(oneLine(out.Reason), 30)
	case ai.IsNotConfigured(err):
		v.state, v.why = triageUnconfigured, "没有配置分拣模型"
	case slow:
		v.state, v.why = triageTimeout, "模型太慢"
	default:
		v.state, v.why = triageFailed, "模型出错"
	}
	v.err = err
	return last, v
}

// ruleTier is the fallback tier, without a model: people talking to me need me, groups are FYI, bots and lists are
// noise; a connector's item has the tier its rules gave, else noise; a plugin's connector's none ("": only the model
// sorts it, inbox_admit.go).
func ruleTier(it *InboxItem) string {
	if it.Connector != nil {
		switch {
		case it.Connector.RuleTier != "":
			return it.Connector.RuleTier
		case fromExtension(it.Source):
			return ""
		}
		return TierNoise
	}
	switch it.Kind {
	case KindMention:
		// An @所有人 without an @me is an announcement: worth knowing, not waiting on me.
		if it.Lark != nil && it.Lark.AtAll && !it.Lark.AtMe {
			return TierFYI
		}
		return TierAction
	case KindDM, KindDoc:
		return TierAction
	case KindMoment: // an @ asks for me; a comment or a reply is mostly chatter
		if it.Moment != nil && strings.HasPrefix(it.Moment.Notice, "atIn") {
			return TierAction
		}
		return TierFYI
	case KindGroup:
		return TierFYI
	case KindMail:
		if it.Mail != nil && it.Mail.Direct {
			return TierFYI
		}
	}
	return TierNoise
}

// triageInput is the item as the models read it (prompts/inbox/item.md).
func triageInput(it *InboxItem) string { return prompts.RenderInboxItem(promptItem(it)) }

func promptItem(it *InboxItem) prompts.InboxItem {
	p := prompts.InboxItem{Kind: it.Kind, MailDirect: it.Mail != nil && it.Mail.Direct, Context: it.Context}
	if it.Connector != nil {
		p.Source = it.Source
		if d, ok := extensionKinds.Load(it.Source + "/" + it.Kind); ok {
			p.Described = d.(string)
		}
	}
	return p
}

// draftReply writes a reply to an inbox item in the user's voice (optionally following their instruction).
func (h *Hub) draftReply(ctx context.Context, id, instruction string) (string, error) {
	h.inbox.mu.Lock()
	it, ok := h.inbox.items[id]
	var item prompts.InboxItem
	if ok {
		item = promptItem(it)
	}
	h.inbox.mu.Unlock()
	if !ok {
		return "", errNoInboxItem
	}
	system := prompts.RenderInboxDraft(prompts.InboxDraft{Owner: h.cfg.Assistant.Owner, Notes: strings.TrimSpace(h.cfg.Assistant.Notes)})
	input := prompts.RenderInboxDraftInput(prompts.InboxDraftInput{Item: item, Instruction: strings.TrimSpace(instruction)})
	text, err := h.purposeModel("inbox.draft").Complete(ctx, system, input, 1024)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(text), nil
}

// inboxReply serializes the Feishu replies to one inbox item and remembers the last one that went out.
type inboxReply struct {
	mu       sync.Mutex
	clientID string
}

// replyInbox sends text as the user's reply through the connector the item belongs to — in the Feishu chat, a
// document's comments, a 公司圈 post, a reply to the newest mail of the thread, a GitHub comment… — and files the item
// as done.
func (h *Hub) replyInbox(ctx context.Context, id, text string, changes ...DocChange) (InboxItem, error) {
	return h.replyInboxAs(ctx, id, text, "", changes...)
}

// replyInboxAs is replyInbox with the app's own id for the send (client_id): a retry of the same request is sent once.
// Empty: one derived from the item and the text.
func (h *Hub) replyInboxAs(ctx context.Context, id, text, clientID string, changes ...DocChange) (InboxItem, error) {
	h.inbox.mu.Lock()
	it, ok := h.inbox.items[id]
	var c InboxItem
	if ok {
		c = *it
		if it.Lark != nil { // collection rewrites these fields while the reply is out
			l := *it.Lark
			c.Lark = &l
		}
		if it.Mail != nil {
			m := *it.Mail
			c.Mail = &m
		}
		if it.Doc != nil {
			d := *it.Doc
			c.Doc = &d
		}
		if it.Moment != nil {
			m := *it.Moment
			c.Moment = &m
		}
		if it.Connector != nil {
			k := *it.Connector
			c.Connector = &k
		}
	}
	h.inbox.mu.Unlock()
	if !ok {
		return InboxItem{}, errNoInboxItem
	}
	// Phone and watch can both press send, and a timed-out request gets retried: one reply of an item at a time,
	// held until the item is filed (the connector's check for "already sent" relies on it).
	r, _ := h.inbox.replies.LoadOrStore(id, &inboxReply{})
	once := r.(*inboxReply)
	once.mu.Lock()
	defer once.mu.Unlock()
	req := inboxReplyReq{id: id, live: it, item: c, text: text, clientID: clientID, changes: changes, once: once}
	var already bool
	var err error
	switch own := h.ownInboxFor(c); {
	case h.migratedConnector(&c) != nil: // a plugin took Feishu over (inbox_migrate.go)
		already, err = h.replyMigrated(ctx, h.migratedConnector(&c), req)
	case len(changes) > 0 && (c.Connector != nil || own == nil):
		return InboxItem{}, errors.New("only document items have a status to change")
	case c.Connector != nil:
		already, err = h.replyConnectorItem(ctx, req)
	case own != nil:
		already, err = own.replyInbox(ctx, req)
	default:
		return InboxItem{}, errCannotReply
	}
	if err != nil {
		return InboxItem{}, err
	}
	now := time.Now()
	if already {
		h.inbox.mu.Lock()
		out := it.listed()
		h.inbox.mu.Unlock()
		h.markCardsSent(ctx, id, now, text)
		return out, nil
	}
	h.inbox.mu.Lock()
	it.done(now, "replied")
	out := it.listed()
	h.inbox.mu.Unlock()
	h.saveInbox(ctx, []*InboxItem{it})
	// The item's cards in the assistant's conversations show it sent, wherever it was sent from.
	h.markCardsSent(ctx, id, now, text)
	return out, nil
}

// mailReply is a reply to orig: Re: subject, threading headers, the original quoted below.
func mailReply(accountID string, orig *mail.MailMessage, text string) mail.SendRequest {
	subject := orig.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	refs := append(append([]string{}, orig.References...), orig.MessageID)
	var quoted []string
	for i, l := range strings.Split(orig.Text, "\n") {
		if i >= 40 {
			break
		}
		quoted = append(quoted, "> "+l)
	}
	from := orig.From.Name
	if from == "" {
		from = orig.From.Email
	}
	body := text + "\n\n" + orig.Date + "，" + from + " 写道：\n" + strings.Join(quoted, "\n")
	return mail.SendRequest{AccountID: accountID, To: []mail.Address{orig.From}, Subject: subject, Text: body,
		InReplyTo: orig.MessageID, References: mail.MessageIDs(strings.Join(refs, " "))}
}
