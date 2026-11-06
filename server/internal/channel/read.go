package channel

import (
	"context"
	"slices"
	"strings"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/transcript"
)

// Reading a run's answer out of the conversation is the same wherever the transcript comes from: the assistant reads it
// over the hub's bus (session.items), the voice extension over the hub's HTTP API (…/items?after_seq=, …/full). An
// ItemSource is that transcript; ReadTurn and RunAnswer are the one copy of the rules (docs/channels.md「跟踪这一轮」).

// ItemSource is one conversation's transcript, read a page at a time.
type ItemSource interface {
	// ItemsAfter is a page of the items after seq after (lazy images: the text is what counts). moreAfter: the page
	// was cut short, more follow after toSeq; toSeq: the last seq the page covers.
	ItemsAfter(ctx context.Context, after int) (items []proto.Item, moreAfter bool, toSeq int, err error)
	// Full is item id whole: a long item comes in a page cut (FullSize says it was).
	Full(ctx context.Context, id string) (proto.Item, error)
}

// How much ReadTurn reads at most: pages of the transcript, and the items it keeps for a run that names no message
// (the answer is at the end).
const (
	readPagesMost = 100
	readKeepMost  = 1000
)

// ReadTurn is what the conversation answered after r's base: after its message when r names one (MsgIDs, Probe). It
// reads to the end of the transcript (a long turn has more lines than one page), keeping the items from that message
// on, or the last ones without one; the answer's long items are read whole. r is not changed. An error: a page could
// not be read (Last is then r.Base).
func ReadTurn(ctx context.Context, src ItemSource, r *Run) (Reading, error) {
	holds := func(it proto.Item) bool {
		if strings.Contains(it.Text, r.Probe) {
			return true
		}
		if it.FullSize == 0 {
			return false
		}
		// A long message (a forwarded chat) is cut in pages, maybe before what the user wrote.
		full, err := src.Full(ctx, it.ID)
		return err == nil && strings.Contains(full.Text, r.Probe)
	}
	named := len(r.MsgIDs) > 0 || r.Probe != ""
	var kept []proto.Item
	found := !named
	after := r.Base
	for page := 0; page < readPagesMost; page++ {
		items, more, to, err := src.ItemsAfter(ctx, after)
		if err != nil {
			return Reading{Last: r.Base}, err
		}
		switch {
		case !found:
			// Its first message starts what is kept; a later page holding another of its messages (folded) adds to
			// it, not starts it again.
			if i, ok := r.Find(items, holds); ok {
				kept, found = slices.Clone(items[i:]), true
			}
		case named:
			kept = append(kept, items...)
		default:
			kept = append(kept, items...)
			if len(kept) > readKeepMost {
				kept = slices.Clone(kept[len(kept)-readKeepMost:])
			}
		}
		if to > after {
			after = to
		}
		if !more {
			break
		}
	}
	if named && found && !slices.ContainsFunc(kept, func(it proto.Item) bool { return it.Role == "assistant" }) {
		// Its message is in, nothing after it yet: its turn has not answered (the session's status can come before its
		// lines, two messages one right after the other): as one still waiting. Unless the turn was stopped before it
		// said anything (#4505): only the 已中断 notice follows it, and the turn is over without words.
		// Only once all of a folded run's messages are in (a later one's turn is still to come), and not for an API
		// error Claude Code is still retrying (the turn goes on).
		if why := Stopped(kept); why != "" && !retrying(kept) {
			if _, all := RunAnswer(r, kept); all {
				return Reading{Found: true, Last: after, Stopped: why}, nil
			}
		}
		return Reading{Last: after}, nil
	}
	answer, all := RunAnswer(r, kept)
	if !found || !all {
		return Reading{Last: after}, nil
	}
	var out []string
	for _, it := range answer {
		text := it.Text
		if it.FullSize > 0 {
			if full, err := src.Full(ctx, it.ID); err == nil && full.Text != "" {
				text = full.Text
			}
		}
		out = append(out, strings.TrimSpace(text))
	}
	return Reading{Answer: strings.Join(out, "\n\n"), Found: true, Last: after, Stopped: Stopped(kept), Words: endsInWords(kept)}, nil
}

// RunAnswer is the answer to r's messages in items (the transcript from the first of them on), and whether all of them
// are there. A run that took in later messages (folded: the owner sent another before the first was answered, a
// backlog handed over at once as the hub started) answers them together, once all are in the conversation: in the
// order the conversation has them (not the order they were sent: a message that had to start the conversation first
// comes later), each message's own answer — the words after it, up to the next of them. A part that ends without words
// (the conversation took the next message in mid-turn and went on) has its answer in the next one. Without client
// message ids on the items (an older node) they can't be told apart: one answer, the last words, as before.
func RunAnswer(r *Run, items []proto.Item) ([]proto.Item, bool) {
	if len(r.MsgIDs) < 2 {
		return Answer(items), true
	}
	ours := map[string]bool{}
	for _, id := range r.MsgIDs {
		ours[id] = true
	}
	var parts [][]proto.Item
	seen, cur, ids := map[string]bool{}, "", false
	for _, it := range items {
		ids = ids || it.ClientMsgID != ""
		if ours[it.ClientMsgID] && it.ClientMsgID != cur || len(parts) == 0 {
			if ours[it.ClientMsgID] {
				cur, seen[it.ClientMsgID] = it.ClientMsgID, true
			}
			parts = append(parts, nil)
		}
		parts[len(parts)-1] = append(parts[len(parts)-1], it)
	}
	if !ids {
		return Answer(items), true
	}
	if len(seen) < len(ours) {
		return nil, false // one of its messages is not in the conversation yet: its answer is still to come
	}
	var out []proto.Item
	for i, p := range parts {
		if i < len(parts)-1 && !endsInWords(p) {
			continue
		}
		out = append(out, Answer(p)...)
	}
	return out, true
}

// endsInWords: the last of items that says anything is the assistant's text (a turn that ended with its answer).
func endsInWords(items []proto.Item) bool {
	for i := len(items) - 1; i >= 0; i-- {
		it := items[i]
		switch {
		// The line a turn ends with (its duration) and a compaction say nothing of how it ended (as in Stopped).
		case it.Kind == proto.KindThinking, it.TurnDuration != nil, it.Kind == proto.KindText && strings.TrimSpace(it.Text) == "",
			it.Kind == proto.KindNotice && compaction(it):
			continue
		case it.Role == "assistant" && it.Kind == proto.KindText:
			return true
		}
		return false
	}
	return false
}

// retrying: the last notice in items is an API error Claude Code is still retrying (read on the whole text: Stopped
// clips it).
func retrying(items []proto.Item) bool {
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].Kind == proto.KindNotice && !compaction(items[i]) { // as Stopped skips them
			return strings.HasSuffix(strings.TrimSpace(items[i].Text), transcript.APIRetrying)
		}
	}
	return false
}
