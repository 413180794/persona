package assistant

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
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/rpc"
)

// The unified inbox (docs/inbox.md) is the manager's: Feishu messages, mail and the connectors' threads in one list,
// one row per "thing" — the new messages in a chat, a mail thread — not per message and not per chat. The hub reads
// the sources when a round is due and hands what it read over (InboxCollected); the inbox keeps the items in Postgres
// (table inbox_items, only it reads and writes it) with their AI triage (inbox_triage.go) and whether the user has
// dealt with them, answers the /api/v1/inbox… routes the hub relays, and tells the apps {"type":"inbox"} (through the
// hub, assistantwire.EventInboxChanged) when the list changes.

const (
	inboxWindow   = 7 * 24 * time.Hour // older unread chats / mail are history, not inbox
	inboxKeepDone = 3 * 24 * time.Hour // done items stay listed (for undo) this long
	inboxForget   = 8 * 24 * time.Hour // and are remembered (so old unread messages don't come back) this long
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
	DoneBy      string     `json:"done_by,omitempty"` // user | dismissed | read | replied
	Lark        *InboxLark `json:"lark,omitempty"`
	// Conversation is where the item opens in a messaging source (docs/extension-messaging.md「九」, #2138): listed
	// only, from Lark; an app with the source opens it there, an older one uses Lark.
	Conversation *wire.MessagingConversationRef `json:"conversation,omitempty"`
	Mail        *InboxMail `json:"mail,omitempty"`
	Doc         *InboxDoc  `json:"doc,omitempty"` // a Feishu document's @me / comment (inbox_blocks.go)
	// Translations by target language: new foreign mail, translated before it is opened (inbox_translate.go).
	Translations map[string]*InboxTranslation `json:"translations,omitempty"`
	Moment       *InboxMoment                 `json:"moment,omitempty"` // a 公司圈 post's comments / @me (inbox_blocks.go)
	// Connector: a thread of a connector's (GitHub…), Source being its id (inbox_connectors.go).
	Connector *InboxConnector `json:"connector,omitempty"`
	// Agent is the AI 分身's work on the item: judged by model 2, handled and drafted by the agent (inbox_agent.go).
	Agent *InboxAgent `json:"agent,omitempty"`
	// Mutes are what it can be muted by (listed only; inbox_mutes.go).
	Mutes []MuteOption `json:"mutes,omitempty"`
	// Version names what the item holds now (listed only): a bulk 已处理 sends it back, so an item that took new
	// messages since the list was drawn is not filed with them (inbox_bulk.go).
	Version string `json:"version,omitempty"`
	// TriagedBy says who sorted an event (inbox_group.go): "model" (its title, summary and tier are the model's) or
	// "rules" (the connector grouped it and its rule tier stands, 按规则).
	TriagedBy string `json:"triaged_by,omitempty"`
	// Parent is what the item grew out of: another item, a todo or an agent session (inbox_parent.go, #2128).
	Parent *InboxParent `json:"parent,omitempty"`

	// Bookkeeping: persisted, not listed (GET /api/v1/inbox/{id} returns Context).
	Last      string `json:"last,omitempty"`       // newest message position / mail uid in the item
	Context   string `json:"context,omitempty"`    // the conversation as text, for triage and 交给 AI
	TriagedAt string `json:"triaged_at,omitempty"` // Last when it was triaged
	BodyOf    string `json:"body_of,omitempty"`    // mail: Last when the full text was read into Preview / Context
	JudgedAt  string `json:"judged_at,omitempty"`  // Last when model 2 looked at it
	// Rules are the item's own triage rules, from its connector (InboxThing.Rules): they replace inbox.rules for it.
	Rules string `json:"rules,omitempty"`
	// ArrivedAt is the clock here when the item first held Last (ArrivedOf), as listed or saved: a bulk 已处理 over a
	// filter leaves what arrived after the list it acted on (inbox_bulk.go). Rows stored before have UpdatedAt.
	ArrivedOf string    `json:"arrived_of,omitempty"`
	ArrivedAt time.Time `json:"arrived_at,omitzero"`
	// Meta is an event's meta as its connector wrote it (which messages are in it…): kept and handed back as it is,
	// never read here. Group is the event's conversation (inbox_group.go).
	Meta  json.RawMessage `json:"meta,omitempty"`
	Group *InboxGroup     `json:"group,omitempty"`
}

type InboxLark struct {
	AccountID int `json:"account_id,omitempty"` // zero in legacy rows means primary account 1
	// OLAccount is what the apps pass as ol_account to the /api/v1/lark routes about this item: AccountID when that
	// is not lark.account, else zero (larkRequestAccount).
	OLAccount       int    `json:"ol_account,omitempty"`
	FeedID          string `json:"feed_id"`
	IsGroup         bool   `json:"is_group"`
	IsTopic         bool   `json:"is_topic,omitempty"`
	StartPosition   string `json:"start_position,omitempty"`   // the first new message (open the chat there)
	ReadPosition    string `json:"read_position,omitempty"`    // the chat's read position in Feishu, as of the last collection
	MentionPosition string `json:"mention_position,omitempty"` // the unread @me
	AtAll           bool   `json:"at_all,omitempty"`
	AtMe            bool   `json:"at_me,omitempty"` // an unread @me (not only @all): kept while the item is open
	// ReplyTo is the newest message from someone else; ThreadKind says where it sits: "" in the chat, "topic" in a
	// topic group's topic, "thread" in a thread of a normal group. A reply from the inbox goes there (the plugin's).
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

// MailAddress is a mail's sender or recipient.
type MailAddress = assistantwire.MailAddress

type InboxMail struct {
	AccountID string      `json:"account_id"`
	Mailbox   string      `json:"mailbox"`
	UIDs      []uint32    `json:"uids"` // oldest first
	MessageID string      `json:"message_id"`
	From      MailAddress `json:"from"`
	Direct    bool        `json:"direct,omitempty"` // to me, not cc / a list
	Thread    string      `json:"thread"`           // normalized subject: replies join the same item
}

func (it *InboxItem) open() bool { return it.DoneAt == nil }

// conversationOf is where a chat item opens in its messaging source: the source is the connector's id ("lark", the
// lark-connector extension's source of the same id), the account the one the apps would pass as ol_account.
func conversationOf(it *InboxItem) *wire.MessagingConversationRef {
	if it.Lark == nil || it.Lark.FeedID == "" || it.Source == "" {
		return nil
	}
	ref := &wire.MessagingConversationRef{Source: it.Source, ID: it.Lark.FeedID, StartPosition: it.Lark.StartPosition,
		ReadPosition: it.Lark.ReadPosition, MentionPosition: it.Lark.MentionPosition}
	if it.Lark.OLAccount > 0 {
		ref.Account = strconv.Itoa(it.Lark.OLAccount)
	}
	return ref
}

// listed is the item as the list shows it (no context).
func (it *InboxItem) listed() InboxItem {
	c := *it
	c.Context, c.Last, c.TriagedAt, c.BodyOf, c.JudgedAt, c.Rules, c.ArrivedOf = "", "", "", "", "", "", ""
	c.ArrivedAt = time.Time{}
	c.Meta, c.Group = nil, nil
	c.Mutes = mutesOf(it)
	c.Version = inboxVersion(it)
	c.Conversation = conversationOf(it)
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

// Listed is the item as GET /api/v1/inbox lists it (the hub's wire fixtures are made from it).
func (it InboxItem) Listed() InboxItem { return it.listed() }

// inboxBook is the inbox's state.
type inboxBook struct {
	lang        func() i18n.Lang // the user's language, for the reasons it writes on items (nil: the default)
	larkAccount int              // lark.account (InboxConfig.LarkAccount); zero is primary account 1
	mu          sync.Mutex
	items       map[string]*InboxItem
	once        sync.Once
	loadMu      sync.Mutex  // one load at a time
	loaded      atomic.Bool // what the database keeps was read: until then nothing is taken or written
	running     sync.Mutex  // one collection applied at a time
	saving      sync.Mutex  // snapshots and writes in the same order
	epoch       string      // the hub's run and its last round applied (InboxRound.Epoch, Seq), under running
	seq         uint64
	triaging    sync.Mutex // one triage pass at a time (inbox_triage.go)
	handing     sync.Mutex // one judge / agent pass at a time (inbox_agent.go)
	translating sync.Mutex // one translation pass at a time (inbox_translate.go)
	replies     sync.Map   // item id -> *inboxReply: replies to one item, one at a time (inbox_triage.go)
	mutes       []InboxMute
	mutesMu     sync.Mutex // one read-change-save of the kept mutes at a time
	cardSends   sync.Map   // a message card's id → the lock its 发送 holds (sendFromCard)
	// translations are the translations made, by model, text or mail and language (inbox_translate.go).
	translations translateCache

	// What the rounds and the hub's settings said of the sources (under srcMu, which is taken after mu when both
	// are): the plugins' connectors (their new items are triaged before they enter; InboxConfig.Extensions names them
	// from the start), the built-in ones a round read, how they describe their kinds, every connector's name.
	srcMu      sync.RWMutex
	extensions map[string]bool
	builtin    map[string]bool
	kinds      map[string]string // "<connector>/<kind>" → words
	names      map[string]string

	// admitting is one triage of a round's new plugin items at a time (admitInbox): a round meanwhile does not start
	// them again.
	admitting sync.Mutex
	batch     *inboxBatch          // while a collection runs: the items it starts, which enter once triaged
	held      map[string]inboxHeld // by source: the new items its last collection held back (inbox_admit.go)
	grouping  inboxGrouping        // the batches being related to events (inbox_group.go)

	cfgMu sync.Mutex
	cfg   assistantwire.InboxConfig // the hub's (InboxConfigMethod), as last read
	cfgAt time.Time
}

func (b *inboxBook) init() {
	b.once.Do(func() {
		b.items = map[string]*InboxItem{}
		b.extensions, b.builtin, b.kinds, b.names = map[string]bool{}, map[string]bool{}, map[string]string{}, map[string]string{}
	})
}

// fromExtension says whether a connector (an item's source) is a plugin's, as the rounds said: its new items are
// triaged before they enter (inbox_admit.go).
func (b *inboxBook) fromExtension(source string) bool {
	b.srcMu.RLock()
	defer b.srcMu.RUnlock()
	return b.extensions[source]
}

// builtinSource says whether a connector is one of the hub's own, as a round said. A connector's item from a source
// neither a round nor the settings named (the manager just started) has no fallback tier: never noise by default.
func (b *inboxBook) builtinSource(source string) bool {
	b.srcMu.RLock()
	defer b.srcMu.RUnlock()
	return b.builtin[source] && !b.extensions[source]
}

// knowExtensions takes the plugins' connectors the hub's settings name (InboxConfig.Extensions).
func (b *inboxBook) knowExtensions(ids []string) {
	b.srcMu.Lock()
	defer b.srcMu.Unlock()
	for _, id := range ids {
		b.extensions[id] = true
	}
}

// kindWords is how a plugin's connector describes one of its kinds of things ("" when it does not).
func (b *inboxBook) kindWords(source, kind string) string {
	b.srcMu.RLock()
	defer b.srcMu.RUnlock()
	return b.kinds[source+"/"+kind]
}

// userLang is the user's language for what b writes for people.
func (b *inboxBook) userLang() i18n.Lang {
	if b.lang == nil {
		return i18n.Default
	}
	return b.lang()
}

// byRules is an item's reason when its rules, not the model, filed it.
func (b *inboxBook) byRules() string { return i18n.T(b.userLang(), "assistant-inbox.by_rules") }

// mutedReason is an item's reason when mute name closed it.
func (b *inboxBook) mutedReason(name string) string {
	return i18n.T(b.userLang(), "assistant-inbox.muted", i18n.Args{"name": name})
}

// sourceName is an inbox source for people: 飞书, 邮件, a connector's name as the hub last said it.
func (b *inboxBook) sourceName(source string) string {
	switch source {
	case "lark":
		return i18n.T(b.userLang(), "assistant-inbox.source_lark")
	case "mail":
		return i18n.T(b.userLang(), "assistant-inbox.source_mail")
	}
	b.srcMu.RLock()
	defer b.srcMu.RUnlock()
	if n := b.names[source]; n != "" {
		return n
	}
	return source
}

// inboxStore is the database's inbox (table inbox_items); a store without it keeps the inbox in memory.
type inboxStore interface {
	PutInboxItem(ctx context.Context, id string, data []byte, updatedAt time.Time, doneAt *time.Time) error
	InboxItems(ctx context.Context, doneSince time.Time) ([][]byte, error)
	DeleteInboxItems(ctx context.Context, ids []string) error
}

// inboxBatchStore is an inboxStore that writes many items in one round trip (the database's PutInboxItems).
type inboxBatchStore interface {
	PutInboxItems(ctx context.Context, rows []db.InboxRow) error
}

func (s *Service) inboxDB() inboxStore {
	st, _ := s.store().(inboxStore)
	return st
}

// errInboxUnavailable: what the inbox keeps could not be read (CodeInboxUnavailable).
var errInboxUnavailable = &rpc.Error{Code: assistantwire.CodeInboxUnavailable, Message: "收件箱暂时读不到存着的内容，稍后再试"}

// inboxReady loads what the inbox keeps (its items and mutes) the first time anything needs it. A load that failed
// (the database did not answer) is errInboxUnavailable and is tried again the next time: until one goes through
// nothing is taken or written, so the items and mutes stored are not overwritten with what memory has.
func (s *Service) inboxReady(ctx context.Context) error {
	b := &s.inbox
	b.init()
	if b.loaded.Load() {
		return nil
	}
	b.loadMu.Lock()
	defer b.loadMu.Unlock()
	if b.loaded.Load() {
		return nil
	}
	if !s.loadInbox(ctx) {
		return errInboxUnavailable
	}
	b.loaded.Store(true)
	return nil
}

// inboxUp is inboxReady for a route: false when it answered 503 inbox_unavailable.
func (s *Service) inboxUp(w http.ResponseWriter, r *http.Request) bool {
	if err := s.inboxReady(r.Context()); err != nil {
		apierr.WriteCode(w, http.StatusServiceUnavailable, assistantwire.CodeInboxUnavailable, err)
		return false
	}
	return true
}

// loadInbox reads the items still remembered and the mutes; false when the database could not be read.
func (s *Service) loadInbox(ctx context.Context) bool {
	b := &s.inbox
	if !s.loadInboxMutes(ctx) {
		return false
	}
	st := s.inboxDB()
	if st == nil {
		return true
	}
	rows, err := st.InboxItems(ctx, time.Now().Add(-inboxForget))
	if err != nil {
		s.logf("inbox: load: %v", err)
		return false
	}
	cfg := s.inboxConfig(ctx)
	b.knowExtensions(cfg.Extensions)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.larkAccount = cfg.LarkAccount
	for _, raw := range rows {
		var it InboxItem
		if json.Unmarshal(raw, &it) == nil && it.ID != "" {
			if _, have := b.items[it.ID]; have {
				continue // newer in memory than in the database
			}
			b.larkRequestAccount(&it)
			if it.ArrivedOf == "" { // stored before arrivals were stamped
				it.ArrivedOf, it.ArrivedAt = it.Last, it.UpdatedAt
			}
			b.items[it.ID] = &it
		}
	}
	return true
}

// larkRequestAccount sets a Feishu item's OLAccount for the lark.account the hub runs with now.
func (b *inboxBook) larkRequestAccount(it *InboxItem) {
	if it.Lark == nil {
		return
	}
	it.Lark.OLAccount = 0
	if a := larkItemAccount(it); a != max(b.larkAccount, 1) {
		it.Lark.OLAccount = a
	}
}

// larkItemAccount is the lark-connector account a Feishu item came from; zero in rows from before accounts were recorded
// is account 1.
func larkItemAccount(it *InboxItem) int {
	if it.Lark == nil {
		return 0
	}
	return max(it.Lark.AccountID, 1)
}

// InboxCollected is inbox.collected: what one collection of the hub read. It applies what changed; the items a
// plugin's connector starts enter the list once triaged (admitInbox, in the background): until then nothing lists,
// stores or works on them. A round older than the last one applied of the hub's run is dropped (it was held up on the
// way); while what the inbox keeps can't be read the round is refused (errInboxUnavailable: the hub hands it again).
func (s *Service) InboxCollected(ctx context.Context, r assistantwire.InboxRound) error {
	if err := s.inboxReady(ctx); err != nil {
		return err
	}
	b := &s.inbox
	b.running.Lock()
	defer b.running.Unlock()
	if r.Seq > 0 {
		if r.Epoch == b.epoch && r.Seq <= b.seq {
			s.logf("inbox: round %d is older than %d, which was applied: dropped", r.Seq, b.seq)
			return nil
		}
		b.epoch, b.seq = r.Epoch, r.Seq
	}
	cfg := s.inboxConfig(ctx)
	b.knowExtensions(cfg.Extensions)
	b.srcMu.Lock()
	for id, name := range r.Names {
		b.names[id] = name
	}
	for _, src := range r.Sources {
		b.names[src.ID] = src.Name
		b.builtin[src.ID] = !src.Extension
		if src.Extension {
			b.extensions[src.ID] = true
		}
		for kind, d := range src.Kinds {
			b.kinds[src.ID+"/"+kind] = d
		}
	}
	b.srcMu.Unlock()
	b.mu.Lock()
	b.larkAccount = cfg.LarkAccount
	b.batch = newInboxBatch()
	b.mu.Unlock()
	var changed []*InboxItem
	// Mail (the hub's own), then the connectors in the order the hub has them.
	if r.Mail != nil {
		changed = append(changed, s.collectMail(ctx, r.Mail)...)
	}
	for _, src := range r.Sources {
		changed = append(changed, s.collectSource(ctx, src)...)
	}
	b.mu.Lock()
	batch := b.batch
	b.batch = nil
	batch.registered = r.Names
	for id, it := range b.items {
		if it.DoneAt != nil && time.Since(*it.DoneAt) > inboxForget {
			delete(b.items, id)
			b.replies.Delete(id)
		}
	}
	now := time.Now()
	changed = append(changed, b.applyMutes(now)...)
	changed, todo := b.sortBatch(batch, changed, now)
	b.mu.Unlock()
	s.saveInbox(ctx, changed)
	s.triageInbox(ctx)
	s.translateInbox(ctx)
	s.admitInbox(ctx, batch, todo)
	return nil
}

// saveInbox persists changed items and tells the hub (it tells the extensions and the apps).
func (s *Service) saveInbox(ctx context.Context, changed []*InboxItem) {
	b := &s.inbox
	if len(changed) == 0 {
		return
	}
	if !b.loaded.Load() { // not read yet: what is stored stays as it is (inboxReady)
		s.logf("inbox: %d changed items not saved: what is stored was not read", len(changed))
		return
	}
	var told []assistantwire.InboxChangedItem
	b.saving.Lock()
	b.mu.Lock()
	rows := make([]db.InboxRow, 0, len(changed))
	now := time.Now()
	for i, it := range changed {
		if current := b.items[it.ID]; current != it {
			continue // the item was forgotten or replaced while this save was waiting
		}
		it.arrived(now)
		raw, _ := json.Marshal(it)
		rows = append(rows, db.InboxRow{ID: it.ID, Data: raw, UpdatedAt: it.UpdatedAt, DoneAt: it.DoneAt})
		if i < 50 { // a first round's hundreds: the plugin reads the inbox if it wants them all
			told = append(told, assistantwire.InboxChangedItem{ID: it.ID, Source: it.Source, Kind: it.Kind, Title: it.Title})
		}
	}
	b.mu.Unlock()
	if len(rows) == 0 {
		b.saving.Unlock()
		return
	}
	if st := s.inboxDB(); st != nil {
		s.putInboxRows(ctx, st, rows)
	}
	b.saving.Unlock()
	s.inboxChanged(ctx, told)
}

// putInboxRows writes rows in one go when the store can (a round's hundreds are one round trip, not hundreds); when
// that fails, or the store can't, each row is written on its own so one that fails does not keep the others out.
func (s *Service) putInboxRows(ctx context.Context, st inboxStore, rows []db.InboxRow) {
	if bs, ok := st.(inboxBatchStore); ok && len(rows) > 1 {
		err := bs.PutInboxItems(ctx, rows)
		if err == nil {
			return
		}
		s.logf("inbox: save %d items at once: %v (one by one)", len(rows), err)
	}
	for _, r := range rows {
		if err := st.PutInboxItem(ctx, r.ID, r.Data, r.UpdatedAt, r.DoneAt); err != nil {
			s.logf("inbox: save %s: %v", r.ID, err)
		}
	}
}

// inboxChanged tells the hub what changed (EventInboxChanged): it tells the extensions and the apps.
func (s *Service) inboxChanged(ctx context.Context, items []assistantwire.InboxChangedItem) {
	if items == nil {
		items = []assistantwire.InboxChangedItem{}
	}
	s.hub.InboxChanged(ctx, assistantwire.InboxChanged{Items: items})
}

// ---- helpers ----

// threadIndex is the items (and the running round's) by thread, for a collection that looks up one thread per
// thing: openFor on each scanned the whole inbox, a week of chats for each of Feishu's few hundred unread ones.
// key is the item's thread among the ones looked up, false for an item of another source. Under b.mu.
type threadIndex struct {
	key func(*InboxItem) (string, bool)
	by  map[string][]*InboxItem
}

func (b *inboxBook) threads(key func(*InboxItem) (string, bool)) *threadIndex {
	x := &threadIndex{key: key, by: map[string][]*InboxItem{}}
	b.each(x.note)
	return x
}

// note files it under its thread: an item added, or one whose thread may have changed. An entry under a thread it
// has left is skipped by the lookups.
func (x *threadIndex) note(it *InboxItem) {
	if k, ok := x.key(it); ok && !slices.Contains(x.by[k], it) {
		x.by[k] = append(x.by[k], it)
	}
}

// openFor finds the open item of a thread (one this round started too); done is the newest done one.
func (x *threadIndex) openFor(thread string) (open, done *InboxItem) {
	for _, it := range x.by[thread] {
		if k, ok := x.key(it); !ok || k != thread {
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

// each calls f for every item, and every item the running round started, under b.mu.
func (b *inboxBook) each(f func(*InboxItem)) {
	for _, it := range b.items {
		f(it)
	}
	if b.batch != nil {
		for _, it := range b.batch.items {
			f(it)
		}
	}
}

// has says whether an item (or one the running round started) has id, under b.mu.
func (b *inboxBook) has(id string) bool {
	_, ok := b.items[id]
	return ok || b.batch != nil && b.batch.items[id] != nil
}

// add keeps an item a collection starts (from: the source it came from), under b.mu. A plugin connector's, while a
// round runs, goes to the round's batch and enters the list once triaged (admitInbox); any other straight in.
func (b *inboxBook) add(from string, it *InboxItem) {
	if b.batch == nil || !b.fromExtension(from) {
		b.items[it.ID] = it
		return
	}
	b.batch.items[it.ID] = it
	b.batch.from[it.ID] = from
}

// readSource notes that the running round read a source: its held-back count is recomputed after the round.
func (b *inboxBook) readSource(id, name string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.batch != nil {
		b.batch.read[id] = name
	}
}

// arrived stamps when the item came to hold what it holds (ArrivedAt), the first time it is listed or saved so. Under
// b.mu.
func (it *InboxItem) arrived(now time.Time) {
	if it.ArrivedOf != it.Last || it.ArrivedAt.IsZero() {
		it.ArrivedOf, it.ArrivedAt = it.Last, now
	}
}

// arrivedAfter: the item took what it holds after asOf (or has not been stamped yet: it is coming in now). Under b.mu.
func (it *InboxItem) arrivedAfter(asOf time.Time) bool {
	return it.ArrivedOf != it.Last || it.ArrivedAt.After(asOf)
}

func (it *InboxItem) done(at time.Time, by string) {
	t := at
	it.DoneAt, it.DoneBy = &t, by
}

// ---- mail ----

// collectMail applies a round's mail: each account's latest page, then the full text of the new mail (the round's
// new items too: they are triaged on it) and of items collected before their text was read.
func (s *Service) collectMail(ctx context.Context, read *assistantwire.InboxMailRead) []*InboxItem {
	b := &s.inbox
	b.readSource("mail", "邮箱")
	var changed []*InboxItem
	for _, a := range read.Accounts {
		changed = append(changed, b.applyMail(a, a.Messages, time.Now())...)
		b.mu.Lock()
		var unread []*InboxItem
		b.each(func(it *InboxItem) {
			if it.Mail != nil && it.Mail.AccountID == a.ID && it.open() && !it.Read && it.BodyOf != it.Last {
				unread = append(unread, it)
			}
		})
		b.mu.Unlock()
		for _, it := range unread {
			if s.readMailBody(ctx, a, it) {
				changed = append(changed, it)
			}
		}
	}
	return changed
}

// readMailBody replaces the list's 2 KB snippet with the newest message's text (HTML-only newsletters have nothing
// but CSS in their first bytes). The hub reads it peeking: the mail stays unread.
func (s *Service) readMailBody(ctx context.Context, a assistantwire.InboxMailAccount, it *InboxItem) bool {
	b := &s.inbox
	b.mu.Lock()
	if it.Mail == nil || len(it.Mail.UIDs) == 0 {
		b.mu.Unlock()
		return false
	}
	uid, mailbox, last := it.Mail.UIDs[len(it.Mail.UIDs)-1], it.Mail.Mailbox, it.Last
	b.mu.Unlock()
	m, err := s.inboxMailBody(ctx, assistantwire.InboxMailRef{AccountID: a.ID, UID: uid, Mailbox: mailbox})
	if err != nil {
		s.logf("inbox: read mail %s/%d: %v", a.Email, uid, err)
		return false
	}
	text := strings.TrimSpace(m.Text)
	b.mu.Lock()
	defer b.mu.Unlock()
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
func (b *inboxBook) applyMail(a assistantwire.InboxMailAccount, msgs []assistantwire.InboxMailSummary, now time.Time) []*InboxItem {
	threads := map[string][]assistantwire.InboxMailSummary{}
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
	index := b.threads(func(it *InboxItem) (string, bool) {
		if it.Mail == nil || it.Mail.AccountID != a.ID {
			return "", false
		}
		return it.Mail.Thread, true
	})
	for _, t := range order {
		list := threads[t]
		sort.Slice(list, func(i, j int) bool { return list[i].UID < list[j].UID })
		var unseen []assistantwire.InboxMailSummary
		for _, m := range list {
			d, _ := time.Parse(time.RFC3339, m.Date)
			if !m.Seen && now.Sub(d) <= inboxWindow {
				unseen = append(unseen, m)
			}
		}
		open, done := index.openFor(t)
		if open == nil && done != nil {
			last, _ := strconv.ParseUint(done.Last, 10, 32)
			var fresh []assistantwire.InboxMailSummary
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
			b.add("mail", it)
			index.note(it)
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

func mailContext(a assistantwire.InboxMailAccount, msgs []assistantwire.InboxMailSummary) string {
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

// ---- the connectors' ----

// collectSource applies one connector's things: its batches go to be related to events (in the background), a
// migrated connector's things with extra keep the items' own blocks, the others become generic items; then the open
// items its rules now leave out are filed.
func (s *Service) collectSource(ctx context.Context, src assistantwire.InboxSource) []*InboxItem {
	b := &s.inbox
	b.readSource(src.ID, src.Name)
	if len(src.Things) == 0 && !src.Settles {
		return nil
	}
	// Batches of new messages go to the model, not the list.
	things := s.relateBatches(ctx, src, src.Things)
	var own []connector.InboxThing // a migrated connector's things that keep the inbox's own item shape
	if src.Migrated {
		generic := things[:0:0]
		for _, t := range things {
			if t.Extra.HasBlock() { // only rule_tier (GitHub's, #2830): a generic item like any connector's
				own = append(own, t)
			} else {
				generic = append(generic, t)
			}
		}
		things = generic
	}
	var changed []*InboxItem
	b.mu.Lock()
	changed = append(changed, b.applyConnector(src.ID, src.Name, things, time.Now())...)
	changed = append(changed, b.applyMigrated(src.ID, own, time.Now())...)
	b.mu.Unlock()
	if src.Settles {
		changed = append(changed, s.settleConnector(ctx, src.ID)...)
	}
	return changed
}

// ---- API ----

var errNoInboxItem = errors.New("no such inbox item")

var errCannotReply = errors.New("this item cannot be replied to")

func (s *Service) inboxRoutes() {
	s.inboxMuteRoutes()
	s.inboxParentRoutes()
	s.inboxBulkRoutes()
	s.translateRoutes()
	// Items of connectors (GitHub…) only go to apps that draw them (?sources=all): an older app would show a PR as
	// a mail and send a reply as one. triage says how many new items wait for the model, and why (inbox_admit.go).
	s.handle("GET /api/v1/inbox", func(w http.ResponseWriter, r *http.Request) {
		if !s.inboxUp(w, r) {
			return
		}
		items, asOf := s.inboxListAt(r.URL.Query().Get("sources") == "all")
		apierr.WriteJSON(w, map[string]any{"items": items, "as_of": asOf, "triage": s.inboxTriage()})
	})
	s.handle("GET /api/v1/inbox/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !s.inboxUp(w, r) {
			return
		}
		b := &s.inbox
		b.mu.Lock()
		it, ok := b.items[r.PathValue("id")]
		var c InboxItem
		if ok {
			c = it.listed()
			c.Context = it.Context
		}
		b.mu.Unlock()
		if !ok {
			apierr.Write(w, http.StatusNotFound, errNoInboxItem)
			return
		}
		apierr.WriteJSON(w, struct {
			InboxItem
			ReplyTarget *inboxMailTarget `json:"reply_target,omitempty"`
		}{c, mailTarget(c)})
	})
	s.handle("POST /api/v1/inbox/{id}/done", func(w http.ResponseWriter, r *http.Request) {
		if !s.inboxUp(w, r) {
			return
		}
		var in struct {
			Done    bool `json:"done"`
			Dismiss bool `json:"dismiss"` // 忽略: filed as not for the user, done_by=dismissed
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			apierr.Write(w, http.StatusBadRequest, err)
			return
		}
		it, err := s.setInboxDone(r.Context(), r.PathValue("id"), in.Done, in.Dismiss)
		if err != nil {
			apierr.Write(w, http.StatusNotFound, err)
			return
		}
		apierr.WriteJSON(w, it)
	})
	s.handle("POST /api/v1/inbox/{id}/draft", func(w http.ResponseWriter, r *http.Request) {
		if !s.inboxUp(w, r) {
			return
		}
		var in struct {
			Instruction string `json:"instruction"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		text, err := s.draftReply(r.Context(), r.PathValue("id"), in.Instruction)
		switch {
		case errors.Is(err, errNoInboxItem):
			apierr.Write(w, http.StatusNotFound, err)
		case err != nil:
			apierr.Write(w, http.StatusBadGateway, err)
		default:
			apierr.WriteJSON(w, map[string]any{"text": text})
		}
	})
	s.handle("POST /api/v1/inbox/{id}/reply", func(w http.ResponseWriter, r *http.Request) {
		if !s.inboxUp(w, r) {
			return
		}
		var in struct {
			Text            string      `json:"text"`
			Changes         []DocChange `json:"changes"` // documents: status moves to make with the reply
			ClientID        string      `json:"client_id"`
			ExpectedUID     *uint32     `json:"expected_uid"`
			ExpectedVersion *string     `json:"expected_version"`
			Card            string      `json:"card"` // the message card it is sent from (sendFromCard)
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Text) == "" && len(in.Changes) == 0 {
			apierr.Write(w, http.StatusBadRequest, errors.New("text is required"))
			return
		}
		if !fullRole(r) {
			// Posted as the user, a "/agent …" comes back as their request and starts an agent (docs/lark-agent.md).
			if len(in.Changes) > 0 || assistantwire.AsksAgent(in.Text) {
				apierr.Write(w, http.StatusForbidden, errors.New("this device can only reply with text"))
				return
			}
		}
		send := func() (InboxItem, error) {
			return s.replyInboxChecked(r.Context(), r.PathValue("id"), strings.TrimSpace(in.Text), in.ClientID, in.ExpectedUID, in.ExpectedVersion, in.Changes...)
		}
		var it InboxItem
		var err error
		if in.Card != "" {
			it, _, err = s.sendFromCard(r.Context(), in.Card, r.PathValue("id"), send)
		} else {
			it, err = send()
		}
		switch {
		case errors.Is(err, errMailReplyTargetChanged):
			apierr.WriteBody(w, http.StatusConflict, map[string]any{"code": "mail_reply_target_changed", "error": i18n.T(i18n.OfWriter(w), "assistant-inbox.mail_target_changed"), "retryable": false, "delivered": "none"})
		case errors.Is(err, errNoInboxItem):
			apierr.Write(w, http.StatusNotFound, err)
		case replyInDoubt(err):
			// It may have gone out: the apps say so (delivered unknown); the same reply again is not sent twice.
			body := apierr.Body(http.StatusBadGateway, assistantwire.CodeAssistantInDoubt, err)
			body["delivered"], body["retryable"] = "unknown", false
			apierr.WriteBody(w, http.StatusBadGateway, body)
		case err != nil:
			apierr.Write(w, http.StatusBadGateway, err)
		default:
			apierr.WriteJSON(w, it)
		}
	})
	s.handle("POST /api/v1/inbox/{id}/doc/status", func(w http.ResponseWriter, r *http.Request) {
		if !s.inboxUp(w, r) {
			return
		}
		var in DocChange
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Ref == "" || in.Field == "" || in.Value == "" {
			apierr.Write(w, http.StatusBadRequest, errors.New("ref, field and value are required"))
			return
		}
		it, err := s.setDocStatus(r.Context(), r.PathValue("id"), in)
		switch {
		case errors.Is(err, errNoInboxItem):
			apierr.Write(w, http.StatusNotFound, err)
		case errors.Is(err, errNoDocRow):
			apierr.Write(w, http.StatusBadRequest, err)
		case err != nil:
			apierr.Write(w, http.StatusBadGateway, err)
		default:
			apierr.WriteJSON(w, it)
		}
	})
	// A pull to refresh: the hub reads every source now and hands it over before it answers.
	s.handle("POST /api/v1/inbox/refresh", func(w http.ResponseWriter, r *http.Request) {
		if !s.inboxUp(w, r) {
			return
		}
		if err := s.appCall(r.Context(), AppInbox, assistantwire.InboxCollect, map[string]any{"round": assistantwire.RoundFull}, nil); err != nil {
			s.logf("inbox: refresh: %v", err)
		}
		items, asOf := s.inboxListAt(r.URL.Query().Get("sources") == "all")
		apierr.WriteJSON(w, map[string]any{"items": items, "as_of": asOf, "triage": s.inboxTriage()})
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

func (s *Service) replyInboxChecked(ctx context.Context, id, text, clientID string, uid *uint32, version *string, changes ...DocChange) (InboxItem, error) {
	if uid == nil && version == nil {
		return s.replyInboxAs(ctx, id, text, clientID, changes...)
	}
	// Collection and checked sends must not change the target between validation and sending.
	b := &s.inbox
	b.running.Lock()
	defer b.running.Unlock()
	b.mu.Lock()
	it := b.items[id]
	var target *inboxMailTarget
	if it != nil {
		target = mailTarget(*it)
	}
	b.mu.Unlock()
	if it == nil {
		return InboxItem{}, errNoInboxItem
	}
	if target == nil || uid != nil && *uid != target.UID || version != nil && *version != target.Version {
		return InboxItem{}, errMailReplyTargetChanged
	}
	return s.replyInboxAs(ctx, id, text, clientID, changes...)
}

// inboxList is every open item and the recently done ones, newest first; connectors' only when all (rows stored
// before Source was set have none, so it is the connector block that tells).
func (s *Service) inboxList(all bool) []InboxItem {
	out, _ := s.inboxListAt(all)
	return out
}

// inboxListAt is inboxList and the moment it was read (as_of): everything listed arrived by then.
func (s *Service) inboxListAt(all bool) ([]InboxItem, time.Time) {
	b := &s.inbox
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-inboxKeepDone)
	out := make([]InboxItem, 0, len(b.items))
	for _, it := range b.items {
		if it.DoneAt != nil && it.DoneAt.Before(cutoff) || !all && it.Connector != nil {
			continue
		}
		it.arrived(now)
		out = append(out, it.listed())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, now
}

func (s *Service) setInboxDone(ctx context.Context, id string, done, dismiss bool) (InboxItem, error) {
	b := &s.inbox
	b.mu.Lock()
	it, ok := b.items[id]
	if !ok {
		b.mu.Unlock()
		return InboxItem{}, errNoInboxItem
	}
	if done {
		it.done(time.Now(), doneByOf(dismiss))
	} else {
		it.DoneAt, it.DoneBy = nil, ""
	}
	out := it.listed()
	b.mu.Unlock()
	s.saveInbox(ctx, []*InboxItem{it})
	return out, nil
}

// ---- what the hub asks of the inbox ----

// InboxItemJSON is inbox.item: one item as it is stored (an extension's inbox_item slot draws it).
func (s *Service) InboxItemJSON(ctx context.Context, id string) (json.RawMessage, bool) {
	if s.inboxReady(ctx) != nil {
		return nil, false
	}
	b := &s.inbox
	b.mu.Lock()
	defer b.mu.Unlock()
	it := b.items[id]
	if it == nil {
		return nil, false
	}
	raw, _ := json.Marshal(it)
	return raw, true
}

// InboxForget is inbox.forget: the user deleted a source's data; its items go too. It says how many went.
func (s *Service) InboxForget(ctx context.Context, source string) (int, error) {
	if err := s.inboxReady(ctx); err != nil {
		return 0, err
	}
	b := &s.inbox
	b.saving.Lock()
	b.mu.Lock()
	var ids []string
	for id, it := range b.items {
		if it.Source == source {
			ids = append(ids, id)
			delete(b.items, id)
			b.replies.Delete(id)
		}
	}
	b.mu.Unlock()
	if st := s.inboxDB(); st != nil && len(ids) > 0 {
		if err := st.DeleteInboxItems(ctx, ids); err != nil {
			b.saving.Unlock()
			return 0, err
		}
	}
	b.saving.Unlock()
	if len(ids) > 0 {
		s.inboxChanged(ctx, nil)
	}
	return len(ids), nil
}

// inboxReport is what the hub shows of the inbox (the snapshot's inbox): the AI 分身's conversations; nil while the
// inbox could not be read.
func (s *Service) inboxReport(ctx context.Context) *assistantwire.InboxReport {
	if s.inboxReady(ctx) != nil {
		return nil
	}
	b := &s.inbox
	b.mu.Lock()
	defer b.mu.Unlock()
	out := &assistantwire.InboxReport{Agents: []SessionRef{}}
	for _, it := range b.items {
		if a := it.Agent; a != nil && a.SessionID != "" {
			out.Agents = append(out.Agents, SessionRef{MachineID: a.MachineID, SessionID: a.SessionID})
		}
	}
	sort.Slice(out.Agents, func(i, j int) bool { return out.Agents[i].SessionID < out.Agents[j].SessionID })
	return out
}
