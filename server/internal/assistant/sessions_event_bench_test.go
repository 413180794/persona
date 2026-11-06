package assistant

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// BenchmarkSessionsChangedEvent: a node's sessions.changed for 300 sessions as the assistant takes it (the devices
// have the list already, as in production).
func BenchmarkSessionsChangedEvent(b *testing.B) {
	f := newFakeHub()
	d := newFleetDevs()
	d.add("mac", "Mac")
	s := New(Options{Hub: f, Logf: func(string, ...any) {}, Devices: d})
	list := make([]proto.Session, 300)
	for i := range list {
		list[i] = proto.Session{SessionID: fmt.Sprintf("s%03d", i), Cwd: "/code/app", Agent: "claude",
			Status: proto.StatusDone, PaneID: fmt.Sprintf("p%d", i), SpaceName: "app", Title: "a session",
			LastText: strings.Repeat("the last words of the turn ", 8)}
	}
	params, _ := json.Marshal(map[string]any{"sessions": list})
	d.setSessions("mac", list)
	b.ReportAllocs()
	for b.Loop() {
		s.NodeEvent("mac", "sessions.changed", params)
	}
}

// A sessions.changed for a machine the devices do not have is decoded from the event, as before; one they have is
// their list.
func TestSessionsChangedEventSource(t *testing.T) {
	ft := newFleetTestNoStore(t)
	params, _ := json.Marshal(map[string]any{"sessions": []proto.Session{{SessionID: "x1"}, {SessionID: "x2"}}})
	ft.s.NodeEvent("elsewhere", "sessions.changed", params)
	if got := ft.s.fleetPrevious("elsewhere"); len(got) != 2 || got[1].SessionID != "x2" || got[1].MachineID != "elsewhere" {
		t.Fatalf("decoded: %+v", got)
	}
	ft.d.setSessions("mac", []proto.Session{{SessionID: "s1", Title: "kept"}})
	ft.s.NodeEvent("mac", "sessions.changed", params)
	if got := ft.s.fleetPrevious("mac"); len(got) != 1 || got[0].SessionID != "s1" || got[0].Title != "kept" {
		t.Fatalf("the devices' list: %+v", got)
	}
}
