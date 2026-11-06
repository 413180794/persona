package channel

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/transcript"
)

// pagedSource is a transcript read page items at a time (more_after while more follow), its cut items whole from full.
type pagedSource struct {
	items []proto.Item
	page  int
	full  map[string]string
	fail  error
	after []int    // the seq each page was asked after
	fulls []string // the items asked whole
}

func (p *pagedSource) ItemsAfter(_ context.Context, after int) ([]proto.Item, bool, int, error) {
	p.after = append(p.after, after)
	if p.fail != nil {
		return nil, false, 0, p.fail
	}
	var page []proto.Item
	for _, it := range p.items {
		if it.Seq <= after {
			continue
		}
		if len(page) == p.page {
			return page, true, page[len(page)-1].Seq, nil
		}
		page = append(page, it)
	}
	to := after
	if len(page) > 0 {
		to = page[len(page)-1].Seq
	}
	return page, false, to, nil
}

func (p *pagedSource) Full(_ context.Context, id string) (proto.Item, error) {
	p.fulls = append(p.fulls, id)
	text, ok := p.full[id]
	if !ok {
		return proto.Item{}, errors.New("no such item")
	}
	return proto.Item{ID: id, Text: text}, nil
}

// numbered numbers items from seq 1 on, with ids.
func numbered(items ...proto.Item) []proto.Item {
	for i := range items {
		items[i].Seq = i + 1
		if items[i].ID == "" {
			items[i].ID = "i" + string(rune('a'+i))
		}
	}
	return items
}

func userSays(id, text string) proto.Item {
	return proto.Item{Role: "user", Kind: proto.KindText, Text: text, ClientMsgID: id}
}

func agentSays(text string) proto.Item {
	return proto.Item{Role: "assistant", Kind: proto.KindText, Text: text}
}

var (
	toolUse    = proto.Item{Role: "assistant", Kind: proto.KindToolUse, ToolName: "Bash"}
	toolResult = proto.Item{Role: "user", Kind: proto.KindToolResult}
)

func texts(items []proto.Item) string {
	var out []string
	for _, it := range items {
		out = append(out, it.Text)
	}
	return strings.Join(out, "|")
}

// A turn longer than a page is read to the end: from the run's base, its message found on one page and its answer on
// a later one; another turn before the message (someone else's) is not its answer.
func TestReadTurnPagesToTheEnd(t *testing.T) {
	src := &pagedSource{page: 2, items: numbered(
		userSays("wechat-9", "someone else's"), agentSays("their answer"),
		userSays("voice-1", "fix the test"), toolUse, toolResult, agentSays("looking"), toolUse, toolResult,
		agentSays("Fixed."), agentSays("All green."))}
	rd, err := ReadTurn(context.Background(), src, &Run{Base: 0, MsgIDs: []string{"voice-1"}})
	if err != nil || !rd.Found || rd.Answer != "Fixed.\n\nAll green." || rd.Last != 10 {
		t.Fatalf("%+v %v", rd, err)
	}
	if !slices.Equal(src.after, []int{0, 2, 4, 6, 8}) {
		t.Fatalf("pages asked after %v", src.after)
	}
	// From a base past the other turn: the pages start there.
	src.after = nil
	if rd, _ := ReadTurn(context.Background(), src, &Run{Base: 2, MsgIDs: []string{"voice-1"}}); rd.Answer != "Fixed.\n\nAll green." || src.after[0] != 2 {
		t.Fatalf("%+v after %v", rd, src.after)
	}
	// A run that names no message (held: its background work's turn) takes the last words after its base.
	if rd, _ := ReadTurn(context.Background(), src, &Run{Base: 6}); !rd.Found || rd.Answer != "Fixed.\n\nAll green." {
		t.Fatalf("unnamed: %+v", rd)
	}
}

// The answer's cut items are read whole (/full); a message found by its probe may be cut before what the user wrote.
func TestReadTurnReadsCutItemsWhole(t *testing.T) {
	long := agentSays("The answer, cut…")
	long.ID, long.FullSize = "long", 4000
	forwarded := proto.Item{ID: "fwd", Role: "user", Kind: proto.KindText, Text: "[a forwarded chat, cut…]", FullSize: 9000}
	src := &pagedSource{page: 50, full: map[string]string{"long": "The answer, whole.", "fwd": "[a forwarded chat]\nsum it up"},
		items: numbered(userSays("", "earlier"), agentSays("old"), forwarded, toolUse, toolResult, long)}
	rd, err := ReadTurn(context.Background(), src, &Run{Probe: "sum it up"})
	if err != nil || !rd.Found || rd.Answer != "The answer, whole." {
		t.Fatalf("%+v %v", rd, err)
	}
	if !slices.Contains(src.fulls, "fwd") || !slices.Contains(src.fulls, "long") {
		t.Fatalf("asked whole: %v", src.fulls)
	}
	// The whole item can't be read: the page's text is the answer.
	delete(src.full, "long")
	if rd, _ := ReadTurn(context.Background(), src, &Run{Probe: "sum it up"}); rd.Answer != "The answer, cut…" {
		t.Fatalf("%+v", rd)
	}
}

// Not answered yet: the message not in the conversation, in but nothing after it, or one of a folded run's messages
// not in; a page that can't be read is an error, read again later from the same base.
func TestReadTurnWaits(t *testing.T) {
	src := &pagedSource{page: 10, items: numbered(userSays("voice-1", "one"), agentSays("answer one"), userSays("voice-2", "two"))}
	for name, r := range map[string]*Run{
		"not in":            {MsgIDs: []string{"voice-3"}},
		"no words after it": {MsgIDs: []string{"voice-2"}},
		"one of two not in": {MsgIDs: []string{"voice-1", "voice-3"}},
	} {
		if rd, err := ReadTurn(context.Background(), src, r); err != nil || rd.Found || rd.Answer != "" || rd.Last != 3 {
			t.Errorf("%s: %+v %v", name, rd, err)
		}
	}
	src.fail = errors.New("the machine is away")
	if rd, err := ReadTurn(context.Background(), src, &Run{Base: 2, MsgIDs: []string{"voice-2"}}); err == nil || rd.Last != 2 {
		t.Fatalf("%+v %v", rd, err)
	}
}

// A message sent while an earlier one waited is folded into one run: once both are in, each message's own answer, in
// the conversation's order, read across pages.
func TestReadTurnFoldedAcrossPages(t *testing.T) {
	first := &Run{Base: 1, MsgIDs: []string{"voice-1"}, Started: t0}
	second := &Run{Base: 4, MsgIDs: []string{"voice-2"}, Started: t0.Add(30e9)}
	second.Fold(first)
	src := &pagedSource{page: 2, items: numbered(agentSays("before"),
		userSays("voice-1", "fix it"), toolUse, toolResult, agentSays("fixed"),
		userSays("voice-2", "and lint"), toolUse, toolResult, agentSays("lint clean"))}
	rd, err := ReadTurn(context.Background(), src, second)
	if err != nil || !rd.Found || rd.Answer != "fixed\n\nlint clean" {
		t.Fatalf("%+v %v", rd, err)
	}
	if src.after[0] != 1 {
		t.Fatalf("read from %d, not the earlier base", src.after[0])
	}
}

// A folded run's answer: each of its messages' own answer, in the conversation's order, once all are in; a part the
// conversation went on from (it took the next message in mid-turn) has its answer in the next one; an older node
// (no client message ids on the items) gets the last words, as before.
func TestFoldedRunAnswer(t *testing.T) {
	r := &Run{MsgIDs: []string{"wechat-1", "wechat-2"}}
	for _, c := range []struct {
		name  string
		items []proto.Item
		want  string
		all   bool
	}{
		{"two turns", []proto.Item{userSays("wechat-1", "一"), toolUse, toolResult, agentSays("答一"), userSays("wechat-2", "二"), toolUse, toolResult, agentSays("答二")}, "答一|答二", true},
		{"the later one first", []proto.Item{userSays("wechat-2", "二"), agentSays("答二"), userSays("wechat-1", "一"), agentSays("答一")}, "答二|答一", true},
		{"taken in mid-turn", []proto.Item{userSays("wechat-1", "一"), toolUse, userSays("wechat-2", "二"), toolResult, agentSays("一起答")}, "一起答", true},
		{"one not in yet", []proto.Item{userSays("wechat-1", "一"), agentSays("答一")}, "", false},
		{"an older node", []proto.Item{userSays("", "一"), agentSays("答一"), userSays("", "二"), agentSays("答二")}, "答二", true},
	} {
		got, all := RunAnswer(r, c.items)
		if all != c.all || texts(got) != c.want {
			t.Errorf("%s: %q, %v; want %q, %v", c.name, texts(got), all, c.want, c.all)
		}
	}
	if got, _ := RunAnswer(&Run{MsgIDs: []string{"wechat-1"}}, []proto.Item{userSays("wechat-1", "一"), agentSays("答一"), userSays("x", "y"), agentSays("答y")}); texts(got) != "答y" {
		t.Errorf("one message: %q (as before: the last words)", texts(got))
	}
}

// A turn stopped before the agent said anything (#4505): its message is followed only by the 已中断 notice. The turn is
// over: found, without words, stopped, so the run ends now instead of waiting as one still queued.
func TestReadTurnStoppedBeforeWords(t *testing.T) {
	stopped := proto.Item{Role: "system", Kind: proto.KindNotice, Text: transcript.InterruptedNotice}
	src := &pagedSource{page: 10, items: numbered(userSays("voice-1", "one"), agentSays("answer one"), userSays("voice-2", "two"), stopped)}
	rd, err := ReadTurn(context.Background(), src, &Run{MsgIDs: []string{"voice-2"}})
	if err != nil || !rd.Found || rd.Answer != "" || rd.Stopped == "" || rd.Last != 4 {
		t.Fatalf("%+v %v", rd, err)
	}
	// A folded run whose next message is not in yet still waits: that message's turn is still to come.
	if rd, _ := ReadTurn(context.Background(), src, &Run{MsgIDs: []string{"voice-2", "voice-3"}}); rd.Found {
		t.Fatalf("a folded run with a message still to come: %+v", rd)
	}
	// Once both are in and the turn was stopped before any words, the folded run is over too.
	folded := &pagedSource{page: 10, items: numbered(userSays("voice-1", "one"), userSays("voice-2", "two"), stopped)}
	if rd, _ := ReadTurn(context.Background(), folded, &Run{MsgIDs: []string{"voice-1", "voice-2"}}); !rd.Found || rd.Stopped == "" {
		t.Fatalf("a folded run stopped before words: %+v", rd)
	}
	// A long API error still being retried: its suffix is read on the whole text, not the clipped reason.
	long := proto.Item{Role: "system", Kind: proto.KindNotice, Text: transcript.APIErrorPrefix + strings.Repeat("x", 400) + transcript.APIRetrying}
	src2 := &pagedSource{page: 10, items: numbered(userSays("voice-2", "two"), long)}
	if rd, _ := ReadTurn(context.Background(), src2, &Run{MsgIDs: []string{"voice-2"}}); rd.Found {
		t.Fatalf("a long API error being retried: %+v", rd)
	}
	// An API error Claude Code is still retrying is not the end of the turn.
	src.items[3] = proto.Item{Seq: 4, ID: "id", Role: "system", Kind: proto.KindNotice, Text: transcript.APIErrorPrefix + "Overloaded（正在重试）"}
	if rd, _ := ReadTurn(context.Background(), src, &Run{MsgIDs: []string{"voice-2"}}); rd.Found {
		t.Fatalf("an API error being retried: %+v", rd)
	}
	// Some other notice after it (not a stop) still waits.
	src.items[3] = proto.Item{Seq: 4, ID: "id", Role: "system", Kind: proto.KindNotice, Text: "something else"}
	if rd, _ := ReadTurn(context.Background(), src, &Run{MsgIDs: []string{"voice-2"}}); rd.Found {
		t.Fatalf("a plain notice is not a stop: %+v", rd)
	}
}
