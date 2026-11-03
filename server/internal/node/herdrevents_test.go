package node

import (
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/host"
)

func TestSubscriptionsCoverEachPane(t *testing.T) {
	subs := subscriptions([]string{"w1:p1", "w2:p3"})
	var status []string
	for _, s := range subs {
		if s["type"] == "pane.agent_status_changed" {
			status = append(status, s["pane_id"])
		} else if s["pane_id"] != "" {
			t.Fatalf("lifecycle subscription with a pane: %v", s)
		}
	}
	if len(status) != 2 || status[0] != "w1:p1" || status[1] != "w2:p3" {
		t.Fatalf("status subscriptions %v", status)
	}
}

func TestPanesChangedResubscribes(t *testing.T) {
	n := &Node{agents: map[string]host.Agent{"s1": {PaneID: "w1:p1"}}, events: newHerdrEvents()}
	cancelled := 0
	n.events.panes, n.events.resub = "w1:p1", func() { cancelled++ }
	n.panesChanged()
	if cancelled != 0 {
		t.Fatal("same panes must keep the subscription")
	}
	n.agents["s2"] = host.Agent{PaneID: "w2:p1"}
	n.panesChanged()
	if cancelled != 1 || !n.events.resubbed || n.events.panes != "w1:p1 w2:p1" {
		t.Fatalf("cancelled %d resubbed %v panes %q", cancelled, n.events.resubbed, n.events.panes)
	}
}

func TestHostDue(t *testing.T) {
	herdrSig := newWatcherSignals()
	coreSig := newWatcherSignals()
	n := &Node{
		agents:  map[string]host.Agent{"s1": {Agent: "claude", PaneID: "w1:p1"}},
		hostSig: map[string]*watcherSignals{host.Herdr: herdrSig, host.HerdrCore: coreSig},
	}
	now := time.Now()
	// Without a live stream every host is polled every tick.
	if !n.hostDue(host.Herdr, now, false) {
		t.Fatal("without an event stream herdr is polled every tick")
	}
	herdrSig.setLive(herdrSig.watcherID())
	coreSig.setLive(coreSig.watcherID())
	if n.hostDue(host.Herdr, now, false) {
		t.Fatal("with events, a fresh snapshot is enough")
	}
	if !n.hostDue(host.Herdr, now.Add(-slowSnapshot), false) {
		t.Fatal("the slow snapshot still runs")
	}
	// herdr-core announcing a change schedules only herdr-core, not herdr (the combined engine's isolation).
	if n.hostDue(host.Herdr, now, false) {
		t.Fatal("herdr-core's event must not snapshot herdr")
	}
	if !n.hostDue(host.HerdrCore, now, true) {
		t.Fatal("the kicked host is due")
	}
	// TRAEX state lives in the title spinner, which sends no event: its own host stays due, the other one not.
	n.agents["t1"] = host.Agent{Agent: traexAgent, PaneID: "herdr-core:t9"}
	if n.hostDue(host.Herdr, now, false) {
		t.Fatal("TRAEX in herdr-core must not make herdr poll")
	}
	if !n.hostDue(host.HerdrCore, now, false) {
		t.Fatal("TRAEX in herdr-core keeps it polling")
	}
}

func TestHostWithoutWatcherAlwaysDue(t *testing.T) {
	n := &Node{} // Orca has no Watcher: hostDue is true on every tick
	if !n.hostDue(host.Orca, time.Now(), false) {
		t.Fatal("a host without an event stream is polled every tick")
	}
}
