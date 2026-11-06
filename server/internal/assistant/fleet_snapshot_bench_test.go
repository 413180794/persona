package assistant

import (
	"fmt"
	"slices"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// BenchmarkFleetSnapshot: the live fleet the assistant computes after every session change (fleet_live.go), for one
// machine listing 300 sessions.
func BenchmarkFleetSnapshot(b *testing.B) {
	f := newFakeHub()
	d := newFleetDevs()
	d.add("mac", "Mac")
	s := New(Options{Hub: f, Logf: b.Logf, Devices: d})
	list := make([]proto.Session, 300)
	for i := range list {
		list[i] = proto.Session{SessionID: fmt.Sprintf("s%03d", i), Cwd: "/code/app", Agent: "claude",
			Status: proto.StatusDone, PaneID: fmt.Sprintf("p%d", i), SpaceName: "app", Title: "a session", LastText: "done"}
	}
	d.setSessions("mac", list)
	b.ReportAllocs()
	for b.Loop() {
		s.fleetSnapshot()
	}
}

// The live fleet leaves out an agent whose space is muted, keeps one turned back on in it and one elsewhere: the
// session's own space decides, as in the node's row.
func TestFleetSnapshotMutedSpace(t *testing.T) {
	ft := newFleetTest(t)
	ft.mutes(Pins{Spaces: []string{"mac/app"}, Sessions: []string{}, Allowed: []string{"mac/s2"}})
	ft.d.setSessions("mac", []proto.Session{
		{SessionID: "s1", Cwd: "/code/app", Agent: "claude", Status: proto.StatusWorking, PaneID: "p1", SpaceName: "app"},
		{SessionID: "s2", Cwd: "/code/app", Agent: "claude", Status: proto.StatusWorking, PaneID: "p2", SpaceName: "app"},
		{SessionID: "s3", Cwd: "/code/web", Agent: "claude", Status: proto.StatusWorking, PaneID: "p3", SpaceName: "web"},
	})
	st := ft.s.fleetSnapshot()
	var ids []string
	for _, it := range st.Items {
		ids = append(ids, it.ID)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []string{"mac/s2", "mac/s3"}) {
		t.Fatalf("items %v", ids)
	}
}
