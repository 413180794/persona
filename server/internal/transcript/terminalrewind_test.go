package transcript

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// #5396: a rewind in the terminal writes nothing, and the node can only see it on the screen. What it asks the file
// for (LastPrompt), how it tells the file (RewoundAt), and what undoes a wrong guess (Claude goes on from the turn
// taken for rewound) are tested here on synthetic transcripts.

func twoTurns() *tline {
	l := &tline{}
	l.user("u1", "", "一", 1)
	l.reply("a1", "u1", "答一", 2)
	l.turnEnd("t1", "a1", 3)
	l.user("u2", "t1", "二", 4)
	l.reply("a2", "u2", "答二", 5)
	l.turnEnd("t2", "a2", 6)
	return l
}

func TestLastPromptIsTheAnsweredEndedLastMessage(t *testing.T) {
	f := NewFile(writeLines(t, twoTurns()))
	if _, err := f.Refresh(); err != nil {
		t.Fatal(err)
	}
	p, ok := f.LastPrompt()
	if !ok || p.UUID != "u2" || p.Text != "二" || p.Before != "一" || p.First {
		t.Fatalf("%v %+v", ok, p)
	}

	// A recap written beside nothing (under the last turn) still leaves it the last prompt.
	l := twoTurns()
	l.awaySummary("s1", "t2", 7)
	f = NewFile(writeLines(t, l))
	f.Refresh()
	if p, ok := f.LastPrompt(); !ok || p.UUID != "u2" {
		t.Fatalf("with a recap under it: %v %+v", ok, p)
	}

	// Not answered (Esc before the reply, #3036), not ended, or something written since that is not part of its turn.
	for name, extra := range map[string]func(l *tline){
		"unanswered": func(l *tline) { l.user("u3", "t2", "三", 7) },
		"not ended":  func(l *tline) { l.user("u3", "t2", "三", 7); l.reply("a3", "u3", "答三", 8) },
		"a new chain": func(l *tline) {
			l.add(`{"type":"user","uuid":"c1","parentUuid":null,"isCompactSummary":true,"timestamp":"2026-11-05T10:00:09Z","message":{"role":"user","content":"summary"}}`)
		},
	} {
		l := twoTurns()
		extra(l)
		f := NewFile(writeLines(t, l))
		f.Refresh()
		if p, ok := f.LastPrompt(); ok {
			t.Fatalf("%s: %+v", name, p)
		}
	}
}

func TestRewoundAtLeavesTheTurnOutOnce(t *testing.T) {
	p := writeLines(t, twoTurns())
	f := NewFile(p)
	_, before, _ := f.RefreshSpan()
	span, ok := f.RewoundAt("u2")
	if !ok || span.History == before.History || !strings.HasSuffix(span.History, ".r1") {
		t.Fatalf("%v %+v (was %q)", ok, span, before.History)
	}
	want(t, allItems(f), "一", "答一")
	if _, ok := f.RewoundAt("u2"); ok {
		t.Fatal("rewound twice to the same message")
	}
	if _, ok := f.RewoundAt("nope"); ok {
		t.Fatal("rewound to a line the file does not have")
	}
	// The message before is the last one now, and it is the first.
	if lp, ok := f.LastPrompt(); !ok || lp.UUID != "u1" || !lp.First || lp.Before != "" {
		t.Fatalf("after: %v %+v", ok, lp)
	}

	// The user sends it again: it hangs from where they went back to, and the branch does not count a second time.
	more := &tline{}
	more.user("u3", "t1", "二", 7)
	more.reply("a3", "u3", "又答二", 8)
	appendLines(t, p, more)
	_, after, _ := f.RefreshSpan()
	if after.History != span.History {
		t.Fatalf("history %q → %q", span.History, after.History)
	}
	want(t, allItems(f), "一", "答一", "二", "又答二")
}

// Taken for rewound, but Claude goes on from that turn (the user typed the same words again as a new message, or the
// screen was misread): the turn comes back, and the token changes so the clients read it again.
func TestAMessageUnderTheTerminalRewoundTurnUndoesIt(t *testing.T) {
	for name, next := range map[string]func(l *tline){
		"a message": func(l *tline) { l.user("u3", "t2", "二", 7) },
		"a reply":   func(l *tline) { l.reply("a2b", "a2", "还有", 7) },
	} {
		t.Run(name, func(t *testing.T) {
			p := writeLines(t, twoTurns())
			f := NewFile(p)
			f.Refresh()
			span, _ := f.RewoundAt("u2")
			more := &tline{}
			next(more)
			appendLines(t, p, more)
			_, after, err := f.RefreshSpan()
			if err != nil {
				t.Fatal(err)
			}
			if after.History == span.History || strings.Contains(after.History, ".r") || after.From != 0 {
				t.Fatalf("history %q → %+v", span.History, after)
			}
			if got := texts(allItems(f)); len(got) != 5 || got[2] != "二" || got[3] != "答二" {
				t.Fatalf("items %q", got)
			}
			if len(f.RewoundLines()) != 0 {
				t.Fatalf("still rewound: %v", f.RewoundLines())
			}
		})
	}

	// A system line (a recap) under it is no such sign, and another root the node rewound to stays.
	l := twoTurns()
	l.user("u3", "t2", "三", 7)
	l.reply("a3", "u3", "答三", 8)
	l.turnEnd("t3", "a3", 9)
	p := writeLines(t, l)
	f := NewFile(p)
	f.Refresh()
	f.RewoundAt("u3")
	f.RewoundAt("u2")
	more := &tline{}
	more.awaySummary("s1", "t3", 10)
	appendLines(t, p, more)
	f.Refresh()
	want(t, allItems(f), "一", "答一")
	more = &tline{}
	more.user("u4", "t2", "三", 11) // goes on from u2's turn: u2 comes back, u3 (under it) stays rewound
	appendLines(t, p, more)
	_, span, _ := f.RefreshSpan()
	want(t, allItems(f), "一", "答一", "二", "答二", "三")
	if !strings.HasSuffix(span.History, ".r1") {
		t.Fatalf("history %q", span.History)
	}
}

// The quick read (Tail) undoes it the same way: the lines it was told about stop being left out.
func TestTailUndoesATerminalRewindClaudeWentOnFrom(t *testing.T) {
	l := twoTurns()
	f := NewFile(writeLines(t, l))
	f.Refresh()
	f.RewoundAt("u2")
	gone := f.RewoundLines()
	read := func(s string) (Meta, *Tail) {
		var m Meta
		tail := &Tail{}
		tail.Rewound(gone)
		for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
			tail.Line(&m, ParseLine(0, []byte(line), &m))
		}
		tail.Settle(&m)
		return m, tail
	}
	if m, tail := read(l.b.String()); m.LastText != "答一" {
		t.Fatalf("rewound: %q", m.LastText)
	} else if _, revoked := tail.Revoked(); revoked {
		t.Fatal("revoked")
	}
	l.user("u3", "t2", "三", 7)
	l.reply("a3", "u3", "答三", 8)
	if m, tail := read(l.b.String()); m.LastText != "答三" {
		t.Fatalf("went on: %q", m.LastText)
	} else if rest, revoked := tail.Revoked(); !revoked || len(rest) != 0 {
		t.Fatalf("went on: %v %v", rest, revoked)
	}
}

// The turn's own lines read again (the tree worked out again: a window further back, or a re-read after another root
// was taken back) are not Claude going on from it.
func TestATerminalRewoundTurnStaysAcrossRereads(t *testing.T) {
	l := twoTurns()
	l.user("u3", "t2", "三", 7)
	l.reply("a3", "u3", "答三", 8)
	l.turnEnd("t3", "a3", 9)
	p := writeLines(t, l)
	f := NewFile(p)
	f.Refresh()
	f.RewoundAt("u3")
	f.RewoundAt("u2")
	more := &tline{}
	more.user("u4", "t2", "四", 10) // under u2: it comes back, u3 stays out
	appendLines(t, p, more)
	f.Refresh()
	_, span, _ := f.RefreshSpan() // nothing new: nothing more taken back
	want(t, allItems(f), "一", "答一", "二", "答二", "四")
	if !strings.HasSuffix(span.History, ".r1") {
		t.Fatalf("history %q", span.History)
	}
	f.tree.prepend([]treeRec{{uuid: "x0", parent: ""}})
	if len(f.tree.revoked) != 0 {
		t.Fatalf("a rebuild took back %v", f.tree.revoked)
	}
}

// A big file is read from a window at its end, and the lines before it come when the client scrolls back
// (branchTree.prepend). Working the tree out again replays the rewound turn's own lines under its root: that is not
// Claude going on from it, and the turn stays out across the next refresh. The same for session.rewind's mark
// (File.Rewound, #4992).
func TestARewoundTurnStaysOutAfterPagingBack(t *testing.T) {
	defer func(w int64) { windowBytes = w }(windowBytes)
	windowBytes = 4 << 10
	pad := strings.Repeat("x", 3000)
	for name, mark := range map[string]func(f *File) bool{
		"RewoundAt": func(f *File) bool { _, ok := f.RewoundAt("u2"); return ok },
		"Rewound":   func(f *File) bool { _, ok := f.Rewound("二", 1); return ok },
	} {
		t.Run(name, func(t *testing.T) {
			l := &tline{}
			l.user("u1", "", "一", 1)
			l.reply("a1", "u1", "答一", 2)
			l.turnEnd("t1", "a1", 3)
			for i := range 6 {
				l.add(fmt.Sprintf(`{"type":"file-history-snapshot","messageId":"m%d","snapshot":{"pad":%q}}`, i, pad))
			}
			l.user("u2", "t1", "二", 4)
			l.reply("a2", "u2", "答二", 5)
			l.turnEnd("t2", "a2", 6)
			p := writeLines(t, l)
			f := NewFile(p)
			if _, _, err := f.RefreshSpan(); err != nil {
				t.Fatal(err)
			}
			if f.base == 0 {
				t.Fatal("the file was not read in a window")
			}
			if !mark(f) {
				t.Fatal("not marked")
			}
			marked := f.history()
			want(t, allItems(f), "一", "答一") // pages back to the start
			if len(f.tree.revoked) != 0 {
				t.Fatalf("paging back took back %v", f.tree.revoked)
			}
			_, span, err := f.RefreshSpan()
			if err != nil {
				t.Fatal(err)
			}
			if span.History != marked || !strings.HasSuffix(span.History, ".r1") {
				t.Fatalf("history %q → %q", marked, span.History)
			}
			want(t, allItems(f), "一", "答一")
			if len(f.RewoundLines()) != 3 {
				t.Fatalf("rewound lines %v", f.RewoundLines())
			}

			// Claude does go on from the turn afterwards: that line is new, and it takes the mark back.
			more := &tline{}
			more.reply("a2b", "a2", "还有", 7)
			appendLines(t, p, more)
			if _, span, _ = f.RefreshSpan(); strings.Contains(span.History, ".r") {
				t.Fatalf("history %q", span.History)
			}
			want(t, allItems(f), "一", "答一", "二", "答二", "还有")
		})
	}
}

// Two roots the node set, one under the other. Claude goes on from the inner one's turn: both come back (the outer
// turn is gone on from too). Two roots side by side: the one Claude went on from comes back, the other stays out across
// the re-read that takes the first back.
func TestTakingOneRootBackKeepsTheOthers(t *testing.T) {
	l := twoTurns()
	l.user("u3", "t2", "三", 7)
	l.reply("a3", "u3", "答三", 8)
	l.turnEnd("t3", "a3", 9)
	p := writeLines(t, l)
	f := NewFile(p)
	f.Refresh()
	f.RewoundAt("u3")
	f.RewoundAt("u2")
	more := &tline{}
	more.reply("a3b", "a3", "还有", 10)
	appendLines(t, p, more)
	f.RefreshSpan()
	f.RefreshSpan()
	want(t, allItems(f), "一", "答一", "二", "答二", "三", "答三", "还有")

	// Side by side: the second root is on a chain of its own (a compaction starts one), so nothing abandons it but the
	// mark.
	l = twoTurns()
	l.add(`{"type":"user","uuid":"c1","parentUuid":null,"isCompactSummary":true,"timestamp":"2026-11-05T10:00:09Z","message":{"role":"user","content":"summary"}}`)
	l.user("u5", "c1", "五", 10)
	l.reply("a5", "u5", "答五", 11)
	l.turnEnd("t5", "a5", 12)
	p = writeLines(t, l)
	f = NewFile(p)
	f.Refresh()
	f.RewoundAt("u2")
	f.RewoundAt("u5")
	more = &tline{}
	more.reply("a2b", "a2", "还有", 13) // Claude goes on from u2's turn
	appendLines(t, p, more)
	f.RefreshSpan()
	_, span, _ := f.RefreshSpan()
	want(t, allItems(f), "一", "答一", "二", "答二", "还有")
	if !strings.HasSuffix(span.History, ".r1") || len(f.tree.manual) != 1 || !f.tree.manual["u5"] {
		t.Fatalf("history %q, manual %v", span.History, f.tree.manual)
	}
}

// Claude goes on from one turn the node took for rewound: the tail takes back only that one. Another (session.rewind's,
// on a chain of its own here) stays out of the tail, and stays with the caller.
func TestTailTakesBackOnlyTheTurnClaudeWentOnFrom(t *testing.T) {
	l := twoTurns()
	l.apiError("e2", "a2", 7)
	l.add(`{"type":"user","uuid":"c1","parentUuid":null,"isCompactSummary":true,"timestamp":"2026-11-05T10:00:09Z","message":{"role":"user","content":"summary"}}`)
	l.user("u5", "c1", "五", 10)
	l.reply("a5", "u5", "答五", 11)
	l.turnEnd("t5", "a5", 12)
	f := NewFile(writeLines(t, l))
	f.Refresh()
	f.RewoundAt("u2")
	r1 := f.RewoundLines()
	f.RewoundAt("u5")
	gone := f.RewoundLines()
	l.reply("a5b", "a5", "还有", 13) // Claude goes on from u5's turn
	var m Meta
	tail := &Tail{}
	tail.Rewound(gone)
	for _, line := range strings.Split(strings.TrimSpace(l.b.String()), "\n") {
		tail.Line(&m, ParseLine(0, []byte(line), &m))
	}
	tail.Settle(&m)
	rest, revoked := tail.Revoked()
	if !revoked || strings.Join(sorted(rest), ",") != strings.Join(sorted(r1), ",") {
		t.Fatalf("rest %v (%v), want %v", rest, revoked, r1)
	}
	if !tail.tree.hidden["u2"] || !tail.tree.hidden["e2"] || tail.tree.hidden["a5"] || m.LastText != "还有" {
		t.Fatalf("hidden %v, last %q", tail.tree.hidden, m.LastText)
	}
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	slices.Sort(out)
	return out
}
