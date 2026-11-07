//go:build enginehostslive

package node

// A live measurement for #2576 (go test -tags enginehostslive -run EngineHostsLive ./internal/node/ -v): how much
// enabling Orca beside herdr costs herdr. On a machine running both, it measures herdr's refresh (the partial refresh a
// herdr event triggers, and a full one) and a send into a herdr pane, first with herdr alone registered, then with
// herdr and Orca registered while Orca is refreshed every second as its watcher does. Orca is reached read-only (no
// call that changes it); herdr gets one scratch workspace running `cat > /dev/null`, closed at the end, so the texts sent
// go nowhere. It prints p50 / p95 / max for each and fails only when herdr is not there to measure.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/herdr"
	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/orcart"
)

const liveRounds = 40

func latencyNode(home string, hs ...host.Host) *Node {
	n := &Node{home: home, paths: map[string]string{}, agents: map[string]host.Agent{}, panes: map[string]string{},
		panePIDs: map[string]panePID{}, state: newState(), cdirs: newClaudeDirs()}
	n.hosts = host.NewRegistry(hs...)
	return n
}

func TestEngineHostsLiveLatency(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	hc := herdr.New()
	hh := &herdrHost{c: hc}
	if hh.Version(ctx) == "" {
		t.Fatal("herdr not running: nothing to measure")
	}
	meta := filepath.Join(home, "Library", "Application Support", "orca", "orca-runtime.json")
	oc := &orcart.Client{MetaPath: meta, ReadOnly: true}
	st, err := oc.Probe(ctx)
	if err != nil {
		t.Skipf("Orca not reachable: %v", err)
	}
	oh := newOrcaHost(oc)
	t.Logf("herdr %s, Orca %s", hh.Version(ctx), st.AppVersion)

	// The scratch pane the sends go to.
	created, err := hh.CreateSpace(ctx, t.TempDir(), "persona-latency-probe")
	if err != nil {
		t.Fatalf("scratch workspace: %v", err)
	}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer ccancel()
		if err := hh.CloseSlot(cctx, created.SlotID); err != nil {
			t.Logf("closing the scratch tab %s: %v", created.SlotID, err)
		}
	})
	if err := hh.Run(ctx, created.Pane, "cat > /dev/null"); err != nil {
		t.Fatalf("scratch pane: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	pane := created.Pane.String()

	measure := func(name string, n *Node, withOrca bool) {
		stop := make(chan struct{})
		done := make(chan struct{})
		polls := 0
		if withOrca { // Orca's watcher: a refresh of Orca every second (hostorca.go)
			go func() {
				defer close(done)
				tick := time.NewTicker(time.Second)
				defer tick.Stop()
				for {
					select {
					case <-stop:
						return
					case <-tick.C:
						_ = n.refreshHosts(ctx, oh)
						polls++
					}
				}
			}()
		} else {
			close(done)
		}
		_ = n.refreshHosts(ctx) // warm caches
		var partial, full, send latencies
		for i := range liveRounds {
			t0 := time.Now()
			if err := n.refreshHosts(ctx, hh); err != nil {
				t.Fatalf("%s: herdr refresh: %v", name, err)
			}
			partial = append(partial, time.Since(t0))
			t0 = time.Now()
			_ = n.refreshHosts(ctx)
			full = append(full, time.Since(t0))
			t0 = time.Now()
			if err := n.sendText(ctx, pane, fmt.Sprintf("probe %d", i)); err != nil {
				t.Fatalf("%s: send: %v", name, err)
			}
			send = append(send, time.Since(t0))
			time.Sleep(150 * time.Millisecond) // spread the rounds over Orca's polls
		}
		close(stop)
		<-done
		t.Logf("%-14s herdr refresh (herdr event)  %s", name, partial)
		t.Logf("%-14s full refresh (all hosts)     %s", name, full)
		t.Logf("%-14s herdr send                   %s", name, send)
		if withOrca {
			t.Logf("%-14s Orca polls meanwhile: %d", name, polls)
		}
	}
	measure("herdr only", latencyNode(home, hh), false)
	measure("herdr + Orca", latencyNode(home, hh, oh), true)
}
