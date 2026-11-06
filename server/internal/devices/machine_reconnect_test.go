package devices

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// A real subprocess keeps stdout open until the hub terminates its transport.
func TestMachinePeerHelper(t *testing.T) {
	if os.Getenv("PERSONA_TEST_MACHINE_PEER") != "1" {
		return
	}
	peer := rpc.New("n", os.Stdout)
	peer.OnRequest = func(_ context.Context, method string, _ json.RawMessage) (any, error) {
		switch method {
		case "node.info":
			return proto.NodeInfo{Hostname: "test-node"}, nil
		case "sessions.list":
			return map[string]any{"sessions": []proto.Session{}}, nil
		}
		return map[string]any{}, nil
	}
	_ = peer.Serve(context.Background(), os.Stdin)
	os.Exit(0)
}

func TestMachineReconnectReplacesClosedPeer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SSH subprocess wrapper requires a POSIX shell")
	}
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte("#!/bin/sh\nexec \"$PERSONA_TEST_PEER_BINARY\" -test.run '^TestMachinePeerHelper$'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PERSONA_TEST_PEER_BINARY", bin)
	t.Setenv("PERSONA_TEST_MACHINE_PEER", "1")
	m := newMachine(newTestDevices(t), MachineConfig{ID: "test-machine", Kind: "ssh", SSHHost: "test-node"})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); m.run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("machine loop did not stop")
		}
	})
	waitPeer := func(previous *rpc.Peer) *rpc.Peer {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			m.mu.Lock()
			peer, online := m.peer, m.online
			m.mu.Unlock()
			if online && peer != nil && peer != previous {
				return peer
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("closed RPC peer remained online instead of reconnecting")
		return nil
	}
	first := waitPeer(nil)
	m.reconnect()
	waitPeer(first)
	callCtx, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	if err := m.call(callCtx, "node.info", nil, nil); err != nil {
		t.Fatalf("RPC failed after reconnect: %v", err)
	}
}
