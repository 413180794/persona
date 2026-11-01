package node

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/herdr"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// Characterization tests of the TUI driving (permission mode, effort) and the screen reads (screen, screenBefore,
// the suggestion) against herdr: the keys pressed and the reads made, in order.

func sendKeys(pane string, keys ...string) string {
	return call("pane.send_keys", map[string]any{"pane_id": pane, "keys": keys})
}

// footerScreens are the empty-box fixture with its footer showing each permission mode.
func footerScreens(t *testing.T) map[string]string {
	t.Helper()
	base := screenFixture(t, "claude-2.1.312-empty-162.txt")
	const auto = "⏵⏵ auto mode on"
	if !strings.Contains(base, auto) {
		t.Fatal("fixture footer changed")
	}
	return map[string]string{
		"default":     strings.Replace(base, auto, "? for shortcuts", 1),
		"acceptEdits": strings.Replace(base, auto, "⏵⏵ accept edits on", 1),
		"plan":        strings.Replace(base, auto, "⏸ plan mode on", 1),
		"auto":        base,
	}
}

// cycleModes makes shift+tab on pane walk default → acceptEdits → plan → auto → default (no bypass in the cycle).
func cycleModes(t *testing.T, f *fakeHerdr, pane, from string) {
	screens := footerScreens(t)
	order := []string{"default", "acceptEdits", "plan", "auto"}
	var mu sync.Mutex
	at := 0
	for i, m := range order {
		if m == from {
			at = i
		}
	}
	f.setScreen(pane, screens[order[at]])
	f.after(func(c herdrCall) {
		if !isKeys("shift+tab")(c) || str(c.Params["pane_id"]) != pane {
			return
		}
		mu.Lock()
		at = (at + 1) % len(order)
		s := screens[order[at]]
		mu.Unlock()
		f.setScreen(pane, s)
	})
}

func TestHerdrTUISetModePressesShiftTabUntilTheFooterShowsIt(t *testing.T) {
	n, f, _ := herdrNode(t)
	cycleModes(t, f, "w1:p1", "default")
	if err := n.tuiSetMode(context.Background(), "w1:p1", "plan"); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, f.trace(), []string{
		readVisible("w1:p1"), sendKeys("w1:p1", "shift+tab"),
		readVisible("w1:p1"), sendKeys("w1:p1", "shift+tab"),
		readVisible("w1:p1"),
	})

	// Already there: one read.
	f.reset()
	if err := n.tuiSetMode(context.Background(), "w1:p1", "plan"); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, f.trace(), []string{readVisible("w1:p1")})

	// No footer (a dialog in front): refused after one read, nothing pressed.
	f.reset()
	f.setScreen("w1:p2", screenFixture(t, "claude-2.1.312-trust-162.txt"))
	if err := n.tuiSetMode(context.Background(), "w1:p2", "plan"); err == nil || !strings.Contains(err.Error(), "输入框") {
		t.Fatalf("err %v", err)
	}
	assertTrace(t, f.trace(), []string{readVisible("w1:p2")})
}

// bypassPermissions is not in a running session's cycle: len(cycle)+1 tries, then the error says so.
func TestHerdrTUISetModeGivesUpOnBypass(t *testing.T) {
	n, f, _ := herdrNode(t)
	cycleModes(t, f, "w1:p1", "auto")
	err := n.tuiSetMode(context.Background(), "w1:p1", "bypassPermissions")
	if err == nil || !strings.Contains(err.Error(), "全部放行") {
		t.Fatalf("err %v", err)
	}
	var want []string
	for range len(tuiModeCycle) + 1 {
		want = append(want, readVisible("w1:p1"), sendKeys("w1:p1", "shift+tab"))
	}
	assertTrace(t, f.trace(), want)
}

const (
	listPicker = "\n Effort\n\n   low\n ❯ medium\n   high\n\n Enter to set as default · s for this session only · Esc to cancel\n"
	slider     = "\n Effort  low ──●──── max\n\n ←/→ to adjust · Enter to confirm · Esc to cancel\n"
)

func effortSent(pane string, rights int) []string {
	keys := []string{"left", "left", "left", "left", "left", "left"}
	for range rights {
		keys = append(keys, "right")
	}
	return []string{
		readVisible(pane),
		call("pane.send_text", map[string]any{"pane_id": pane, "text": "/effort"}),
		sendKeys(pane, "enter"),
		readVisible(pane), // the picker
		sendKeys(pane, keys...),
	}
}

// The older list picker: to the left end, one right per level, "s" for this session only.
func TestHerdrTUISetEffortWithTheListPicker(t *testing.T) {
	n, f, _ := herdrNode(t)
	empty := screenFixture(t, "claude-2.1.312-empty-162.txt")
	done := empty + "\n  ⎿  Set effort level to high (this session only)\n"
	f.screenSteps("w1:p1", func(c herdrCall) bool { return isKeys("enter")(c) || isKeys("s")(c) }, empty, listPicker, done)
	if err := n.tuiSetEffort(context.Background(), "w1:p1", "high"); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, f.trace(), append(effortSent("w1:p1", 2), sendKeys("w1:p1", "s"), readVisible("w1:p1")))
}

// The slider (Claude Code 2.1.2xx): the same walk, Enter confirms. An effort set in the environment wins: an error.
func TestHerdrTUISetEffortWithTheSlider(t *testing.T) {
	n, f, _ := herdrNode(t)
	empty := screenFixture(t, "claude-2.1.312-empty-162.txt")
	overridden := empty + "\n  ⎿  CLAUDE_CODE_EFFORT_LEVEL=low overrides this session's effort\n"
	f.screenSteps("w1:p1", isKeys("enter"), empty, slider, overridden)
	err := n.tuiSetEffort(context.Background(), "w1:p1", "max")
	if err == nil || !strings.Contains(err.Error(), "CLAUDE_CODE_EFFORT_LEVEL=low") {
		t.Fatalf("err %v", err)
	}
	assertTrace(t, f.trace(), append(effortSent("w1:p1", 4), sendKeys("w1:p1", "enter"), readVisible("w1:p1")))
}

// A draft in the box: refused after one read. A picker that never opens: 15 reads, then esc.
func TestHerdrTUISetEffortRefusesADraftAndEscapesWithoutAPicker(t *testing.T) {
	n, f, _ := herdrNode(t)
	f.setScreen("w1:p1", screenFixture(t, "claude-2.1.312-typed-162.txt"))
	if err := n.tuiSetEffort(context.Background(), "w1:p1", "low"); err == nil {
		t.Fatal("typed /effort after a draft")
	}
	assertTrace(t, f.trace(), []string{readVisible("w1:p1")})

	f.reset()
	f.setScreen("w1:p2", screenFixture(t, "claude-2.1.312-empty-162.txt"))
	if err := n.tuiSetEffort(context.Background(), "w1:p2", "low"); err == nil || !strings.Contains(err.Error(), "/effort") {
		t.Fatalf("err %v", err)
	}
	want := []string{readVisible("w1:p2"),
		call("pane.send_text", map[string]any{"pane_id": "w1:p2", "text": "/effort"}), sendKeys("w1:p2", "enter")}
	for range 15 {
		want = append(want, readVisible("w1:p2"))
	}
	assertTrace(t, f.trace(), append(want, sendKeys("w1:p2", "esc")))
}

// screen: source is visible unless "recent"; lines are clamped (visible ≤ 400, recent ≤ 20000, ≤ 0 → 60); ansi
// reads with the escapes kept; the status is herdr's for the agent. A session herdr does not have: offline.
func TestHerdrScreenReads(t *testing.T) {
	n, f, _ := herdrNode(t)
	f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
	f.setScreen("w1:p1", "hello")
	n.agents[sidA] = herdr.Agent{Agent: "claude", PaneID: "w1:p1", AgentStatus: proto.StatusWorking}
	ctx := context.Background()
	for _, c := range []struct {
		lines  int
		source string
		ansi   bool
	}{{0, "", false}, {10, "visible", false}, {1000, "visible", false}, {0, "recent", false}, {50000, "recent", false},
		{-5, "scrollback", false}, {30, "recent", true}} {
		text, status, err := n.screen(ctx, sidA, c.lines, c.source, c.ansi)
		if err != nil || text != "hello" || status != proto.StatusWorking {
			t.Fatalf("%+v: %q %s %v", c, text, status, err)
		}
	}
	read := func(source string, lines int) string {
		return call("pane.read", map[string]any{"pane_id": "w1:p1", "source": source, "lines": lines})
	}
	assertTrace(t, f.trace(), []string{
		read("visible", 60), read("visible", 10), read("visible", 400), read("recent", 60), read("recent", 20000),
		read("visible", 60),
		call("pane.read", map[string]any{"pane_id": "w1:p1", "source": "recent", "lines": 30, "format": "ansi", "strip_ansi": false}),
	})

	f.reset()
	text, status, err := n.screen(ctx, sidB, 0, "", false)
	if err != nil || text != "" || status != proto.StatusOffline {
		t.Fatalf("offline: %q %s %v", text, status, err)
	}
	assertTrace(t, f.trace(), []string{call("session.snapshot", nil)})
}

func numbered(from, to int) string {
	var b strings.Builder
	for i := from; i <= to; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String()
}

// screenBefore: one read of herdr's whole scrollback (1000 lines), the page right before the anchor (the match
// nearest len-loaded when it repeats); at_limit once the page reaches the start of a full scrollback; with ansi a
// second read gives the same rows with colors, used only when the row counts agree.
func TestHerdrScreenBefore(t *testing.T) {
	n, f, _ := herdrNode(t)
	n.agents[sidA] = herdr.Agent{Agent: "claude", PaneID: "w1:p1"}
	ctx := context.Background()
	recent := call("pane.read", map[string]any{"pane_id": "w1:p1", "source": "recent", "lines": 1000})
	recentANSI := call("pane.read", map[string]any{"pane_id": "w1:p1", "source": "recent", "lines": 1000, "format": "ansi", "strip_ansi": false})

	f.setScreen("w1:p1", numbered(1, 30))
	got, err := n.screenBefore(ctx, sidA, []string{"line 21", "line 22"}, 5, 10, false)
	if err != nil || got["text"] != "line 16\nline 17\nline 18\nline 19\nline 20" || got["has_more"] != true || got["at_limit"] != false {
		t.Fatalf("page %v %v", got, err)
	}
	// Default 200 lines: from the top, nothing more, a short pane is not at herdr's limit.
	got, _ = n.screenBefore(ctx, sidA, []string{"line 3"}, 0, 28, false)
	if got["text"] != "line 1\nline 2" || got["has_more"] != false || got["at_limit"] != false {
		t.Fatalf("top %v", got)
	}
	assertTrace(t, f.trace(), []string{recent, recent})

	// A full scrollback: reaching its top is at_limit; an anchor not found is an empty page.
	f.reset()
	f.setScreen("w1:p1", numbered(1, 1000))
	got, _ = n.screenBefore(ctx, sidA, []string{"line 5"}, 200, 996, false)
	if got["has_more"] != false || got["at_limit"] != true || !strings.HasPrefix(got["text"].(string), "line 1\n") {
		t.Fatalf("limit %v", got)
	}
	got, _ = n.screenBefore(ctx, sidA, []string{"not there"}, 200, 0, false)
	if got["text"] != "" || got["has_more"] != false || got["at_limit"] != true {
		t.Fatalf("missing anchor %v", got)
	}

	// Redraws repeat the anchor: the copy nearest len-loaded is the one.
	f.setScreen("w1:p1", "a\nX\nb\nc\nX\nd\n")
	got, _ = n.screenBefore(ctx, sidA, []string{"X"}, 1, 5, false)
	if got["text"] != "a" {
		t.Fatalf("near the top: %v", got)
	}
	got, _ = n.screenBefore(ctx, sidA, []string{"X"}, 1, 2, false)
	if got["text"] != "c" {
		t.Fatalf("near the end: %v", got)
	}

	// ansi: the colored rows when they line up with the plain ones.
	f.reset()
	f.handle("pane.read", func(p map[string]any) any {
		text := "a\nb\nc\n"
		if p["format"] == "ansi" {
			text = "\x1b[1ma\x1b[0m\r\n\x1b[2mb\x1b[0m\r\nc\r\n"
		}
		return map[string]any{"read": map[string]any{"text": text}}
	})
	got, _ = n.screenBefore(ctx, sidA, []string{"c"}, 5, 1, true)
	if got["text"] != "\x1b[1ma\x1b[0m\n\x1b[2mb\x1b[0m" {
		t.Fatalf("ansi %q", got["text"])
	}
	f.handle("pane.read", func(p map[string]any) any {
		text := "a\nb\nc\n"
		if p["format"] == "ansi" {
			text = "new\r\na\r\nb\r\nc\r\n" // output arrived between the reads
		}
		return map[string]any{"read": map[string]any{"text": text}}
	})
	got, _ = n.screenBefore(ctx, sidA, []string{"c"}, 5, 1, true)
	if got["text"] != "a\nb" {
		t.Fatalf("misaligned ansi %q", got["text"])
	}
	assertTrace(t, f.trace(), []string{recent, recentANSI, recent, recentANSI})
}

// readSuggestion reads the screen with colors (grey text is the suggestion) until it shows one; a draft stops it.
func TestHerdrReadSuggestion(t *testing.T) {
	defer func(r []time.Duration) { suggestReads = r }(suggestReads)
	suggestReads = []time.Duration{0, 10 * time.Millisecond, 20 * time.Millisecond}
	n, f, _ := herdrNode(t)
	n.suggests = map[string]*suggestState{sidA: {}, sidB: {}}
	readANSI := func(pane string) string {
		return call("pane.read", map[string]any{"pane_id": pane, "source": "visible", "lines": 60, "format": "ansi", "strip_ansi": false})
	}
	// Nothing yet, then the suggestion.
	f.screenSteps("w1:p1", func(c herdrCall) bool { return c.Method == "pane.read" },
		screenFixture(t, "claude-2.1.312-empty-162.ansi"), screenFixture(t, "claude-2.1.312-suggestion-171.ansi"))
	n.readSuggestion(context.Background(), sidA, "w1:p1")
	if got := n.suggests[sidA].text; got != "add all three to the README" {
		t.Fatalf("suggestion %q", got)
	}
	assertTrace(t, f.trace(), []string{readANSI("w1:p1"), readANSI("w1:p1")})

	f.reset()
	f.setScreen("w1:p2", screenFixture(t, "claude-2.1.312-suggestion-typed-171.ansi"))
	n.readSuggestion(context.Background(), sidB, "w1:p2")
	if got := n.suggests[sidB].text; got != "" {
		t.Fatalf("over a draft: %q", got)
	}
	assertTrace(t, f.trace(), []string{readANSI("w1:p2")})
}
