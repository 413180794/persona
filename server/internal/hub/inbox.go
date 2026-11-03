package hub

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/lark"
	"github.com/chyroc/jarvis/server/internal/mail"
)

// The unified inbox (docs/inbox.md): Feishu messages and mail in one list, one row per "thing" — the new messages
// in a chat, a mail thread — not per message and not per chat. The hub collects them in the background, keeps them
// in Postgres with their AI triage (inbox_triage.go) and whether the user has dealt with them, and tells the apps
// {"type":"inbox"} when the list changes.

const (
	inboxWindow   = 7 * 24 * time.Hour // older unread chats / mail are history, not inbox
	inboxKeepDone = 3 * 24 * time.Hour // done items stay listed (for undo) this long
	inboxForget   = 8 * 24 * time.Hour // and are remembered (so old unread messages don't come back) this long
	inboxPoll     = 2 * time.Minute    // fallback when no Feishu event arrives
	inboxMailPoll = 3 * time.Minute    // IMAP is slow; Gmail goes through a proxy
	inboxDebounce = 3 * time.Second    // a burst of Feishu events collects once
	inboxMessages = 20                 // newest messages of a chat read into an item
	inboxText     = 600                // characters of one message kept for triage
)

// Item kinds.
const (
	KindDM      = "dm"      // a person wrote to me
	KindMention = "mention" // @me / @all in a group
	KindGroup   = "group"   // new messages in a group I follow (not muted)
	KindBot     = "bot"     // a bot or app chat
	KindMail    = "mail"
)

// Triage tiers (inbox_triage.go).
const (
	TierAction = "action" // 需要你
	TierFYI    = "fyi"    // 知悉即可
	TierNoise  = "noise"  // 噪音
)

type InboxItem struct {
	ID          string     `json:"id"`
	Source      string     `json:"source"` // lark | mail
	Kind        string     `json:"kind"`
	Account     string     `json:"account"`      // "feishu" or a mail account id
	AccountName string     `json:"account_name"` // 飞书 / the mail address
	Title       string     `json:"title"`        // chat name / mail subject
	Sender      string     `json:"sender,omitempty"`
	Avatar      string     `json:"avatar,omitempty"`
	Preview     string     `json:"preview"`
	Count       int        `json:"count"`
	StartedAt   time.Time  `json:"started_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Tier        string     `json:"tier,omitempty"` // empty until triaged
	Summary     string     `json:"summary,omitempty"`
	Reason      string     `json:"reason,omitempty"`
	Read        bool       `json:"read,omitempty"` // read somewhere else (Feishu, another mail client)
	DoneAt      *time.Time `json:"done_at,omitempty"`
	DoneBy      string     `json:"done_by,omitempty"` // user | read | replied
	Lark        *InboxLark `json:"lark,omitempty"`
	Mail        *InboxMail `json:"mail,omitempty"`
	Doc         *InboxDoc  `json:"doc,omitempty"` // a Feishu document's @me / comment (inbox_docs.go)
	// Translations by target language: new foreign mail, translated before it is opened (inbox_translate.go).
	Translations map[string]*InboxTranslation `json:"translations,omitempty"`
	Moment       *InboxMoment                 `json:"moment,omitempty"` // a removed post's comments / @me (inbox_moments.go)
	// Connector: a thread of a connector's (GitHub…), Source being its id (inbox_connectors.go).
	Connector *InboxConnector `json:"connector,omitempty"`
	// Agent is the AI 分身's work on the item: judged by model 2, handled and drafted by the agent (inbox_agent.go).
	Agent *InboxAgent `json:"agent,omitempty"`
	// Mutes are what it can be muted by (listed only; inbox_mutes.go).
	Mutes []MuteOption `json:"mutes,omitempty"`

	// Bookkeeping: persisted, not listed (GET /api/v1/inbox/{id} returns Context).
	Last      string `json:"last,omitempty"`       // newest message position / mail uid in the item
	Context   string `json:"context,omitempty"`    // the conversation as text, for triage and 交给 AI
	TriagedAt string `json:"triaged_at,omitempty"` // Last when it was triaged
	BodyOf    string `json:"body_of,omitempty"`
	JudgedAt  string `json:"judged_at,omitempty"` // Last when model 2 looked at it    // mail: Last when the full text was read into Preview / Context
}

type InboxLark struct {
	AccountID       int    `json:"account_id,omitempty"` // zero in legacy rows means primary account 1
	FeedID          string `json:"feed_id"`
	IsGroup         bool   `json:"is_group"`
	IsTopic         bool   `json:"is_topic,omitempty"`
	StartPosition   string `json:"start_position,omitempty"`   // the first new message (open the chat there)
	ReadPosition    string `json:"read_position,omitempty"`    // the chat's read position in Feishu, as of the last collection
	MentionPosition string `json:"mention_position,omitempty"` // the unread @me
	AtAll           bool   `json:"at_all,omitempty"`
	// ReplyTo is the newest message from someone else; ThreadKind says where it sits: "" in the chat, "topic" in a
	// topic group's topic, "thread" in a thread of a normal group. A reply from the inbox goes there (replyInbox).
	ReplyTo    string `json:"reply_to,omitempty"`
	ThreadKind string `json:"thread_kind,omitempty"`
	// SenderID is who sent the newest message from someone else (a group's sender, to mute them).
	SenderID string `json:"sender_id,omitempty"`
}

// Values of InboxLark.ThreadKind.
const (
	LarkThreadTopic  = "topic"
	LarkThreadThread = "thread"
)

// larkThreadKind is InboxLark.ThreadKind for message m of chat c: a topic group (lark-connector chatMode "topic") lists
// topics, so every item there is in one; in a normal group a thread reply shown in the chat has threadMessageType
// "reply". A thread root is a plain message in the chat.
func larkThreadKind(c lark.Conversation, m lark.Message) string {
	switch {
	case c.IsTopic:
		return LarkThreadTopic
	case c.IsGroup && m.ThreadKind == "reply":
		return LarkThreadThread
	}
	return ""
}

type InboxMail struct {
	AccountID string       `json:"account_id"`
	Mailbox   string       `json:"mailbox"`
	UIDs      []uint32     `json:"uids"` // oldest first
	MessageID string       `json:"message_id"`
	From      mail.Address `json:"from"`
	Direct    bool         `json:"direct,omitempty"` // to me, not cc / a list
	Thread    string       `json:"thread"`           // normalized subject: replies join the same item
}

func (it *InboxItem) open() bool { return it.DoneAt == nil }

// listed is the item as the list shows it (no context).
func (it *InboxItem) listed() InboxItem {
	c := *it
	c.Context, c.Last, c.TriagedAt, c.BodyOf, c.JudgedAt = "", "", "", "", ""
	c.Mutes = mutesOf(it)
	if it.Agent != nil { // the copy is encoded after the lock is released
		a := *it.Agent
		c.Agent = &a
	}
	if it.Mail != nil {
		m := *it.Mail
		m.UIDs = append([]uint32(nil), m.UIDs...)
		c.Mail = &m
	}
	if it.Doc != nil {
		d := *it.Doc
		c.Doc = &d
	}
	if len(it.Translations) > 0 { // the list shows subject and preview; the body is fetched when the mail opens
		c.Translations = map[string]*InboxTranslation{}
		for k, t := range it.Translations {
			if t != nil && t.Of == it.Last {
				c.Translations[k] = &InboxTranslation{Subject: t.Subject, Preview: t.Preview, Clipped: t.Clipped}
			}
		}
	}
	if it.Moment != nil {
		m := *it.Moment
		m.Notices = append([]string(nil), it.Moment.Notices...)
		c.Moment = &m
	}
	if it.Connector != nil {
		k := *it.Connector
		c.Connector = &k
	}
	return c
}

type inbox struct {
	larkAccount int // selected once at startup; zero is primary for legacy callers
	mu          sync.Mutex
	items       map[string]*InboxItem
	kick        chan struct{}
	once        sync.Once
	running     sync.Mutex  // one collection at a time
	triaging    sync.Mutex  // one triage pass at a time (inbox_triage.go)
	handing     sync.Mutex  // one judge / agent pass at a time (inbox_agent.go)
	translating sync.Mutex  // one translation pass at a time (inbox_translate.go)
	me          lark.Me     // the Feishu account (who @me is in documents), read once
	replies     sync.Map    // item id -> *inboxReply: Feishu replies to one item, one at a time (inbox_triage.go)
	mutes       []InboxMute // what the user muted (inbox_mutes.go)
	mutesMu     sync.Mutex  // one read-change-save of the kept mutes at a time
}

func (b *inbox) init() {
	b.once.Do(func() {
		b.items = map[string]*InboxItem{}
		b.kick = make(chan struct{}, 1)
	})
}

// kickInbox asks for a Feishu collection soon (a realtime event arrived).
func (h *Hub) kickInbox() {
	h.inbox.init()
	select {
	case h.inbox.kick <- struct{}{}:
	default:
	}
}

func (h *Hub) inboxLoop(ctx context.Context) {
	h.inbox.init()
	h.loadInbox(ctx)
	if !h.isMember() {
		h.collectInbox(ctx, true)
	}
	lastMail := time.Now()
	t := time.NewTicker(inboxPoll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-h.inbox.kick:
			select { // let the burst finish
			case <-ctx.Done():
				return
			case <-time.After(inboxDebounce):
			}
		}
		if h.isMember() { // the center keeps the group's inbox; this one's stays as it was (mesh_group.go)
			continue
		}
		withMail := time.Since(lastMail) >= inboxMailPoll
		if withMail {
			lastMail = time.Now()
		}
		h.collectInbox(ctx, withMail)
	}
}

func (h *Hub) loadInbox(ctx context.Context) {
	h.loadInboxMutes(ctx)
	if h.lark != nil {
		h.inbox.mu.Lock()
		h.inbox.larkAccount = h.lark.Account()
		h.inbox.mu.Unlock()
	}
	if h.store == nil {
		return
	}
	rows, err := h.store.InboxItems(ctx, time.Now().Add(-inboxForget))
	if err != nil {
		h.logf("inbox: load: %v", err)
		return
	}
	h.inbox.mu.Lock()
	defer h.inbox.mu.Unlock()
	for _, raw := range rows {
		var it InboxItem
		if json.Unmarshal(raw, &it) == nil && it.ID != "" && h.inbox.acceptsAccount(&it) {
			h.inbox.items[it.ID] = &it
		}
	}
}

// Keep persisted items from another account out of all inbox reads and actions.
// They stay in Postgres and become available again after switching back.
func (b *inbox) acceptsAccount(it *InboxItem) bool {
	if it.Lark == nil {
		return true
	}
	account, selected := it.Lark.AccountID, b.larkAccount
	if account == 0 {
		account = 1
	}
	if selected == 0 {
		selected = 1
	}
	return account == selected
}

// collectInbox reads Feishu (and mail) once and applies what changed.
func (h *Hub) collectInbox(ctx context.Context, withMail bool) {
	h.inbox.running.Lock()
	defer h.inbox.running.Unlock()
	var changed []*InboxItem
	// The hub's own connectors (Feishu, then mail) in the order registered, then the others' generic things.
	for _, c := range h.connectors.All() {
		if own, ok := c.(ownInbox); ok {
			changed = append(changed, own.collectInbox(ctx, withMail)...)
		}
	}
	changed = append(changed, h.collectConnectors(ctx)...)
	h.inbox.mu.Lock()
	for id, it := range h.inbox.items {
		if it.DoneAt != nil && time.Since(*it.DoneAt) > inboxForget {
			delete(h.inbox.items, id)
			h.inbox.replies.Delete(id)
		}
	}
	changed = append(changed, h.inbox.applyMutes(time.Now())...)
	h.inbox.mu.Unlock()
	h.saveInbox(ctx, changed)
	h.triageInbox(ctx)
	h.translateInbox(ctx)
}

// saveInbox persists changed items and tells the apps.
func (h *Hub) saveInbox(ctx context.Context, changed []*InboxItem) {
	if len(changed) == 0 {
		return
	}
	h.inbox.mu.Lock()
	type row struct {
		id      string
		data    []byte
		updated time.Time
		done    *time.Time
	}
	rows := make([]row, 0, len(changed))
	for _, it := range changed {
		raw, _ := json.Marshal(it)
		rows = append(rows, row{it.ID, raw, it.UpdatedAt, it.DoneAt})
	}
	h.inbox.mu.Unlock()
	if h.store != nil {
		for _, r := range rows {
			if err := h.store.PutInboxItem(ctx, r.id, r.data, r.updated, r.done); err != nil {
				h.logf("inbox: save %s: %v", r.id, err)
			}
		}
	}
	h.broadcast(map[string]any{"type": "inbox"})
}

// ---- Feishu ----

func (h *Hub) collectLark(ctx context.Context) ([]*InboxItem, error) {
	// Feishu's 未读 (muted chats left out) plus @me anywhere, muted groups included.
	var convs []lark.Conversation
	seen := map[string]bool{}
	for _, scope := range []string{"unread", "mentions"} {
		cursor := ""
		for page := 0; page < 3; page++ {
			list, next, err := h.lark.Conversations(ctx, scope, cursor, 100)
			if err != nil {
				return nil, err
			}
			for _, c := range list {
				if !seen[c.ID] {
					seen[c.ID] = true
					convs = append(convs, c)
				}
			}
			if next == "" {
				break
			}
			cursor = next
		}
	}
	fetch := func(c lark.Conversation, n int) ([]lark.Message, error) {
		p, err := h.lark.Messages(ctx, c.ID, lark.Window{}, n, c.IsTopic)
		return p.Messages, err
	}
	changed := h.inbox.applyLark(convs, fetch, time.Now(), h.userLocation())
	// Open items whose chat is no longer unread: read in Feishu (or here). An item that needs the user stays
	// until they reply or tick it off; the rest are done.
	changed = append(changed, h.inbox.settleLark(convs, func(feed string, topic bool) (*lark.Message, error) {
		p, err := h.lark.Messages(ctx, feed, lark.Window{}, 1, topic)
		if err != nil || len(p.Messages) == 0 {
			return nil, err
		}
		return &p.Messages[len(p.Messages)-1], nil
	}, time.Now())...)
	return changed, nil
}

// larkKind says how a chat with unread messages enters the inbox; "" keeps it out (a muted group nobody @'d me in).
func larkKind(c lark.Conversation) string {
	if !c.IsGroup {
		for _, t := range c.Tags {
			if t == "机器人" || t == "AI" {
				return KindBot
			}
		}
		return KindDM
	}
	if c.AtMe || c.AtAll || c.MentionAt != "" {
		return KindMention
	}
	if c.Muted {
		return ""
	}
	return KindGroup
}

// positionAfter reports whether position a comes after b (positions are numbers as strings).
func positionAfter(a, b string) bool {
	x, err1 := strconv.ParseInt(a, 10, 64)
	y, err2 := strconv.ParseInt(b, 10, 64)
	if err1 != nil || err2 != nil {
		return a > b
	}
	return x > y
}

// openFor finds the open item of a Feishu chat / mail thread; done is the newest done one.
func (b *inbox) openFor(match func(*InboxItem) bool) (open, done *InboxItem) {
	for _, it := range b.items {
		if !match(it) {
			continue
		}
		if it.open() {
			open = it
		} else if done == nil || it.UpdatedAt.After(done.UpdatedAt) {
			done = it
		}
	}
	return open, done
}

// applyLark turns the unread chats into items: one open item per chat collects its new messages until it is done;
// messages after that start a new one.
// loc is the user's time zone: the context gives each message's time in it.
func (b *inbox) applyLark(convs []lark.Conversation, fetch func(lark.Conversation, int) ([]lark.Message, error), now time.Time, loc *time.Location) []*InboxItem {
	var changed []*InboxItem
	for _, c := range convs {
		if c.Unread == 0 && !c.MarkedUnread {
			continue
		}
		if !c.UpdatedAt.IsZero() && now.Sub(c.UpdatedAt) > inboxWindow {
			continue
		}
		kind := larkKind(c)
		if kind == "" {
			continue
		}
		b.mu.Lock()
		open, done := b.openFor(func(it *InboxItem) bool { return it.Lark != nil && it.Lark.FeedID == c.ID })
		unchanged := open != nil && open.UpdatedAt.Equal(c.UpdatedAt) && open.Count >= c.Unread && !open.Read ||
			open == nil && done != nil && !c.UpdatedAt.After(done.UpdatedAt) // done here, still unread in Feishu
		if unchanged && open != nil && lark-connector.ReadPosition != c.ReadPosition {
			// Read further elsewhere with nothing new: only the read position moves.
			lark-connector.ReadPosition = c.ReadPosition
			changed = append(changed, open)
		}
		b.mu.Unlock()
		if unchanged {
			continue
		}
		msgs, err := fetch(c, min(max(c.Unread, 1), inboxMessages))
		if err != nil || len(msgs) == 0 {
			continue
		}
		// The unread ones are the newest; a done item already covered what came before its last message.
		if c.Unread > 0 && len(msgs) > c.Unread {
			msgs = msgs[len(msgs)-c.Unread:]
		}
		b.mu.Lock()
		if open == nil && done != nil {
			var fresh []lark.Message
			for _, m := range msgs {
				if positionAfter(m.Position, done.Last) {
					fresh = append(fresh, m)
				}
			}
			msgs = fresh
		}
		var others []lark.Message // what others wrote: my own messages don't make a chat need me
		for _, m := range msgs {
			if !m.IsMine {
				others = append(others, m)
			}
		}
		if len(others) == 0 {
			b.mu.Unlock()
			continue
		}
		it := open
		if it == nil {
			account := max(b.larkAccount, 1)
			id := "lark:" + c.ID + ":" + others[0].Position
			if account != 1 {
				id = fmt.Sprintf("lark:account:%d:%s:%s", account, c.ID, others[0].Position)
			}
			it = &InboxItem{ID: id, Source: "lark", Account: "feishu", AccountName: "飞书",
				StartedAt: others[0].CreatedAt, Lark: &InboxLark{AccountID: account, FeedID: c.ID, StartPosition: others[0].Position}}
			b.items[it.ID] = it
		}
		last := others[len(others)-1]
		it.Kind = kind
		if it.Kind != KindMention && open != nil && open.Kind == KindMention {
			it.Kind = KindMention // an @me stays an @me while the item is open
		}
		it.Title, it.Avatar = c.Name, c.Avatar
		it.Lark.IsGroup, it.Lark.IsTopic, it.Lark.AtAll = c.IsGroup, c.IsTopic, c.AtAll
		it.Lark.ReadPosition = c.ReadPosition
		it.Lark.ReplyTo, it.Lark.ThreadKind, it.Lark.SenderID = last.ID, larkThreadKind(c, last), last.SenderID
		if c.MentionAt != "" {
			it.Lark.MentionPosition = c.MentionAt
		}
		it.Sender, it.Preview = last.SenderName, clip(oneLine(last.Text), 200)
		it.Count = max(c.Unread, len(others))
		it.UpdatedAt = c.UpdatedAt
		if it.UpdatedAt.IsZero() || last.CreatedAt.After(it.UpdatedAt) {
			it.UpdatedAt = last.CreatedAt
		}
		it.Read = false
		it.Last = last.Position
		it.Context = larkContext(c, others, loc)
		changed = append(changed, it)
		b.mu.Unlock()
	}
	return changed
}

// settleLark closes what was read elsewhere: an open item whose chat has no unread messages any more.
func (b *inbox) settleLark(convs []lark.Conversation, latest func(feed string, topic bool) (*lark.Message, error), now time.Time) []*InboxItem {
	unread := map[string]bool{}
	for _, c := range convs {
		if c.Unread > 0 || c.MarkedUnread {
			unread[c.ID] = true
		}
	}
	b.mu.Lock()
	var stale []*InboxItem
	for _, it := range b.items {
		if it.Lark != nil && it.open() && !unread[it.Lark.FeedID] && !it.Read {
			stale = append(stale, it)
		}
	}
	b.mu.Unlock()
	var changed []*InboxItem
	for _, it := range stale {
		replied := false
		if it.Tier == TierAction || it.Tier == "" {
			if m, err := latest(it.Lark.FeedID, it.Lark.IsTopic); err == nil && m != nil && m.IsMine && positionAfter(m.Position, it.Last) {
				replied = true
			}
		}
		b.mu.Lock()
		// Nothing in the chat is unread any more: drop the count so the row stops showing the old badge.
		it.Read, it.Count = true, 0
		switch {
		case replied:
			it.done(now, "replied")
		case it.Tier != TierAction && it.Tier != "":
			it.done(now, "read")
		}
		b.mu.Unlock()
		changed = append(changed, it)
	}
	return changed
}

func (it *InboxItem) done(at time.Time, by string) {
	t := at
	it.DoneAt, it.DoneBy = &t, by
}

func larkContext(c lark.Conversation, msgs []lark.Message, loc *time.Location) string {
	var b strings.Builder
	if c.IsGroup {
		fmt.Fprintf(&b, "飞书群「%s」的新消息：\n", c.Name)
	} else {
		fmt.Fprintf(&b, "飞书私聊（%s）的新消息：\n", c.Name)
	}
	for _, m := range msgs {
		at := ""
		if m.AtMe {
			at = "（@了我）"
		}
		fmt.Fprintf(&b, "[%s] %s%s：%s\n", m.CreatedAt.In(loc).Format("01-02 15:04"), m.SenderName, at, clip(oneLine(m.Text), inboxText))
	}
	return strings.TrimSpace(b.String())
}

// ---- mail ----

func (h *Hub) collectMail(ctx context.Context) []*InboxItem {
	accounts, err := h.mail.ListAccounts()
	if err != nil {
		h.logf("inbox: mail accounts: %v", err)
		return nil
	}
	var changed []*InboxItem
	for _, a := range accounts {
		res, err := h.mail.ListMessages(ctx, mail.ListOptions{AccountID: a.ID, Limit: 40})
		if err != nil {
			h.logf("inbox: mail %s: %v", a.Email, err)
			continue
		}
		changed = append(changed, h.inbox.applyMail(a, res.Messages, time.Now())...)
		// New mail, and items collected before their text was read.
		h.inbox.mu.Lock()
		var unread []*InboxItem
		for _, it := range h.inbox.items {
			if it.Mail != nil && it.Mail.AccountID == a.ID && it.open() && !it.Read && it.BodyOf != it.Last {
				unread = append(unread, it)
			}
		}
		h.inbox.mu.Unlock()
		for _, it := range unread {
			if h.readMailBody(ctx, a, it) {
				changed = append(changed, it)
			}
		}
	}
	return changed
}

// readMailBody replaces the list's 2 KB snippet with the newest message's text (HTML-only newsletters have nothing
// but CSS in their first bytes). The read peeks: the mail stays unread.
func (h *Hub) readMailBody(ctx context.Context, a mail.MailAccount, it *InboxItem) bool {
	h.inbox.mu.Lock()
	if it.Mail == nil || len(it.Mail.UIDs) == 0 {
		h.inbox.mu.Unlock()
		return false
	}
	uid, mailbox, last := it.Mail.UIDs[len(it.Mail.UIDs)-1], it.Mail.Mailbox, it.Last
	h.inbox.mu.Unlock()
	m, err := h.mail.GetMessage(ctx, a.ID, uid, mailbox)
	if err != nil {
		h.logf("inbox: read mail %s/%d: %v", a.Email, uid, err)
		return false
	}
	text := strings.TrimSpace(m.Text)
	if text == "" {
		text = strings.TrimSpace(mail.PlainText(m.HTML))
	}
	h.inbox.mu.Lock()
	defer h.inbox.mu.Unlock()
	it.BodyOf = last
	if text == "" {
		return true
	}
	it.Preview = clip(oneLine(text), 200)
	var to []string
	for _, t := range m.To {
		to = append(to, t.Email)
	}
	var cc []string
	for _, t := range m.Cc {
		cc = append(cc, t.Email)
	}
	it.Context = fmt.Sprintf("邮箱 %s 收到的未读邮件（共 %d 封，下面是最新一封）：\n发件人：%s <%s>\n收件人：%s\n抄送：%s\n时间：%s\n主题：%s\n\n%s",
		a.Email, len(it.Mail.UIDs), m.From.Name, m.From.Email, strings.Join(to, ", "), strings.Join(cc, ", "), m.Date, m.Subject, clip(text, 3000))
	return true
}

var replyPrefix = regexp.MustCompile(`(?i)^\s*((re|fw|fwd|aw|回复|答复|转发)\s*[:：]\s*|\[[^\]]{1,30}\]\s*)+`)

// mailThread is what keeps replies in one item: the subject without Re: / Fwd: / [list] prefixes.
func mailThread(subject string) string {
	s := strings.ToLower(strings.Join(strings.Fields(replyPrefix.ReplaceAllString(subject, "")), " "))
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:6])
}

// applyMail turns one account's latest inbox page into items: unseen messages grouped by thread.
func (b *inbox) applyMail(a mail.MailAccount, msgs []mail.MailSummary, now time.Time) []*InboxItem {
	threads := map[string][]mail.MailSummary{}
	var order []string
	for _, m := range msgs {
		t := mailThread(m.Subject)
		if _, ok := threads[t]; !ok {
			order = append(order, t)
		}
		threads[t] = append(threads[t], m)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var changed []*InboxItem
	for _, t := range order {
		list := threads[t]
		sort.Slice(list, func(i, j int) bool { return list[i].UID < list[j].UID })
		var unseen []mail.MailSummary
		for _, m := range list {
			d, _ := time.Parse(time.RFC3339, m.Date)
			if !m.Seen && now.Sub(d) <= inboxWindow {
				unseen = append(unseen, m)
			}
		}
		open, done := b.openFor(func(it *InboxItem) bool {
			return it.Mail != nil && it.Mail.AccountID == a.ID && it.Mail.Thread == t
		})
		if open == nil && done != nil {
			last, _ := strconv.ParseUint(done.Last, 10, 32)
			var fresh []mail.MailSummary
			for _, m := range unseen {
				if uint64(m.UID) > last {
					fresh = append(fresh, m)
				}
			}
			unseen = fresh
		}
		if len(unseen) == 0 {
			// Read in another client: done unless it needs the user.
			if open != nil && !open.Read {
				open.Read = true
				if open.Tier != TierAction && open.Tier != "" {
					open.done(now, "read")
				}
				changed = append(changed, open)
			}
			continue
		}
		newest := unseen[len(unseen)-1]
		if open != nil && open.Last == strconv.FormatUint(uint64(newest.UID), 10) && len(open.Mail.UIDs) == len(unseen) {
			continue
		}
		it := open
		if it == nil {
			it = &InboxItem{ID: fmt.Sprintf("mail:%s:%s:%d", a.ID, t, unseen[0].UID), Source: "mail", Kind: KindMail,
				Account: a.ID, Mail: &InboxMail{AccountID: a.ID, Mailbox: newest.Mailbox, Thread: t}}
			it.StartedAt, _ = time.Parse(time.RFC3339, unseen[0].Date)
			b.items[it.ID] = it
		}
		it.AccountName = a.Email
		it.Title = newest.Subject
		if it.Title == "" {
			it.Title = "（无主题）"
		}
		it.Sender = newest.From.Name
		if it.Sender == "" {
			it.Sender = newest.From.Email
		}
		it.Preview = clip(oneLine(newest.Snippet), 200)
		it.Count = len(unseen)
		it.UpdatedAt, _ = time.Parse(time.RFC3339, newest.Date)
		it.Read = false
		it.Last = strconv.FormatUint(uint64(newest.UID), 10)
		it.Mail.UIDs = it.Mail.UIDs[:0]
		for _, m := range unseen {
			it.Mail.UIDs = append(it.Mail.UIDs, m.UID)
		}
		it.Mail.MessageID, it.Mail.From = newest.MessageID, newest.From
		it.Mail.Direct = false
		for _, to := range newest.To {
			if strings.EqualFold(to.Email, a.Email) {
				it.Mail.Direct = true
			}
		}
		it.Context = mailContext(a, unseen)
		changed = append(changed, it)
	}
	return changed
}

func mailContext(a mail.MailAccount, msgs []mail.MailSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "邮箱 %s 收到的未读邮件：\n", a.Email)
	for _, m := range msgs {
		var to []string
		for _, t := range m.To {
			to = append(to, t.Email)
		}
		fmt.Fprintf(&b, "---\n发件人：%s <%s>\n收件人：%s\n时间：%s\n主题：%s\n%s\n", m.From.Name, m.From.Email, strings.Join(to, ", "),
			m.Date, m.Subject, clip(oneLine(m.Snippet), inboxText))
	}
	return strings.TrimSpace(b.String())
}

// ---- API ----

var errNoInboxItem = errors.New("no such inbox item")

var errCannotReply = errors.New("this item cannot be replied to")

func (h *Hub) inboxRoutes(api *http.ServeMux) {
	h.inbox.init()
	h.inboxMuteRoutes(api)
	// Items of connectors (GitHub…) only go to apps that draw them (?sources=all): an older app would show a PR as
	// a mail and send a reply as one.
	api.HandleFunc("GET /api/v1/inbox", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"items": h.inboxList(r.URL.Query().Get("sources") == "all")})
	})
	api.HandleFunc("GET /api/v1/inbox/{id}", func(w http.ResponseWriter, r *http.Request) {
		h.inbox.mu.Lock()
		it, ok := h.inbox.items[r.PathValue("id")]
		var c InboxItem
		if ok {
			c = it.listed()
			c.Context = it.Context
		}
		h.inbox.mu.Unlock()
		if !ok {
			writeErr(w, http.StatusNotFound, errNoInboxItem)
			return
		}
		writeJSON(w, struct {
			InboxItem
			ReplyTarget *inboxMailTarget `json:"reply_target,omitempty"`
		}{c, mailTarget(c)})
	})
	api.HandleFunc("POST /api/v1/inbox/{id}/done", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Done bool `json:"done"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		it, err := h.setInboxDone(r.Context(), r.PathValue("id"), in.Done)
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, it)
	})
	api.HandleFunc("POST /api/v1/inbox/{id}/draft", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Instruction string `json:"instruction"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		text, err := h.draftReply(r.Context(), r.PathValue("id"), in.Instruction)
		switch {
		case errors.Is(err, errNoInboxItem):
			writeErr(w, http.StatusNotFound, err)
		case err != nil:
			writeErr(w, http.StatusBadGateway, err)
		default:
			writeJSON(w, map[string]any{"text": text})
		}
	})
	api.HandleFunc("POST /api/v1/inbox/{id}/reply", h.withReplyReceipt("inbox_reply", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Text            string      `json:"text"`
			Changes         []DocChange `json:"changes"` // documents: status moves to make with the reply
			ClientID        string      `json:"client_id"`
			ExpectedUID     *uint32     `json:"expected_uid"`
			ExpectedVersion *string     `json:"expected_version"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Text) == "" && len(in.Changes) == 0 {
			writeErr(w, http.StatusBadRequest, errors.New("text is required"))
			return
		}
		it, err := h.replyInboxChecked(r.Context(), r.PathValue("id"), strings.TrimSpace(in.Text), in.ClientID, in.ExpectedUID, in.ExpectedVersion, in.Changes...)
		switch {
		case errors.Is(err, errMailReplyTargetChanged):
			writeErrorBody(w, http.StatusConflict, map[string]any{"code": "mail_reply_target_changed", "error": "邮件回复对象已变化，请查看最新邮件后确认发送", "retryable": false, "delivered": "none"})
		case errors.Is(err, errNoInboxItem):
			writeErr(w, http.StatusNotFound, err)
		case err != nil:
			writeErr(w, http.StatusBadGateway, err)
		default:
			writeJSON(w, it)
		}
	}))
	api.HandleFunc("POST /api/v1/inbox/{id}/doc/status", func(w http.ResponseWriter, r *http.Request) {
		var in DocChange
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Ref == "" || in.Field == "" || in.Value == "" {
			writeErr(w, http.StatusBadRequest, errors.New("ref, field and value are required"))
			return
		}
		it, err := h.setDocStatus(r.Context(), r.PathValue("id"), in)
		switch {
		case errors.Is(err, errNoInboxItem):
			writeErr(w, http.StatusNotFound, err)
		case errors.Is(err, errNoDocRow):
			writeErr(w, http.StatusBadRequest, err)
		case err != nil:
			writeErr(w, http.StatusBadGateway, err)
		default:
			writeJSON(w, it)
		}
	})
	api.HandleFunc("POST /api/v1/inbox/refresh", func(w http.ResponseWriter, r *http.Request) {
		h.collectInbox(r.Context(), true)
		writeJSON(w, map[string]any{"items": h.inboxList(r.URL.Query().Get("sources") == "all")})
	})
}

type inboxMailTarget struct {
	UID     uint32 `json:"uid"`
	Version string `json:"version"`
}

var errMailReplyTargetChanged = errors.New("mail reply target changed")

func mailTarget(it InboxItem) *inboxMailTarget {
	if it.Mail == nil || len(it.Mail.UIDs) == 0 {
		return nil
	}
	// Bind the version to the account, mailbox, message and recipient, not read/triage state.
	raw, _ := json.Marshal(it.Mail)
	sum := sha256.Sum256(raw)
	return &inboxMailTarget{UID: it.Mail.UIDs[len(it.Mail.UIDs)-1], Version: hex.EncodeToString(sum[:])}
}

func (h *Hub) replyInboxChecked(ctx context.Context, id, text, clientID string, uid *uint32, version *string, changes ...DocChange) (InboxItem, error) {
	if uid == nil && version == nil {
		return h.replyInboxAs(ctx, id, text, clientID, changes...)
	}
	// Collection and checked sends must not change the target between validation and sending.
	h.inbox.running.Lock()
	defer h.inbox.running.Unlock()
	h.inbox.mu.Lock()
	it := h.inbox.items[id]
	var target *inboxMailTarget
	if it != nil {
		target = mailTarget(*it)
	}
	h.inbox.mu.Unlock()
	if it == nil {
		return InboxItem{}, errNoInboxItem
	}
	if target == nil || uid != nil && *uid != target.UID || version != nil && *version != target.Version {
		return InboxItem{}, errMailReplyTargetChanged
	}
	return h.replyInboxAs(ctx, id, text, clientID, changes...)
}

// inboxList is every open item and the recently done ones, newest first; connectors' only when all (rows stored
// before Source was set have none, so it is the connector block that tells).
func (h *Hub) inboxList(all bool) []InboxItem {
	h.inbox.mu.Lock()
	defer h.inbox.mu.Unlock()
	cutoff := time.Now().Add(-inboxKeepDone)
	out := make([]InboxItem, 0, len(h.inbox.items))
	for _, it := range h.inbox.items {
		if it.DoneAt != nil && it.DoneAt.Before(cutoff) || !all && it.Connector != nil {
			continue
		}
		out = append(out, it.listed())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

func (h *Hub) setInboxDone(ctx context.Context, id string, done bool) (InboxItem, error) {
	h.inbox.mu.Lock()
	it, ok := h.inbox.items[id]
	if !ok {
		h.inbox.mu.Unlock()
		return InboxItem{}, errNoInboxItem
	}
	if done {
		it.done(time.Now(), "user")
	} else {
		it.DoneAt, it.DoneBy = nil, ""
	}
	out := it.listed()
	h.inbox.mu.Unlock()
	h.saveInbox(ctx, []*InboxItem{it})
	return out, nil
}
