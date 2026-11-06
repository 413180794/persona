package node

import (
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// A restart takes herdr's panes with it. Sessions Persona had started in one are shown as interrupted, so the apps
// can offer "Continue"; everything else keeps the status it had.
func TestMarkInterrupted(t *testing.T) {
	n, _ := testNode(t)
	n.bootAt = time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	before := n.bootAt.Add(-time.Hour)
	after := n.bootAt.Add(time.Hour)

	n.state.Sessions["cut-off"] = sessionState{Pane: "pane-1", StartedAt: before}
	n.state.Sessions["ended-on-its-own"] = sessionState{Pane: "pane-2", StartedAt: after}
	n.state.Sessions["no-pane"] = sessionState{StartedAt: before} // the user's own terminal: never a pane of ours
	n.state.Sessions["still-running"] = sessionState{Pane: "pane-3", StartedAt: before}
	// "unknown" is simply not in the record.

	rows := []proto.Session{
		{SessionID: "cut-off", Status: proto.StatusOffline},
		{SessionID: "ended-on-its-own", Status: proto.StatusOffline},
		{SessionID: "no-pane", Status: proto.StatusOffline},
		{SessionID: "still-running", Status: proto.StatusOffline},
		{SessionID: "unknown", Status: proto.StatusOffline},
	}
	live := map[string]bool{"still-running": true}

	n.markInterrupted(rows, live)

	want := map[string]string{
		"cut-off":          proto.StatusInterrupted,
		"ended-on-its-own": proto.StatusOffline,
		"no-pane":          proto.StatusOffline,
		"still-running":    proto.StatusOffline, // live: not touched
		"unknown":          proto.StatusOffline,
	}
	for _, r := range rows {
		if r.Status != want[r.SessionID] {
			t.Errorf("%s: status %q, want %q", r.SessionID, r.Status, want[r.SessionID])
		}
	}
}

// A session that is not offline is left alone whatever the record says.
func TestMarkInterruptedLeavesRunningRows(t *testing.T) {
	n, _ := testNode(t)
	n.bootAt = time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	n.state.Sessions["busy"] = sessionState{Pane: "pane-1", StartedAt: n.bootAt.Add(-time.Hour)}

	rows := []proto.Session{
		{SessionID: "busy", Status: proto.StatusWorking},
		{SessionID: "busy2", Status: proto.StatusBlocked},
	}
	n.markInterrupted(rows, nil)

	if rows[0].Status != proto.StatusWorking || rows[1].Status != proto.StatusBlocked {
		t.Fatalf("running rows kept their status: %v", rows)
	}
}
