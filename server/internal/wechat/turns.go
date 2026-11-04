package wechat

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// A turn the owner started from WeChat is followed over the hub's API until the conversation stops working; then
// its answer (the assistant's last words, after its last tool call) goes back to WeChat. The rules follow the Feishu
// bot's (hub/bots_runs.go): what the user sent is found by its client_msg_id (or a piece of its text, from older
// nodes), a message queued behind a running turn waits for its own turn, and a turn that leaves background work
// answers now and again when that work reports back.

type run struct {
	Machine string `json:"machine"`
	Session string `json:"session"`
	// Base is the session's last item before the message went in: the answer comes after it.
	Base int `json:"base"`
	// MsgIDs are the client_msg_ids of the owner's messages this turn answers; Probe a piece of the first one's text.
	MsgIDs  []string  `json:"msg_ids,omitempty"`
	Probe   string    `json:"probe,omitempty"`
	Started time.Time `json:"started"`
	// Seen: it was seen working (until then a quiet session may just not have started).
	Seen bool `json:"seen,omitempty"`
	// Held: answered once, waiting for the turn its background work starts.
	Held    bool      `json:"held,omitempty"`
	Missing time.Time `json:"missing,omitzero"`
}

const (
	runMax       = 4 * time.Hour
	heldMax      = 2 * time.Hour
	quietGrace   = 20 * time.Second
	emptyGrace   = 2 * time.Minute
	queuedMax    = 10 * time.Minute
	missingMax   = 3 * time.Minute
	typingEvery  = 5 * time.Second
	heldIdleDrop = 10 * time.Minute
)

// startRun follows a message sent to a session (msgID and probe find it; both "" for a conversation it started,
// whose whole transcript answers it). A message sent while an earlier one waits on the same session is answered with
// it: one answer for both.
func (b *Bot) startRun(ctx context.Context, machine, session string, base int, msgID, probe string) {
	b.mu.Lock()
	now := b.opt.Now()
	var ids []string
	if msgID != "" {
		ids = []string{msgID}
	}
	i := slices.IndexFunc(b.st.Runs, func(r *run) bool { return r.Machine == machine && r.Session == session })
	if i >= 0 && !b.st.Runs[i].Held {
		r := b.st.Runs[i]
		r.MsgIDs = append(r.MsgIDs, ids...)
		r.Base = min(r.Base, base)
	} else {
		if i >= 0 {
			b.st.Runs = slices.Delete(b.st.Runs, i, i+1)
		}
		b.st.Runs = append(b.st.Runs, &run{Machine: machine, Session: session, Base: base, MsgIDs: ids, Probe: probe, Started: now})
	}
	b.saveLocked(ctx)
	b.mu.Unlock()
	b.startTyping(ctx, session)
	b.poke()
}

// dropRuns forgets the turns waited on in a session (/stop).
func (b *Bot) dropRuns(ctx context.Context, machine, session string) {
	b.mu.Lock()
	b.st.Runs = slices.DeleteFunc(b.st.Runs, func(r *run) bool { return r.Machine == machine && r.Session == session })
	b.saveLocked(ctx)
	b.mu.Unlock()
	b.stopTyping(ctx, session)
}

// poke has the turns looked at now (a turn ended: agent.finished).
func (b *Bot) poke() {
	select {
	case b.nudge <- struct{}{}:
	default:
	}
}

func (b *Bot) watchTurns(ctx context.Context) {
	t := time.NewTicker(b.opt.Tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-b.nudge:
		}
		b.mu.Lock()
		runs := slices.Clone(b.st.Runs)
		bound := b.st.Creds != nil
		b.mu.Unlock()
		if !bound {
			continue
		}
		for _, r := range runs {
			b.check(ctx, r)
		}
		b.keepTyping(ctx)
	}
}

// check looks at one turn and answers it once it is over.
func (b *Bot) check(ctx context.Context, r *run) {
	now := b.opt.Now()
	hub := b.opt.Hub
	b.mu.Lock()
	snap := *r // a message sent meanwhile adds to r
	snap.MsgIDs = slices.Clone(r.MsgIDs)
	b.mu.Unlock()
	if now.Sub(snap.Started) > runMax || snap.Held && now.Sub(snap.Started) > heldMax {
		b.finish(ctx, r, "", "等了太久还没做完，不再在微信里等它了，到 App 里看结果。", false, 0)
		return
	}
	s, err := hub.Session(ctx, snap.Machine, snap.Session)
	if err != nil {
		return
	}
	if s == nil {
		b.mu.Lock()
		if r.Missing.IsZero() {
			r.Missing = now
		}
		gone := now.Sub(r.Missing) > missingMax
		b.mu.Unlock()
		if gone {
			b.finish(ctx, r, "", "这段对话不见了（被关掉或删掉了）。", false, 0)
		}
		return
	}
	b.mu.Lock()
	r.Missing = time.Time{}
	if s.Status == "working" || s.State == "working" || s.Starting {
		r.Seen = true
		b.mu.Unlock()
		return
	}
	seen := r.Seen
	b.mu.Unlock()
	if s.Status == "blocked" || s.State == "needs_you" || s.PendingInteractions > 0 {
		return // waiting on the owner: what it asks comes as agent.needs_you
	}
	age := now.Sub(snap.Started)
	if !seen && age < quietGrace {
		return
	}
	items, err := hub.ItemsAfter(ctx, snap.Machine, snap.Session, snap.Base)
	if err != nil {
		return
	}
	start, found := snap.find(items)
	if !found && age < queuedMax {
		return // still queued behind the turn that was running
	}
	if !found {
		start = 0
	}
	last := snap.Base
	if len(items) > 0 {
		last = items[len(items)-1].Seq
	}
	answer := b.answerText(ctx, r, turnAnswer(items[start:]))
	held := backgroundRunning(*s)
	if answer == "" {
		switch {
		case held:
			return
		case snap.Held && age < heldIdleDrop:
			return
		case snap.Held:
			b.finish(ctx, r, "", "", false, last) // its background work ended without more to say
			return
		case !seen && age < emptyGrace:
			return
		}
		answer = "做完了，没有文字回答。"
	}
	note := ""
	if held {
		note = fmt.Sprintf("后台还有 %d 个任务在跑，做完再告诉你。", max(len(s.Background), 1))
	}
	b.finish(ctx, r, answer, note, held, last)
}

// find is where the owner's message is in items: by its client_msg_id, else by its probe.
func (r *run) find(items []proto.Item) (int, bool) {
	if len(r.MsgIDs) == 0 && r.Probe == "" {
		return 0, true // a held turn: everything after Base is new
	}
	for i, it := range items {
		if it.ClientMsgID != "" && slices.Contains(r.MsgIDs, it.ClientMsgID) {
			return i, true
		}
	}
	if r.Probe == "" {
		return 0, false
	}
	for i, it := range items {
		if it.Role == "user" && it.Kind == proto.KindText && strings.Contains(it.Text, r.Probe) {
			return i, true
		}
	}
	return 0, false
}

// answerText is the answer's items as one text, the ones cut for pages read in full.
func (b *Bot) answerText(ctx context.Context, r *run, parts []proto.Item) string {
	var out []string
	for _, it := range parts {
		text := it.Text
		if it.FullSize > 0 {
			if full, err := b.opt.Hub.FullText(ctx, r.Machine, r.Session, it.ID); err == nil && full != "" {
				text = full
			}
		}
		if text = strings.TrimSpace(text); text != "" {
			out = append(out, text)
		}
	}
	return strings.Join(out, "\n\n")
}

// turnAnswer picks a turn's answer out of its items: the assistant's texts after its last tool call, or after the
// last message it was given (a message taken mid-turn); without those, its last text (hub/bots_runs.go).
func turnAnswer(items []proto.Item) []proto.Item {
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

func backgroundRunning(s proto.Session) bool {
	if len(s.Background) > 0 {
		return true
	}
	for _, a := range s.Subagents {
		if a.Running {
			return true
		}
	}
	return false
}

// finish sends a turn's answer (and a note) to the owner and ends the run, or — held — keeps it, from last on, for
// the turn its background work starts.
func (b *Bot) finish(ctx context.Context, r *run, answer, note string, held bool, last int) {
	text := strings.TrimSpace(strings.TrimSpace(answer) + "\n\n" + note)
	b.mu.Lock()
	if held {
		r.Base, r.MsgIDs, r.Probe, r.Seen, r.Held, r.Started = last, nil, "", false, true, b.opt.Now()
	} else {
		b.st.Runs = slices.DeleteFunc(b.st.Runs, func(x *run) bool { return x == r })
	}
	b.saveLocked(ctx)
	b.mu.Unlock()
	if !held {
		b.stopTyping(ctx, r.Session)
	}
	if text != "" && !b.tell(ctx, text) {
		log.Printf("wechat: an answer could not be sent")
	}
}

// typing is the owner's chat showing that the bot is writing, while a turn runs.
type typing struct {
	ticket string
	at     time.Time
}

func (b *Bot) startTyping(ctx context.Context, session string) {
	b.mu.Lock()
	c, creds, contextToken := b.client, b.st.Creds, b.st.Context
	_, on := b.typing[session]
	b.mu.Unlock()
	if c == nil || creds == nil || contextToken == "" || on {
		return
	}
	ticket, err := c.TypingTicket(ctx, creds.UserID, contextToken)
	if err != nil {
		return // presence is best effort
	}
	if c.SetTyping(ctx, creds.UserID, ticket, true) != nil {
		return
	}
	b.mu.Lock()
	b.typing[session] = &typing{ticket: ticket, at: b.opt.Now()}
	b.mu.Unlock()
}

// keepTyping shows it again every few seconds (WeChat clears it on its own).
func (b *Bot) keepTyping(ctx context.Context) {
	b.mu.Lock()
	c, creds := b.client, b.st.Creds
	var due []string
	now := b.opt.Now()
	for _, t := range b.typing {
		if now.Sub(t.at) >= typingEvery {
			due = append(due, t.ticket)
			t.at = now
		}
	}
	b.mu.Unlock()
	if c == nil || creds == nil {
		return
	}
	for _, ticket := range slices.Compact(due) {
		_ = c.SetTyping(ctx, creds.UserID, ticket, true)
	}
}

func (b *Bot) stopTyping(ctx context.Context, session string) {
	b.mu.Lock()
	t := b.typing[session]
	delete(b.typing, session)
	others := len(b.typing) > 0
	c, creds := b.client, b.st.Creds
	b.mu.Unlock()
	if t == nil || others || c == nil || creds == nil {
		return
	}
	_ = c.SetTyping(ctx, creds.UserID, t.ticket, false)
}
