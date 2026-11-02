package connector

import (
	"context"
	"time"
)

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
	Thread string
	// Kind is the connector's kind of thing ("review_requested"): prompts/inbox/kinds/<id>.md tells the models
	// what it means; Label is the row's tag for people (「请你 review」).
	Kind, Label string
	Title       string
	Sender      string // who made the newest move
	Preview     string // the newest move in a line
	Context     string // the thread as text, for triage, drafting and the AI 分身
	// At is when the newest move happened; Last marks it, and a different Last adds to the open item.
	At   time.Time
	Last string
	URL  string // the thread on the web, opened in the app
	// OpenURL is a link the system opens instead (sms:…: 在信息里回复), kept apart from URL.
	OpenURL string
	// ReplyTo says where a reply goes and who sees it (「评论到 example-org/example-repo#12（仓库可见的人都能看到）」);
	// CanReply: Reply works for this thread.
	ReplyTo  string
	CanReply bool
	// Tier is what triage falls back to without a model ("action" for a review request); "" is noise.
	Tier string
	// Done ends the open item (DoneRead, DoneReplied): the thread is over.
	Done string
	// Read: seen somewhere else (the notification read on GitHub); an item that does not need the user is done.
	Read bool
}

// Inbox is a connector with things for the inbox. Collect answers from the connector's memory (it is called on
// every inbox collection; a connector that polls does so in its own Watch and calls changed(ScopeInbox)). Reply
// sends text the user confirmed (the send button is the confirmation) to the thread.
type Inbox interface {
	Collect(ctx context.Context) ([]InboxThing, error)
	Reply(ctx context.Context, thread, text string) error
}

// InboxReply is a reply to one of a connector's inbox items, as InboxReplier gets it.
type InboxReply struct {
	Thread string
	ItemID string // the hub's inbox item
	Last   string // the newest move the item holds (InboxThing.Last): what the reply answers
	Text   string
}

// InboxReplier is an inbox that answers with more than the thread and the text (which move it answers, which item);
// the hub uses it instead of Inbox.Reply when a connector has it.
type InboxReplier interface {
	ReplyItem(ctx context.Context, r InboxReply) error
}
