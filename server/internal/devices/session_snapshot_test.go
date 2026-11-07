package devices

import (
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

func TestReconnectSnapshotCannotOverwriteLiveSessions(t *testing.T) {
	for _, status := range []string{proto.StatusWorking, proto.StatusDone} {
		t.Run(status, func(t *testing.T) {
			peer := new(rpc.Peer)
			m := &Machine{cfg: MachineConfig{ID: "computer"}, peer: peer}
			revision := m.sessionsRevision()
			m.setSessions([]proto.Session{{SessionID: "agent", Status: status}})
			if m.setInitialSessions(peer, revision, []proto.Session{{SessionID: "agent", Status: proto.StatusIdle}}) {
				t.Fatal("a reconnect snapshot replaced a live notification")
			}
			if got := m.sessionList(); len(got) != 1 || got[0].Status != status {
				t.Fatalf("sessions = %+v", got)
			}
		})
	}
}

func TestReconnectSnapshotRequiresCurrentConnection(t *testing.T) {
	peer := new(rpc.Peer)
	m := &Machine{cfg: MachineConfig{ID: "computer"}, peer: peer}
	if m.setInitialSessions(new(rpc.Peer), m.sessionsRevision(), nil) {
		t.Fatal("accepted a disconnected owner's snapshot")
	}
	if !m.setInitialSessions(peer, m.sessionsRevision(), []proto.Session{{SessionID: "agent", Status: proto.StatusWorking}}) {
		t.Fatal("rejected the current connection's initial list")
	}
	if got := m.sessionList()[0]; got.MachineID != "computer" || got.Status != proto.StatusWorking {
		t.Fatalf("session = %+v", got)
	}
}
