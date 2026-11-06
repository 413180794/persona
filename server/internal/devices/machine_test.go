package devices

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/rpc"
)

// TestFakeNode is not a test: run as a subprocess (PERSONA_FAKE_NODE=1) it is a node on stdio answering every call.
func TestFakeNode(t *testing.T) {
	if os.Getenv("PERSONA_FAKE_NODE") != "1" {
		t.Skip("helper process")
	}
	p := rpc.New("n", os.Stdout)
	p.OnRequest = func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		return map[string]any{"pid": os.Getpid()}, nil
	}
	_ = p.Serve(context.Background(), os.Stdin)
	os.Exit(0)
}

// "Reconnect" closed the peer but kept waiting for the ssh process to end, which it never did: the machine stayed
// online with every call failing (rpc: connection closed) until the hub restarted (2026-10-31, found sending a
// message during a reconnect). A reconnect must end in a new, working connection.
func TestReconnectGivesAWorkingConnection(t *testing.T) {
	h := newTestDevices(t)
	m := newMachine(h, MachineConfig{ID: "box", Name: "Box", Kind: "local"})
	var started atomic.Int32
	m.nodeCmd = func(ctx context.Context) *exec.Cmd {
		started.Add(1)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFakeNode$")
		cmd.Env = append(os.Environ(), "PERSONA_FAKE_NODE=1")
		return cmd
	}
	runMachine(t, m)
	pid := func() (int, error) {
		var out struct {
			PID int `json:"pid"`
		}
		cctx, ccancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer ccancel()
		err := m.call(cctx, "ping", map[string]any{}, &out)
		return out.PID, err
	}
	waitPID := func(not int) int {
		deadline := time.Now().Add(15 * time.Second)
		var err error
		for time.Now().Before(deadline) {
			var p int
			if p, err = pid(); err == nil && p != not {
				return p
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("no working connection (last error %v, %d node processes started)", err, started.Load())
		return 0
	}
	first := waitPID(0)
	m.reconnect()
	if _, err := pid(); err != nil && !errors.Is(err, rpc.ErrClosed) && !errors.Is(err, errOffline) {
		t.Fatalf("during the reconnect: %v", err)
	}
	if second := waitPID(first); second == first {
		t.Fatal("still the old node")
	}
}
