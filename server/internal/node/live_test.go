//go:build herdrlive

package node

import (
	"context"
	"os"
	"testing"

	"github.com/chyroc/jarvis/server/internal/herdr"
	"github.com/chyroc/jarvis/server/internal/host"
	"github.com/chyroc/jarvis/server/internal/screen"
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
