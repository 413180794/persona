package node

import (
	"context"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
)

// #2553: Claude Code 2.1.309's auto mode notice held three terminals while herdr called the agents done, so they
// were listed as 完成未看. A resting Claude whose screen shows a dialog is now blocked, the dialog's first line its
// reason; once the dialog is gone it is done again.
func TestADialogOnARestingClaudeNeedsYou(t *testing.T) {
	old := paneDialogEvery
	paneDialogEvery = 0
	t.Cleanup(func() { paneDialogEvery = old })
	const id = "0d000000-0000-4000-8000-000000002553"
	f := &fakePane{screens: []string{screenFixture(t, "claude-2.1.309-auto-mode-notice-120.txt"),
		screenFixture(t, "claude-2.1.312-empty-162.txt")}}
	n, _ := testNode(t)
	n.hosts = herdrHosts(f.serve(t))
	n.agents = map[string]host.Agent{id: {Agent: "claude", PaneID: "w1:p1", Cwd: "/work/repo", AgentStatus: proto.StatusDone}}
	row := func() proto.Session {
		for _, s := range n.sessionList(context.Background(), false) {
			if s.SessionID == id {
				return s
			}
		}
		t.Fatal("no row")
		return proto.Session{}
	}
	waitRow := func(status string) proto.Session {
		t.Helper()
		for end := time.Now().Add(2 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
			if s := row(); s.Status == status {
				return s
			}
		}
		t.Fatalf("never %s: %+v", status, row())
		return proto.Session{}
	}
	s := waitRow(proto.StatusBlocked)
	if want := "We're changing auto mode to no longer charge for classifier requests in Claude Code."; s.AttentionReason != want {
		t.Fatalf("reason %q", s.AttentionReason)
	}
	f.mu.Lock()
	f.state = 1 // closed at the computer
	f.mu.Unlock()
	if s := waitRow(proto.StatusDone); s.AttentionReason != "" {
		t.Fatalf("reason left %q", s.AttentionReason)
	}

	// A working Claude's screen is not read for a dialog; neither is one Persona drives itself (a picker it opened).
	n.forgetDialog("w1:p1")
	looked := func() bool {
		n.dialogs.mu.Lock()
		defer n.dialogs.mu.Unlock()
		_, ok := n.dialogs.m["w1:p1"]
		return ok
	}
	n.agents[id] = host.Agent{Agent: "claude", PaneID: "w1:p1", Cwd: "/work/repo", AgentStatus: proto.StatusWorking}
	if row(); looked() {
		t.Fatal("read a working agent's screen for a dialog")
	}
	n.agents[id] = host.Agent{Agent: "claude", PaneID: "w1:p1", Cwd: "/work/repo", AgentStatus: proto.StatusDone}
	n.driving = map[string]bool{id: true}
	if s := row(); looked() || s.Status == proto.StatusBlocked {
		t.Fatalf("read a driven agent's screen for a dialog: %s", s.Status)
	}
}

// dismiss_notice closes a notice Enter only closes, then the send goes on; without it, or for any other dialog,
// the send is refused with the dialog's first line and nothing is pressed.
func TestDismissNoticeClosesOnlyNotices(t *testing.T) {
	old := noticeSettle
	noticeSettle = 0
	t.Cleanup(func() { noticeSettle = old })
	notice := screenFixture(t, "claude-2.1.309-auto-mode-notice-120.txt")
	empty := screenFixture(t, "claude-2.1.312-empty-162.ansi")
	ctx := context.Background()

	f := &fakePane{screens: []string{notice, empty}}
	n := &Node{hosts: herdrHosts(f.serve(t))}
	code, data := refusalOf(t, n.checkPaneFor(ctx, "w1:p1", "claude", sendOpts{}))
	if code != "dialog_open" || data["notice"] != true || data["delivered"] != "none" ||
		data["dialog"] != "We're changing auto mode to no longer charge for classifier requests in Claude Code." || len(f.keys) != 0 {
		t.Fatalf("without dismiss_notice: %s %v keys %v", code, data, f.keys)
	}
	if err := n.checkPaneFor(ctx, "w1:p1", "claude", sendOpts{DismissNotice: true}); err != nil {
		t.Fatalf("with dismiss_notice: %v", err)
	}
	if len(f.keys) != 1 || len(f.keys[0]) != 1 || f.keys[0][0] != "enter" {
		t.Fatalf("keys %v", f.keys)
	}

	for _, name := range []string{"claude-2.1.312-trust-162.ansi", "claude-2.1.312-model-picker-162.ansi", "claude-2.1.316-danger-rm-120.txt"} {
		g := &fakePane{screens: []string{screenFixture(t, name), empty}}
		m := &Node{hosts: herdrHosts(g.serve(t))}
		code, data := refusalOf(t, m.checkPaneFor(ctx, "w1:p1", "claude", sendOpts{DismissNotice: true}))
		if code != "dialog_open" || data["notice"] != false || data["dialog"] == nil || len(g.keys) != 0 {
			t.Fatalf("%s: %s %v keys %v", name, code, data, g.keys)
		}
	}
}

// A notice Enter does not close (or Claude is slow to redraw) is pressed once, not again: a second Enter would land
// in Claude's input box and send its suggestion. The send is refused.
func TestDismissNoticePressesEnterOnce(t *testing.T) {
	old := noticeSettle
	noticeSettle = 50 * time.Millisecond
	t.Cleanup(func() { noticeSettle = old })
	f := &fakePane{screens: []string{screenFixture(t, "claude-2.1.309-auto-mode-notice-120.txt")}} // stays on screen
	n := &Node{hosts: herdrHosts(f.serve(t))}
	code, data := refusalOf(t, n.checkPaneFor(context.Background(), "w1:p1", "claude", sendOpts{DismissNotice: true}))
	if code != "dialog_open" || data["notice"] != true {
		t.Fatalf("%s %v", code, data)
	}
	if len(f.keys) != 1 {
		t.Fatalf("pressed %v", f.keys)
	}
}
