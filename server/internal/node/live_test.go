//go:build herdrlive

package node

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/herdr"
	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/screen"
	"github.com/nwverse/persona/server/internal/transcript"
)

// Against a real claude pane: PERSONA_LIVE_PANE=<pane> PERSONA_LIVE_WANT=<code or ""> [PERSONA_LIVE_CLEAR=<draft>]
// go test -tags herdrlive -run TestLiveCheckPane ./internal/node/
func TestLiveCheckPane(t *testing.T) {
	pane := os.Getenv("PERSONA_LIVE_PANE")
	if pane == "" {
		t.Skip("PERSONA_LIVE_PANE not set")
	}
	n := &Node{hosts: herdrHosts(herdr.New())}
	err := n.checkPane(context.Background(), pane, "claude", os.Getenv("PERSONA_LIVE_CLEAR"))
	code := ""
	if err != nil {
		code, _ = refusalOf(t, err)
	}
	t.Logf("pane %s: %v", pane, err)
	if want := os.Getenv("PERSONA_LIVE_WANT"); code != want {
		t.Fatalf("code %q, want %q", code, want)
	}
}

// PERSONA_LIVE_PANE=<pane> PERSONA_LIVE_OPTION=<key>: answers the pane's dialog the way a card does — rev checked, keys
// worked out from the screen now — and presses them.
func TestLiveAnswerDialog(t *testing.T) {
	pane, option := os.Getenv("PERSONA_LIVE_PANE"), os.Getenv("PERSONA_LIVE_OPTION")
	if pane == "" || option == "" {
		t.Skip("PERSONA_LIVE_PANE / PERSONA_LIVE_OPTION not set")
	}
	n := &Node{hosts: herdrHosts(herdr.New()), agents: map[string]host.Agent{"live": {PaneID: pane}}}
	ctx := context.Background()
	text, err := n.readPane(ctx, pane, "visible", screen.DialogLines)
	if err != nil {
		t.Fatal(err)
	}
	d := screen.ParseDialog(text)
	if d == nil {
		t.Fatalf("no dialog on\n%s", text)
	}
	t.Logf("dialog %q rev %s options %+v", d.Question, d.Rev, d.Options)
	if _, _, err := n.dialogKeys(ctx, "live", "000000000000", option); err == nil {
		t.Fatal("a wrong revision must refuse")
	}
	keys, pre, err := n.dialogKeys(ctx, "live", d.Rev, option)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("keys %v pre %v", keys, pre)
	if err := n.sendKeys(ctx, pane, keys...); err != nil {
		t.Fatal(err)
	}
}

// PERSONA_LIVE_PANE=<pane> PERSONA_LIVE_CHECK=<keys, comma-separated> [PERSONA_LIVE_TEXT=<free text>]: answers the
// multi-select question on the pane the way a card does (dialogmulti.go) — rev checked, one key at a time from
// whatever the terminal shows — and checks the question is gone afterwards (submitted, or the next one shown).
func TestLiveMultiSelect(t *testing.T) {
	pane, check := os.Getenv("PERSONA_LIVE_PANE"), os.Getenv("PERSONA_LIVE_CHECK")
	if pane == "" {
		t.Skip("PERSONA_LIVE_PANE not set")
	}
	n := &Node{hosts: herdrHosts(herdr.New()), agents: map[string]host.Agent{"live": {PaneID: pane}}}
	ctx := context.Background()
	text, err := n.readPane(ctx, pane, "visible", screen.DialogLines)
	if err != nil {
		t.Fatal(err)
	}
	d := screen.ParseDialog(text)
	if d == nil || !d.Multi {
		t.Fatalf("no multi-select question on\n%s", text)
	}
	t.Logf("question %q rev %s cursor %s steps %+v", d.Question, d.Rev, d.Cursor, d.Steps)
	var want []string
	if check != "" {
		want = strings.Split(check, ",")
	}
	if err := n.dialogMulti(ctx, "live", "000000000000", want, ""); err == nil {
		t.Fatal("a wrong revision must refuse")
	}
	start := time.Now()
	if err := n.dialogMulti(ctx, "live", d.Rev, want, os.Getenv("PERSONA_LIVE_TEXT")); err != nil {
		t.Fatal(err)
	}
	t.Logf("answered in %s", time.Since(start).Round(time.Millisecond))
	time.Sleep(time.Second)
	text, _ = n.readPane(ctx, pane, "visible", screen.DialogLines)
	if after := screen.ParseDialog(text); after != nil && after.Rev == d.Rev {
		t.Fatalf("the question is still there:\n%s", text)
	}
}

// PERSONA_LIVE_PANE=<pane> PERSONA_LIVE_OPTION=<1 or 2> on Claude Code's dangerous-command prompt with its countdown
// running (#564): the card's revision is taken, the clock moves on for a few seconds (as a person reads the card),
// and the answer still goes in; afterwards the prompt is gone.
func TestLiveAnswerDangerousPrompt(t *testing.T) {
	pane, option := os.Getenv("PERSONA_LIVE_PANE"), os.Getenv("PERSONA_LIVE_OPTION")
	if pane == "" || option == "" || os.Getenv("PERSONA_LIVE_DANGER") == "" {
		t.Skip("PERSONA_LIVE_PANE / PERSONA_LIVE_OPTION / PERSONA_LIVE_DANGER not set")
	}
	n := &Node{hosts: herdrHosts(herdr.New()), agents: map[string]host.Agent{"live": {PaneID: pane}}}
	ctx := context.Background()
	text, err := n.readPane(ctx, pane, "visible", screen.DialogLines)
	if err != nil {
		t.Fatal(err)
	}
	d := screen.ParseDialog(text)
	if d == nil || d.Deadline == "" || d.Warning == "" {
		t.Fatalf("no dangerous-command prompt with a countdown on\n%s", text)
	}
	t.Logf("question %q rev %s deadline %q", d.Question, d.Rev, d.Deadline)
	time.Sleep(4 * time.Second)
	keys, pre, err := n.dialogKeys(ctx, "live", d.Rev, option)
	if err != nil {
		t.Fatalf("refused after the countdown moved: %v", err)
	}
	if err := n.sendKeys(ctx, pane, append(pre, keys...)...); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	text, _ = n.readPane(ctx, pane, "visible", screen.DialogLines)
	if after := screen.ParseDialog(text); after != nil && after.Rev == d.Rev {
		t.Fatalf("the prompt is still there:\n%s", text)
	}
}

// PERSONA_LIVE_PANE=<pane> PERSONA_LIVE_PROMPT=<message> [PERSONA_LIVE_OCC=<n>] [PERSONA_LIVE_AFTER=<n>]
// [PERSONA_LIVE_RESTORE=conversation|code_and_conversation]: rewinds the throwaway Claude in the pane (never a real
// session) to before the message, through the screens of /rewind (rewind.go).
func TestLiveRewind(t *testing.T) {
	pane, prompt := os.Getenv("PERSONA_LIVE_PANE"), os.Getenv("PERSONA_LIVE_PROMPT")
	if pane == "" || prompt == "" {
		t.Skip("PERSONA_LIVE_PANE / PERSONA_LIVE_PROMPT not set")
	}
	o := rewindOpts{Prompt: prompt, Occurrence: 1, Restore: os.Getenv("PERSONA_LIVE_RESTORE")}
	if o.Restore == "" {
		o.Restore = RestoreConversation
	}
	if v, err := strconv.Atoi(os.Getenv("PERSONA_LIVE_OCC")); err == nil {
		o.Occurrence = v
	}
	if v, err := strconv.Atoi(os.Getenv("PERSONA_LIVE_AFTER")); err == nil {
		o.PromptsAfter = &v
	}
	n := &Node{hosts: herdrHosts(herdr.New()), state: newState(),
		agents: map[string]host.Agent{"live": {PaneID: pane, Agent: "claude", AgentStatus: "idle"}}}
	res, err := n.rewind(context.Background(), "live", o)
	t.Logf("result %v, error %v", res, err)
	if want := os.Getenv("PERSONA_LIVE_WANT"); want != "" {
		code := ""
		if err != nil {
			code, _ = refusalOf(t, err)
		}
		if code != want {
			t.Fatalf("code %q, want %q", code, want)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
}

// #5396 against a real Claude pane: the user's own /rewind ("Restore conversation" to before the last prompt), pressed
// here as keys and not through session.rewind, is seen on the screen and the turn leaves the session list; then the
// prompt left in the box is sent again (PERSONA_LIVE_RESEND=1) and the turn gone back over stays out.
// With PERSONA_LIVE_RETYPE=1 the words are only typed into the box again, and must not be taken for a rewind.
// PERSONA_LIVE_ESC=1 opens the panel with Esc Esc instead of /rewind.
// PERSONA_LIVE_PANE=<pane> PERSONA_LIVE_SESSION=<id> PERSONA_LIVE_TRANSCRIPT=<path>
// go test -tags herdrlive -run TestLiveTerminalRewind ./internal/node/
func TestLiveTerminalRewind(t *testing.T) {
	pane, id, path := os.Getenv("PERSONA_LIVE_PANE"), os.Getenv("PERSONA_LIVE_SESSION"), os.Getenv("PERSONA_LIVE_TRANSCRIPT")
	if pane == "" || id == "" || path == "" {
		t.Skip("PERSONA_LIVE_PANE / PERSONA_LIVE_SESSION / PERSONA_LIVE_TRANSCRIPT not set")
	}
	old := paneDialogEvery
	paneDialogEvery = time.Second
	t.Cleanup(func() { paneDialogEvery = old })
	ctx := context.Background()
	n := &Node{hosts: herdrHosts(herdr.New()), state: newState(),
		agents: map[string]host.Agent{id: {PaneID: pane, Agent: "claude", AgentStatus: "done"}},
		paths:  map[string]string{id: path}, metas: map[string]quickMeta{}, bgScans: map[string]*bgScan{},
		files: map[string]*transcript.File{}, watches: map[string]*watch{}}
	row := func() proto.Session {
		for _, s := range n.sessionList(ctx, false) {
			if s.SessionID == id {
				return s
			}
		}
		t.Fatal("no row")
		return proto.Session{}
	}
	before := row()
	f, err := n.file(id)
	if err != nil {
		t.Fatal(err)
	}
	n.refresh(id, f)
	target, ok := f.LastPrompt()
	if !ok {
		t.Fatal("the last prompt is not one a rewind could go back over")
	}
	st0, _ := os.Stat(path)
	t.Logf("before: last text %q, transcript %d bytes", before.LastText, st0.Size())

	// The user's rewind: /rewind, Up to the last prompt, Enter, the cursor to "Restore conversation", Enter.
	read := func() string { s, _ := n.readPane(ctx, pane, "visible", 60); return s }
	wait := func(what string, ok func(string) bool) string {
		for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
			if s := read(); ok(s) {
				return s
			}
		}
		t.Fatalf("no %s on screen:\n%s", what, read())
		return ""
	}
	keys := func(k ...string) {
		if err := n.sendKeys(ctx, pane, k...); err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("PERSONA_LIVE_RETYPE") != "" {
		// No rewind: the last prompt's words typed into the box again. Not taken for one.
		_, _, was := f.Page(0, 1)
		if err := n.sendText(ctx, pane, target.Text); err != nil {
			t.Fatal(err)
		}
		for end := time.Now().Add(8 * time.Second); time.Now().Before(end); time.Sleep(200 * time.Millisecond) {
			row()
		}
		_, _, span := f.Page(0, 1)
		keys(screen.ClearKeys...)
		t.Logf("retyped: last text %q, history %q (was %q)", row().LastText, span.History, was.History)
		if span.History != was.History {
			t.Fatal("the words typed again were taken for a rewind")
		}
		return
	}
	_, _, was := f.Page(0, 1)
	if os.Getenv("PERSONA_LIVE_ESC") != "" { // Esc Esc opens the same panel
		keys("esc")
		time.Sleep(150 * time.Millisecond)
		keys("esc")
	} else {
		if err := n.sendText(ctx, pane, "/rewind"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(300 * time.Millisecond)
		keys("enter")
	}
	wait("list", func(s string) bool { return panel(s, screen.RewindList) != nil })
	keys("up")
	time.Sleep(300 * time.Millisecond)
	keys("enter")
	s := wait("options", func(s string) bool { return panel(s, screen.RewindOptions) != nil })
	for i := 0; i < 6; i++ {
		o := panel(s, screen.RewindOptions)
		cur, want := o.SelectedChoice(), o.Choice(screen.RewindChoiceConversation)
		if cur == nil || want == nil {
			t.Fatalf("choices:\n%s", s)
		}
		if cur.Number == want.Number {
			break
		}
		keys(map[bool]string{true: "down", false: "up"}[cur.Number < want.Number])
		time.Sleep(300 * time.Millisecond)
		s = read()
	}
	keys("enter")
	rewoundAt := time.Now()
	wait("the panel closed", func(s string) bool { return screen.ParseRewind(s) == nil })

	var after proto.Session
	for end := time.Now().Add(20 * time.Second); time.Now().Before(end); time.Sleep(200 * time.Millisecond) {
		if after = row(); after.LastText != before.LastText {
			break
		}
	}
	st1, _ := os.Stat(path)
	_, _, span := f.Page(0, 1)
	t.Logf("after %.1fs: last text %q, history %q, transcript %d bytes (was %d)", time.Since(rewoundAt).Seconds(),
		after.LastText, span.History, st1.Size(), st0.Size())
	if after.LastText == before.LastText || span.History == was.History {
		t.Fatal("the rewind in the terminal was not seen")
	}
	if os.Getenv("PERSONA_LIVE_RESEND") == "" {
		return
	}
	keys("enter") // the prompt left in the box, sent again
	for end := time.Now().Add(90 * time.Second); time.Now().Before(end); time.Sleep(time.Second) {
		n.refresh(id, f)
		if lp, ok := f.LastPrompt(); ok && lp.UUID != target.UUID && lp.Text == target.Text {
			break
		}
	}
	n.mu.Lock()
	m := n.metas[path]
	m.readAt = time.Time{}
	n.metas[path] = m
	n.mu.Unlock()
	again := row()
	items, _, span2 := f.Page(0, 1000)
	var texts []string
	for _, it := range items {
		if it.Kind == proto.KindText {
			texts = append(texts, it.Role+": "+it.Text)
		}
	}
	t.Logf("resent: last text %q, history %q, items %q", again.LastText, span2.History, texts)
	if span2.History != span.History {
		t.Fatalf("history %q after the resend", span2.History)
	}
}
