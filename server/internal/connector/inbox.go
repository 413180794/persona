package connector

import (
	"context"
	"time"
)

// Tier is the inbox's tier a connector's rules decide for a thing (InboxThing.Tier).
type Tier string

// Tiers: needs the user, worth knowing, noise.
const (
	TierAction Tier = "action"
	TierFYI    Tier = "fyi"
	TierNoise  Tier = "noise"
)

// Known says whether t is one of the tiers ("" is not: it leaves the tier to the model).
func (t Tier) Known() bool { return t == TierAction || t == TierFYI || t == TierNoise }

// Done reasons of an InboxThing (the inbox item's done_by).
const (
	DoneRead    = "read"    // over without the user: merged, closed, the request withdrawn
	DoneReplied = "replied" // the user answered (here or elsewhere)
)

// InboxThing is one thread a connector thinks may need the user. The hub turns things into inbox items: one open
// item per thread gathers its later moves until the user deals with it; a move after that starts a new item. What
// happens to an item afterwards (triage, the AI 分身, done, kept for a while) is the hub's, the same for every source.
type InboxThing struct {
	// Thread is the connector's stable key of the thread ("example-org/example-repo#12").
	Thread string `json:"thread"`
	// Kind is the connector's kind of thing ("review_requested"): prompts/inbox/kinds/<id>.md tells the models
	// what it means; Label is the row's tag for people (「请你 review」).
	Kind    string `json:"kind"`
	Label   string `json:"label,omitempty"`
	Title   string `json:"title,omitempty"`
	Sender  string `json:"sender,omitempty"`  // who made the newest move
	Preview string `json:"preview,omitempty"` // the newest move in a line
	Context string `json:"context,omitempty"` // the thread as text, for triage, drafting and the AI 分身
	// At is when the newest move happened; Last marks it, and a different Last adds to the open item.
	At   time.Time `json:"at,omitempty"`
	Last string    `json:"last,omitempty"`
	URL  string    `json:"url,omitempty"` // the thread on the web, opened in the app
	// OpenURL is a link the system opens instead (sms:…: 在信息里回复), kept apart from URL.
	OpenURL string `json:"open_url,omitempty"`
	// ReplyTo says where a reply goes and who sees it (「评论到 example-org/example-repo#12（仓库可见的人都能看到）」);
	// CanReply: Reply works for this thread.
	ReplyTo  string `json:"reply_to,omitempty"`
	CanReply bool   `json:"can_reply,omitempty"`
	// Tier is the tier the connector's rules decide (action, fyi, noise): the item gets it without a model. "" leaves it
	// to the model (a human's ordinary comment). Why says which rule, for people (「规则：真人 @ 你」).
	Tier Tier   `json:"tier,omitempty"`
	Why  string `json:"why,omitempty"`
	// Done ends the open item (DoneRead, DoneReplied): the thread is over; Why says how when it is not obvious
	// (「GitHub：后来成功了」).
	Done string `json:"done,omitempty"`
	// Read: seen somewhere else (the notification read on GitHub); an item that does not need the user is done.
	Read bool `json:"read,omitempty"`
	// Tags are what the thing is about, for muting and rules: its repository, chat, sender (TagRepo, TagChat,
	// TagSender: the row's menu offers to mute those), a CI failure's branch. The hub mutes by them for every source.
	Tags []Tag `json:"tags,omitempty"`
}

// Tag is one thing an inbox thing is about: Key (TagRepo…), the Value it matches by (an id), the Name people read.
type Tag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Name  string `json:"name,omitempty"`
}

// Tag keys. The user can mute by the first three (the inbox row's 静音这个仓库 / 会话 / 发件人).
const (
	TagRepo   = "repo"
	TagChat   = "chat"
	TagSender = "sender"
	TagBranch = "branch"
)

// Inbox is a connector with things for the inbox. Collect answers from the connector's memory (it is called on
// every inbox collection; a connector that polls does so in its own Watch and calls changed(ScopeInbox)). Reply
// sends text the user confirmed (the send button is the confirmation) to the thread.
type Inbox interface {
	Collect(ctx context.Context) ([]InboxThing, error)
	Reply(ctx context.Context, r InboxReply) error
}

// InboxReply is a reply to one of a connector's inbox items.
type InboxReply struct {
	Thread string `json:"thread"`
	ItemID string `json:"item_id,omitempty"` // the hub's inbox item
	Last   string `json:"last,omitempty"`    // the newest move the item holds (InboxThing.Last): what the reply answers
	Text   string `json:"text,omitempty"`
}

// InboxSettler is an inbox whose rules can close items the hub still has open (made under older rules, or of a
// thread the connector no longer brings in): Settle gets the open items' threads and returns, for each that is
// over, why. Nothing is deleted: the hub files them as done (read).
type InboxSettler interface {
	Settle(ctx context.Context, threads []string) map[string]string
}
