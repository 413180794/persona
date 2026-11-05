package channel

import (
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// A message from a channel starts a run: the hub follows the conversation until the turn is over, then sends its
// answer (the assistant's last words, after its last tool call) back. The rules (docs/channels.md「跟踪这一轮」):
// the message is found by its client_msg_id (or a piece of its text, from older nodes); a message queued behind a
// running turn waits for its own turn; a turn that leaves background work answers now and again when that work
// reports back; a message sent while one waits on the same conversation is answered with it.

// Run is one turn a channel waits on, kept by the hub so a restart still answers it.
type Run struct {
	ID      string `json:"id"`
	Channel string `json:"channel"`
	// ReplyTo is the chat app's message it answers ("" for a channel that does not reply to one).
	ReplyTo   string `json:"reply_to,omitempty"`
	MachineID string `json:"machine_id"`
	SessionID string `json:"session_id"`
	// TargetKind is what the conversation is to the channel: conversation, or agent (the Feishu bot's /agents).
	TargetKind string `json:"target_kind,omitempty"`
	Label      string `json:"label,omitempty"`
	// Base is the conversation's last line before the message went in: the answer comes after it.
	Base int `json:"base"`
	// MsgIDs are the client_msg_ids of the messages it answers; Probe a piece of the first one's text, to find it
	// when the node does not tie items to their send.
	MsgIDs  []string  `json:"msg_ids,omitempty"`
	Probe   string    `json:"probe,omitempty"`
	Started time.Time `json:"started"`
	// Seen: the conversation was seen working since (until then a quiet one may just not have started).
	Seen bool `json:"seen,omitempty"`
	// Held: answered once; its background work goes on, and the turn that work starts is answered too.
	Held bool `json:"held,omitempty"`
	// Folded are earlier messages to the same conversation this answer covers too (sent while it worked).
	Folded []Folded `json:"folded,omitempty"`
	// Ext is the channel's own: the progress card, the reaction on the message.
	Ext map[string]string `json:"ext,omitempty"`
}

// Folded is an earlier message a run answers too.
type Folded struct {
	ReplyTo string            `json:"reply_to,omitempty"`
	Ext     map[string]string `json:"ext,omitempty"`
}

// How long a run waits, and on what.
const (
	RunMost       = 4 * time.Hour
	HeldMost      = 2 * time.Hour
	QuietGrace    = 20 * time.Second // not seen working: the turn may not have started
	EmptyGrace    = 2 * time.Minute  // no words yet and not seen working
	QueuedMost    = 10 * time.Minute // the message is not in the conversation yet: it waits behind another turn
	MissingAfter  = 3 * time.Minute  // the conversation is not listed (closed, deleted)
	OfflineGrace  = time.Minute
	HeldIdleMost  = 10 * time.Minute // a held run's background work ended without a word
	ProbeLen      = 24
	ProgressAfter = 5 * time.Second
	ProgressEvery = 3 * time.Second
	ProgressIdle  = 12 * time.Second // the elapsed time moves on even without new steps
	TypingEvery   = 5 * time.Second
)

// Fold adds an earlier run on the same conversation to r: one answer covers both messages.
func (r *Run) Fold(o *Run) {
	if o.Base < r.Base || r.Base == 0 {
		r.Base = o.Base
	}
	if o.Started.Before(r.Started) {
		r.Started = o.Started
	}
	r.Folded = append(append(r.Folded, o.Folded...), Folded{ReplyTo: o.ReplyTo, Ext: o.Ext})
	r.MsgIDs = append(slices.Clone(o.MsgIDs), r.MsgIDs...)
	if r.Probe == "" {
		r.Probe = o.Probe
	}
}

// Look is the conversation as it is now, for Check.
type Look struct {
	Now time.Time
	// Exists: the conversation is listed; Online: its machine is connected.
	Exists, Online bool
	Working        bool      // working, or starting
	Waiting        bool      // blocked on the owner, or a card waits for an answer
	Offline        bool      // the conversation's own status is offline
	Background     int       // background work still running (commands, Monitors, subagents)
	Lang           i18n.Lang // what the notes are written in (the user's language; zh-Hans when unknown)
}

// Action is what Check says to do.
type Action int

const (
	Wait   Action = iota
	Read          // read the answer now (then Decide)
	Finish        // stop waiting: Step.Note says why
)

// Step is Check's verdict.
type Step struct {
	Do   Action
	Note string
}

// Check looks at a run against the conversation now: wait, read the answer, or give up. It marks the run Seen when
// the conversation works.
func (r *Run) Check(l Look) Step {
	age := l.Now.Sub(r.Started)
	if r.Held && age > HeldMost {
		return Step{Do: Finish}
	}
	if age > RunMost {
		return Step{Do: Finish, Note: i18n.T(l.Lang, "channel-turns.gave_up", i18n.Args{"took": Duration(l.Lang, age)})}
	}
	if !l.Exists {
		if age > MissingAfter && l.Online {
			return Step{Do: Finish, Note: i18n.T(l.Lang, "channel-turns.gone")}
		}
		return Step{Do: Wait}
	}
	if l.Working {
		r.Seen = true
		return Step{Do: Wait}
	}
	if l.Waiting {
		return Step{Do: Wait} // waiting on the owner: what it asks goes out as an ask
	}
	if !r.Seen && age < QuietGrace {
		return Step{Do: Wait}
	}
	if l.Offline && age < OfflineGrace {
		return Step{Do: Wait}
	}
	return Step{Do: Read}
}

// Reading is what reading the conversation found.
type Reading struct {
	Answer string // the turn's answer ("" when it has no words)
	Found  bool   // the run's message is in the conversation
	Last   int    // the last line read
}

// Result is what to send when a run is over (or held: answered once, waiting on its background work).
type Result struct {
	Answer string
	Note   string
	Held   bool
	Last   int
	// Quiet: nothing to send (a held run whose background work ended without a word).
	Quiet bool
}

// Decide is, after reading, whether the run is answered: nil to keep waiting. found false past QueuedMost reads the
// conversation's last words instead: the caller reads again without the message (Reading of the whole tail).
func (r *Run) Decide(rd Reading, l Look) *Result {
	age := l.Now.Sub(r.Started)
	held := l.Background > 0
	if strings.TrimSpace(rd.Answer) == "" {
		switch {
		case held:
			return nil
		case r.Held && age < HeldIdleMost:
			return nil
		case r.Held:
			return &Result{Quiet: true, Last: rd.Last} // its background work ended without more to say
		case !r.Seen && age < EmptyGrace:
			return nil // not started yet (queued behind another message, a slow start)
		}
	}
	res := &Result{Answer: rd.Answer, Held: held, Last: rd.Last}
	if held {
		res.Note = i18n.T(l.Lang, "channel-turns.background", i18n.Args{"count": l.Background})
	}
	return res
}

// Hold makes r wait for the turn its background work starts, from last on: answered once already.
func (r *Run) Hold(last int, now time.Time) {
	if last > r.Base {
		r.Base = last
	}
	r.MsgIDs, r.Probe, r.Seen, r.Held, r.Started, r.Folded = nil, "", false, true, now, nil
}

// Find is where the run's message is in items: by its client_msg_id, else by its probe (holds says whether a user
// item holds it, for one cut for pages); 0, true for a run that has neither (everything after Base is new).
func (r *Run) Find(items []proto.Item, holds func(proto.Item) bool) (int, bool) {
	if len(r.MsgIDs) == 0 && r.Probe == "" {
		return 0, true
	}
	for i, it := range items {
		if it.ClientMsgID != "" && slices.Contains(r.MsgIDs, it.ClientMsgID) {
			return i, true
		}
	}
	if r.Probe == "" {
		return 0, false
	}
	if holds == nil {
		holds = func(it proto.Item) bool { return strings.Contains(it.Text, r.Probe) }
	}
	for i, it := range items {
		if it.Role == "user" && it.Kind == proto.KindText && holds(it) {
			return i, true
		}
	}
	return 0, false
}

// Answer picks a turn's answer out of its items: the assistant's texts after its last tool call, or after the last
// message it was given (a message taken mid-turn); without those, its last text.
func Answer(items []proto.Item) []proto.Item {
	var tail []proto.Item
	var last *proto.Item
	for i := range items {
		it := items[i]
		switch {
		case it.Role == "assistant" && it.Kind == proto.KindText && strings.TrimSpace(it.Text) != "":
			tail = append(tail, it)
			last = &items[i]
		case it.Kind == proto.KindToolUse || it.Kind == proto.KindToolResult || it.Kind == proto.KindNotice ||
			it.Role == "user" && it.Kind == proto.KindText:
			tail = nil
		}
	}
	if len(tail) == 0 && last != nil {
		return []proto.Item{*last}
	}
	return tail
}

// Background is how much background work a conversation still runs (commands, Monitors, subagents).
func Background(s proto.Session) int {
	n := len(s.Background)
	for _, a := range s.Subagents {
		if a.Running {
			n++
		}
	}
	return n
}

// Probe is a piece of what the owner wrote, to find the message in the transcript: its first line; else something
// only it has (fallbacks, in order).
func Probe(text string, fallbacks ...string) string {
	for _, s := range append([]string{text}, fallbacks...) {
		for _, l := range strings.Split(s, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				r := []rune(l)
				return string(r[:min(len(r), ProbeLen)])
			}
		}
	}
	return ""
}

// Duration is d for people in l: 「12 秒」「3 分 4 秒」「2 小时 5 分」.
func Duration(l i18n.Lang, d time.Duration) string {
	s := int(d.Round(time.Second).Seconds())
	switch {
	case s < 60:
		return i18n.T(l, "channel-turns.seconds", i18n.Args{"s": s})
	case s < 3600:
		return i18n.T(l, "channel-turns.minutes", i18n.Args{"m": s / 60, "s": s % 60})
	}
	return i18n.T(l, "channel-turns.hours", i18n.Args{"h": s / 3600, "m": s % 3600 / 60})
}
