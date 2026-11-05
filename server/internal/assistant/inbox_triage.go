package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/rpc"
)

const triageConcurrency = 4

// triageInbox sorts open items whose content changed since they were last looked at into 需要你 / 知悉即可 / 噪音
// with a one-line summary (the small-task model, inbox.triage). It runs in the background; one pass at a time.
func (s *Service) triageInbox(ctx context.Context) {
	b := &s.inbox
	if !b.triaging.TryLock() {
		return
	}
	b.mu.Lock()
	var todo []*InboxItem
	for _, it := range b.items {
		if it.open() && it.TriagedAt != it.Last && it.Context != "" {
			todo = append(todo, it)
		}
	}
	b.mu.Unlock()
	if len(todo) == 0 {
		b.triaging.Unlock()
		s.handInbox(ctx)
		return
	}
	// The pass outlives the caller: a pull to refresh answers before it is done.
	ctx = context.WithoutCancel(ctx)
	go func() {
		defer b.triaging.Unlock()
		sem := make(chan struct{}, triageConcurrency)
		var wg sync.WaitGroup
		for _, it := range todo {
			wg.Add(1)
			sem <- struct{}{}
			go func(it *InboxItem) {
				defer wg.Done()
				defer func() { <-sem }()
				if s.triageOne(ctx, it) {
					s.saveInbox(ctx, []*InboxItem{it})
				}
			}(it)
		}
		wg.Wait()
		s.handInbox(ctx)
	}()
}

// triageOne asks the model about an item in the list whose content changed; when the model fails the item gets its
// fallback tier (and no summary). When its connection is held the item stays untriaged. An item without a fallback
// (a connector's thing its rules leave to the model) keeps what it shows until a pass gets an answer.
func (s *Service) triageOne(ctx context.Context, it *InboxItem) bool {
	last, v := s.askTriage(ctx, it, 90*time.Second)
	if ctx.Err() != nil {
		return false
	}
	b := &s.inbox
	b.mu.Lock()
	defer b.mu.Unlock()
	if it.Last != last { // new messages arrived meanwhile: the next pass sees them
		return false
	}
	fallback := b.ruleTier(it)
	switch {
	case v.state == triagePaused:
		// The model's connection is held (quota used up, busy): not triaged, so a pass after it is back asks again.
		// Meanwhile an untriaged item sits in its rule tier rather than in 需要你.
		if it.Tier == "" && fallback != "" {
			it.Tier, it.Reason = fallback, b.byRules()
			return true
		}
		return false
	case v.state == "":
		it.Tier, it.Summary, it.Reason = v.tier, v.summary, v.reason
	case fallback == "":
		return false
	default:
		if v.err != nil {
			s.logf("inbox: triage %s: %v", it.ID, v.err)
		}
		it.Tier, it.Reason = fallback, b.byRules()
	}
	it.TriagedAt = last
	// Triage can settle an item read elsewhere while it was still untriaged (not one filed while the model thought).
	if it.Read && it.Tier != TierAction && it.open() {
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
func (s *Service) askTriage(ctx context.Context, it *InboxItem, timeout time.Duration) (last string, v triageVerdict) {
	b := &s.inbox
	b.mu.Lock()
	last, input, rules := it.Last, b.triageInput(it), strings.TrimSpace(it.Rules)
	b.mu.Unlock()
	if rules == "" {
		rules = s.inboxConfig(ctx).Rules
	}
	hello := s.hello(ctx)
	system := prompts.RenderInboxTriage(prompts.InboxTriage{Owner: hello.Owner, Rules: rules, Language: answerLanguage(hello.Locale)})
	var out struct {
		Tier    string `json:"tier"`
		Summary string `json:"summary"`
		Reason  string `json:"reason"`
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	err := s.inboxModelJSON(cctx, purposeTriage, system, input, 0, &out)
	slow := errors.Is(cctx.Err(), context.DeadlineExceeded) && ctx.Err() == nil
	cancel()
	paused, quota := modelPaused(err)
	l := s.lang(ctx)
	switch {
	case paused:
		v.state, v.why = triagePaused, i18n.T(l, "assistant-inbox_triage.busy")
		if quota {
			v.why = i18n.T(l, "assistant-inbox_triage.no_quota")
		}
	case out.Tier == TierAction || out.Tier == TierFYI || out.Tier == TierNoise:
		v.tier, v.summary, v.reason = out.Tier, clip(oneLine(out.Summary), 80), clip(oneLine(out.Reason), 30)
	case modelUnconfigured(err):
		v.state, v.why = triageUnconfigured, i18n.T(l, "assistant-inbox_triage.unconfigured")
	case slow:
		v.state, v.why = triageTimeout, i18n.T(l, "assistant-inbox_triage.slow")
	default:
		v.state, v.why = triageFailed, i18n.T(l, "assistant-inbox_triage.failed")
	}
	v.err = err
	return last, v
}

// ruleTier is the fallback tier, without a model: people talking to me need me, groups are FYI, bots and lists are
// noise; a connector's item has the tier its rules gave, else noise; a plugin's connector's none ("": only the model
// sorts it, inbox_admit.go), nor one's whose source is not known yet to be built in (the manager just started).
func (b *inboxBook) ruleTier(it *InboxItem) string {
	if it.Connector != nil {
		switch {
		case it.Connector.RuleTier != "":
			return it.Connector.RuleTier
		case b.fromExtension(it.Source) || !b.builtinSource(it.Source):
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
func (b *inboxBook) triageInput(it *InboxItem) string {
	return prompts.RenderInboxItem(b.promptItem(it))
}

func (b *inboxBook) promptItem(it *InboxItem) prompts.InboxItem {
	p := prompts.InboxItem{Kind: it.Kind, MailDirect: it.Mail != nil && it.Mail.Direct, Context: it.Context}
	if it.Connector != nil {
		p.Source = it.Source
		p.Described = b.kindWords(it.Source, it.Kind)
	}
	return p
}

// draftModelWait is how long a draft may take (the app waits on it).
const draftModelWait = 2 * time.Minute

// draftReply writes a reply to an inbox item in the user's voice (optionally following their instruction).
func (s *Service) draftReply(ctx context.Context, id, instruction string) (string, error) {
	b := &s.inbox
	b.mu.Lock()
	it, ok := b.items[id]
	var item prompts.InboxItem
	if ok {
		item = b.promptItem(it)
	}
	b.mu.Unlock()
	if !ok {
		return "", errNoInboxItem
	}
	hello := s.hello(ctx)
	system := prompts.RenderInboxDraft(prompts.InboxDraft{Owner: hello.Owner, Notes: strings.TrimSpace(hello.OwnerNotes),
		Language: answerLanguage(hello.Locale)})
	input := prompts.RenderInboxDraftInput(prompts.InboxDraftInput{Item: item, Instruction: strings.TrimSpace(instruction)})
	cctx, cancel := context.WithTimeout(ctx, draftModelWait)
	defer cancel()
	text, err := s.inboxModelComplete(cctx, purposeDraft, system, input, 1024)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(text), nil
}

// inboxReply serializes the replies to one inbox item and remembers the last one that went out.
type inboxReply struct {
	mu       sync.Mutex
	clientID string
	// keptOpen: the last reply went out but the item stayed open, for what came into it while the reply was out.
	keptOpen bool
}

// repeated reports whether a reply with key went out last and still stands, so this one is a repeat (pressed on
// phone and watch, a timed-out request retried): the item filed as replied, or kept open by that reply. Without the
// second, a retry after new messages came in would go out again (#1828). Under b.mu, with r.mu held.
func (r *inboxReply) repeated(live *InboxItem, key string) bool {
	return r.clientID == key && (live.DoneBy == "replied" || r.keptOpen && live.open())
}

// inboxReplyReq is one reply: the item as it is in the inbox (live, under b.mu) and a copy taken for the reply, the
// text, the documents' status moves, and the item's replies (held for the whole reply).
type inboxReplyReq struct {
	id   string
	live *InboxItem
	item InboxItem
	text string
	// clientID: the app's id for the send (POST …/reply client_id); empty for one derived from the item and the text.
	clientID string
	changes  []DocChange
	once     *inboxReply
	// sendID names the send for the hub, which sends it once however often it is asked (InboxSent): a retry after
	// the answer was lost (the link dropping, the manager restarting) does not go out twice (replySendID).
	sendID string
}

// replySendID names one reply: the app's id for the send when it gave one, else the item, the text and the status
// moves (the same reply asked again is the same send). Not the message it answers: a retry after new messages came
// into the item (it stayed open, replyInboxAs) would go out again (#1828), as the derived Feishu clientId is.
func replySendID(id, text, clientID string, changes []DocChange) string {
	if validClientID(clientID) {
		return namedClientID("send\n" + id + "\n" + strings.ToLower(clientID))
	}
	moves, _ := json.Marshal(changes)
	return namedClientID("send\n" + id + "\n" + text + "\n" + string(moves))
}

// replyInDoubt: the reply may have gone out though no answer said so: the link to the hub dropped while it was out,
// the time ran out, or the hub says a send under its id was cut off before (assistantwire.CodeAssistantInDoubt).
func replyInDoubt(err error) bool {
	var re *rpc.Error
	return errors.Is(err, rpc.ErrDropped) || errors.Is(err, context.DeadlineExceeded) ||
		errors.As(err, &re) && re.Code == assistantwire.CodeAssistantInDoubt
}

// replyInbox sends text as the user's reply to the item's source — in the Feishu chat, a document's comments, a
// 公司圈 post (the plugin that took Feishu over), a reply to the newest mail of the thread, a GitHub comment… (the
// hub sends it) — and files the item as done, unless something new came into it while the reply was out.
func (s *Service) replyInbox(ctx context.Context, id, text string, changes ...DocChange) (InboxItem, error) {
	return s.replyInboxAs(ctx, id, text, "", changes...)
}

// replyInboxAs is replyInbox with the app's own id for the send (client_id): a retry of the same request is sent once.
// Empty: one derived from the item and the text.
func (s *Service) replyInboxAs(ctx context.Context, id, text, clientID string, changes ...DocChange) (InboxItem, error) {
	b := &s.inbox
	b.mu.Lock()
	it, ok := b.items[id]
	var c InboxItem
	if ok {
		c = *it
		if it.Lark != nil { // collection rewrites these fields while the reply is out
			l := *it.Lark
			c.Lark = &l
		}
		if it.Mail != nil {
			m := *it.Mail
			m.UIDs = append([]uint32(nil), it.Mail.UIDs...)
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
	b.mu.Unlock()
	if !ok {
		return InboxItem{}, errNoInboxItem
	}
	// Phone and watch can both press send, and a timed-out request gets retried: one reply of an item at a time,
	// held until the item is filed (the check for "already sent" relies on it).
	r, _ := b.replies.LoadOrStore(id, &inboxReply{})
	once := r.(*inboxReply)
	once.mu.Lock()
	defer once.mu.Unlock()
	req := inboxReplyReq{id: id, live: it, item: c, text: text, clientID: clientID, changes: changes, once: once,
		sendID: replySendID(id, text, clientID, changes)}
	var already bool
	var err error
	switch {
	case c.Connector == nil && migratedThread(&c) != "": // a plugin took Feishu over (inbox_migrate.go)
		already, err = s.replyMigrated(ctx, req)
	case len(changes) > 0:
		return InboxItem{}, errors.New("only document items have a status to change")
	case c.Connector != nil:
		already, err = s.replyConnectorItem(ctx, req)
	case c.Mail != nil:
		already, err = s.replyMail(ctx, req)
	default:
		return InboxItem{}, errCannotReply
	}
	if err != nil {
		return InboxItem{}, err
	}
	now := time.Now()
	if already {
		b.mu.Lock()
		out := it.listed()
		b.mu.Unlock()
		s.markCardsSent(ctx, id, now, text, true)
		return out, nil
	}
	b.mu.Lock()
	// The reply answers what the item held when it was sent. A move that came in meanwhile (collection runs while the
	// reply is out) is not answered: the item stays open with it, so it is not filed unseen (#1828). An item filed
	// meanwhile (read elsewhere) is filed as replied.
	kept := it.open() && it.Last != c.Last
	once.keptOpen = kept
	if !kept {
		it.done(now, "replied")
	}
	out := it.listed()
	b.mu.Unlock()
	if !kept {
		s.saveInbox(ctx, []*InboxItem{it})
	}
	// The item's cards in the assistant's conversations show it sent, wherever it was sent from.
	s.markCardsSent(ctx, id, now, text, true)
	return out, nil
}

// replyMail answers the newest mail of the thread from the account that received it (the hub sends it).
func (s *Service) replyMail(ctx context.Context, r inboxReplyReq) (bool, error) {
	m := r.item.Mail
	if m == nil || len(m.UIDs) == 0 {
		return false, errCannotReply
	}
	var sent assistantwire.InboxSent
	err := s.appCall(ctx, AppInbox, inboxReplyMail, map[string]any{"account_id": m.AccountID, "uid": m.UIDs[len(m.UIDs)-1],
		"mailbox": m.Mailbox, "text": r.text, "send_id": r.sendID}, &sent)
	s.inboxSent(sent, r.sendID)
	return false, err
}
