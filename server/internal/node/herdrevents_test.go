package node

import (
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/herdr"
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
	n := &Node{agents: map[string]herdr.Agent{"s1": {PaneID: "w1:p1"}}, events: newHerdrEvents()}
	cancelled := 0
	n.events.panes, n.events.resub = "w1:p1", func() { cancelled++ }
	n.panesChanged()
	if cancelled != 0 {
		t.Fatal("same panes must keep the subscription")
	}
	n.agents["s2"] = herdr.Agent{PaneID: "w2:p1"}
	n.panesChanged()
	if cancelled != 1 || !n.events.resubbed || n.events.panes != "w1:p1 w2:p1" {
		t.Fatalf("cancelled %d resubbed %v panes %q", cancelled, n.events.resubbed, n.events.panes)
	}
}

func TestSnapshotDue(t *testing.T) {
	n := &Node{agents: map[string]herdr.Agent{"s1": {Agent: "claude", PaneID: "w1:p1"}}, events: newHerdrEvents()}
	now := time.Now()
	if !n.snapshotDue(now) {
		t.Fatal("without an event stream herdr is polled every tick")
	}
	n.events.live.Store(true)
	if n.snapshotDue(now) {
		t.Fatal("with events, a fresh snapshot is enough")
	}
	if !n.snapshotDue(now.Add(-slowSnapshot)) {
		t.Fatal("the slow snapshot still runs")
	}
	n.agents["t1"] = herdr.Agent{Agent: traexAgent, PaneID: "w1:p2"}
	if !n.snapshotDue(now) {
		t.Fatal("TRAEX state is in the title spinner, which sends no event")
	}
}
