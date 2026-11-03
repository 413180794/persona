//go:build herdrlive

package node

import (
	"context"
	"os"
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
