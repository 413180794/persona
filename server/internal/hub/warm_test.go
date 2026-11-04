package hub

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// A node that drops and comes straight back (a resident node restarting on a new binary) is not news: the machine
// shows online meanwhile and nothing goes to the apps; one that stays away shows offline once the grace is over.
func TestNodeReconnectGrace(t *testing.T) {
	old := reconnectGrace
	reconnectGrace = 150 * time.Millisecond
	defer func() { reconnectGrace = old }()
	h := newTestHub(t)
	m := newMachine(h, MachineConfig{ID: "box", Name: "box"})
	h.machines, h.byID["box"] = append(h.machines, m), m
	m.online = true
	m.sessions = []proto.Session{{SessionID: "s1", Status: proto.StatusWorking}}
	c := &client{out: make(chan []byte, 64)}
	h.clients[c] = struct{}{}
	machinesSent := func() (n int, online bool) {
		for {
			select {
			case b := <-c.out:
				var msg struct {
					Type     string          `json:"type"`
					Machines []proto.Machine `json:"machines"`
				}
				_ = json.Unmarshal(b, &msg)
				if msg.Type == "machines" {
					n++
					online = len(msg.Machines) == 1 && msg.Machines[0].Online
				}
			default:
				return n, online
			}
		}
	}

	m.detach(errors.New("node exited"), false)
	if !m.snapshot().Online || m.sessionList()[0].Status != proto.StatusWorking {
		t.Fatal("a node that just disconnected shows offline at once")
	}
	if n, _ := machinesSent(); n != 0 {
		t.Fatalf("the apps were told about a disconnect within the grace (%d)", n)
	}
	time.Sleep(3 * reconnectGrace)
	if m.snapshot().Online || m.sessionList()[0].Status != proto.StatusUnknown {
		t.Fatal("still online after the grace")
	}
	if n, online := machinesSent(); n != 1 || online {
		t.Fatalf("offline after the grace: %d broadcasts, online %v", n, online)
	}
}
