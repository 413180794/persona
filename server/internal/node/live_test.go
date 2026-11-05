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
	"github.com/nwverse/persona/server/internal/screen"
)

// Against a real claude pane: JARVIS_LIVE_PANE=<pane> JARVIS_LIVE_WANT=<code or ""> [JARVIS_LIVE_CLEAR=<draft>]
// go test -tags herdrlive -run TestLiveCheckPane ./internal/node/
func TestLiveCheckPane(t *testing.T) {
	pane := os.Getenv("JARVIS_LIVE_PANE")
	if pane == "" {
		t.Skip("JARVIS_LIVE_PANE not set")
	}
	n := &Node{hosts: herdrHosts(herdr.New())}
	err := n.checkPane(context.Background(), pane, "claude", os.Getenv("JARVIS_LIVE_CLEAR"))
	code := ""
	if err != nil {
		code, _ = refusalOf(t, err)
	}
	t.Logf("pane %s: %v", pane, err)
	if want := os.Getenv("JARVIS_LIVE_WANT"); code != want {
		t.Fatalf("code %q, want %q", code, want)
	}
}

// JARVIS_LIVE_PANE=<pane> JARVIS_LIVE_OPTION=<key>: answers the pane's dialog the way a card does — rev checked, keys
// worked out from the screen now — and presses them.
func TestLiveAnswerDialog(t *testing.T) {
	pane, option := os.Getenv("JARVIS_LIVE_PANE"), os.Getenv("JARVIS_LIVE_OPTION")
	if pane == "" || option == "" {
		t.Skip("JARVIS_LIVE_PANE / JARVIS_LIVE_OPTION not set")
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

// JARVIS_LIVE_PANE=<pane> JARVIS_LIVE_CHECK=<keys, comma-separated> [JARVIS_LIVE_TEXT=<free text>]: answers the
// multi-select question on the pane the way a card does (dialogmulti.go) — rev checked, one key at a time from
// whatever the terminal shows — and checks the question is gone afterwards (submitted, or the next one shown).
func TestLiveMultiSelect(t *testing.T) {
	pane, check := os.Getenv("JARVIS_LIVE_PANE"), os.Getenv("JARVIS_LIVE_CHECK")
	if pane == "" {
		t.Skip("JARVIS_LIVE_PANE not set")
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
	if err := n.dialogMulti(ctx, "live", d.Rev, want, os.Getenv("JARVIS_LIVE_TEXT")); err != nil {
		t.Fatal(err)
	}
	t.Logf("answered in %s", time.Since(start).Round(time.Millisecond))
	time.Sleep(time.Second)
	text, _ = n.readPane(ctx, pane, "visible", screen.DialogLines)
	if after := screen.ParseDialog(text); after != nil && after.Rev == d.Rev {
		t.Fatalf("the question is still there:\n%s", text)
	}
}

// JARVIS_LIVE_PANE=<pane> JARVIS_LIVE_OPTION=<1 or 2> on Claude Code's dangerous-command prompt with its countdown
// running (#564): the card's revision is taken, the clock moves on for a few seconds (as a person reads the card),
// and the answer still goes in; afterwards the prompt is gone.
func TestLiveAnswerDangerousPrompt(t *testing.T) {
	pane, option := os.Getenv("JARVIS_LIVE_PANE"), os.Getenv("JARVIS_LIVE_OPTION")
	if pane == "" || option == "" || os.Getenv("JARVIS_LIVE_DANGER") == "" {
		t.Skip("JARVIS_LIVE_PANE / JARVIS_LIVE_OPTION / JARVIS_LIVE_DANGER not set")
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

// JARVIS_LIVE_PANE=<pane> JARVIS_LIVE_PROMPT=<message> [JARVIS_LIVE_OCC=<n>] [JARVIS_LIVE_AFTER=<n>]
// [JARVIS_LIVE_RESTORE=conversation|code_and_conversation]: rewinds the throwaway Claude in the pane (never a real
// session) to before the message, through the screens of /rewind (rewind.go).
func TestLiveRewind(t *testing.T) {
	pane, prompt := os.Getenv("JARVIS_LIVE_PANE"), os.Getenv("JARVIS_LIVE_PROMPT")
	if pane == "" || prompt == "" {
		t.Skip("JARVIS_LIVE_PANE / JARVIS_LIVE_PROMPT not set")
	}
	o := rewindOpts{Prompt: prompt, Occurrence: 1, Restore: os.Getenv("JARVIS_LIVE_RESTORE")}
	if o.Restore == "" {
		o.Restore = RestoreConversation
	}
	if v, err := strconv.Atoi(os.Getenv("JARVIS_LIVE_OCC")); err == nil {
		o.Occurrence = v
	}
	if v, err := strconv.Atoi(os.Getenv("JARVIS_LIVE_AFTER")); err == nil {
		o.PromptsAfter = &v
	}
	n := &Node{hosts: herdrHosts(herdr.New()), state: newState(),
		agents: map[string]host.Agent{"live": {PaneID: pane, Agent: "claude", AgentStatus: "idle"}}}
	res, err := n.rewind(context.Background(), "live", o)
	t.Logf("result %v, error %v", res, err)
	if want := os.Getenv("JARVIS_LIVE_WANT"); want != "" {
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
