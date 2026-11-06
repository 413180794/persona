package devices

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/devicewire"
)

// After the machine moves to a better path, the new connection is the machine's: what its node asks still reaches
// the hub (the old connection ending must not cancel the new one with it).
func TestRequestsWorkAfterMovingToABetterPath(t *testing.T) {
	// pathCheckEvery stays as it is (2 s): a run's path watcher may still read it after the test ends.
	d := newTestDevices(t)
	d.mu.Lock()
	d.cfg.Mesh = &devicewire.Mesh{PathSwitchMS: 50}
	d.mu.Unlock()
	host := d.host()
	host.request = func(ctx context.Context, p devicewire.NodeCall) (json.RawMessage, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return json.RawMessage(`{"ok":true}`), nil
	}

	slow, fast := &testNode{}, &testNode{}
	m := newMachine(d, MachineConfig{ID: "box", Name: "box", Kind: "ssh"})
	m.dialNode = slow.dial
	other := newMachine(d, MachineConfig{ID: "box-local", Name: "box-local", Kind: "local"})
	other.dialNode = fast.dial
	d.addEntry(m)
	mine, better := cfgPath(m), cfgPath(other)
	f := &d.fleet
	f.mu.Lock()
	f.init()
	f.paths[m] = []*nodePath{better, mine}
	f.absorbed[other] = m
	// The better path is held off at first: the machine connects through its own, then moves.
	f.health[better.key] = &pathHealth{failUntil: time.Now().Add(200 * time.Millisecond)}
	f.mu.Unlock()

	runMachine(t, m)
	eventually(t, 5*time.Second, "connected through its own path", func() bool {
		return m.reachable() && m.activePathKey() == mine.key
	})
	eventually(t, 10*time.Second, "moved to the better path", func() bool {
		return m.reachable() && m.activePathKey() == better.key && len(fast.seen("sessions.list")) > 0
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out json.RawMessage
	if err := fast.current().Call(ctx, "assistant.context", map[string]any{}, &out); err != nil {
		t.Fatalf("the node's request on the new path: %v", err)
	}
	if !m.reachable() || m.activePathKey() != better.key {
		t.Fatalf("not on the better path any more (active %q)", m.activePathKey())
	}
}
