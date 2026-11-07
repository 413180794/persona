package node

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/orcart"
	"github.com/nwverse/persona/server/internal/orcart/orcarttest"
	"github.com/nwverse/persona/server/internal/proto"
)

// latencies prints p50 / p95 / p99 / max of a set of timings (this measurement and the live one, enginehosts_live_test.go).
type latencies []time.Duration

func (l latencies) String() string {
	s := slices.Clone(l)
	slices.Sort(s)
	q := func(p float64) time.Duration { return s[int(p*float64(len(s)-1))] }
	return fmt.Sprintf("p50 %6.1f ms  p95 %6.1f ms  p99 %6.1f ms  max %6.1f ms  (n=%d)", ms(q(0.5)), ms(q(0.95)), ms(q(0.99)),
		ms(s[len(s)-1]), len(s))
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// benchOrcaScan is what one Orca process scan costs in the fakes: scanOrcaProcs (three ps runs) took 157-211 ms on a
// Mac with a few hundred processes (2026-11-07).
const benchOrcaScan = 170 * time.Millisecond

// benchRounds and benchGap: the live measurement's rounds, ten times over, so one stall does not set p95.
const (
	benchRounds = 400
	benchGap    = 150 * time.Millisecond
)

// benchNode is a node on a fake herdr and a fake Orca, both registered (herdr the default): herdr runs claudes in
// panes w1:p1… (sessions herdr names, their pids' sessions files too) and a shell in w1:p99, the pane sends go to; Orca
// runs two claudes whose sessions files name their sessions; a claude outside every pane runs another session, so the
// node also asks the herdr panes what they run (sessionFromProcess), as on a machine with a claude in a plain terminal.
// Orca's process scan sleeps benchOrcaScan.
func benchNode(t *testing.T, claudes int) (*Node, *fakeHerdr, host.Host, *orcaHost) {
	t.Helper()
	n, f, home := herdrNode(t)
	var agents, panes, tabs []any
	pids := map[string]int{}
	for i := range claudes {
		sid := fmt.Sprintf("0d000000-0000-4000-8000-%012d", 500+i)
		pane, tab := fmt.Sprintf("w1:p%d", i+1), fmt.Sprintf("w1:t%d", i+1)
		a := claudeAgent(sid, proto.StatusIdle, pane, tab, "w1", "/work/repo", "task")
		agents, panes = append(agents, a), append(panes, a)
		tabs = append(tabs, map[string]any{"tab_id": tab, "workspace_id": "w1", "label": "task", "number": i + 1})
		pids[pane] = 5100 + i
		writeClaudePID(t, home, 5100+i, sid)
		writeTranscript(t, home, "/work/repo", sid, time.Now().Add(-time.Minute), `{"type":"custom-title","customTitle":"task"}`)
	}
	panes = append(panes, map[string]any{"pane_id": "w1:p99", "tab_id": "w1:t99", "workspace_id": "w1", "cwd": "/work/repo"})
	f.reply("session.snapshot", snapshot([]any{map[string]any{"workspace_id": "w1", "label": "repo", "number": 1}}, tabs, agents, panes))
	f.handle("pane.process_info", func(p map[string]any) any {
		return map[string]any{"process_info": map[string]any{"foreground_processes": []any{
			map[string]any{"pid": pids[str(p["pane_id"])], "name": "2.1.400", "argv0": "claude"}}}}
	})
	dir := filepath.Join(home, ".claude", "sessions")
	b, _ := json.Marshal(map[string]any{"pid": os.Getpid(), "sessionId": "0d000000-0000-4000-8000-0000000005ff", "kind": "interactive"})
	if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(os.Getpid())+".json"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	o := orcarttest.New(t)
	procs := &fakeProcs{}
	oh := newOrcaHost(&orcart.Client{MetaPath: o.MetaPath})
	oh.scan = func() (map[string][]orcaProc, error) {
		time.Sleep(benchOrcaScan)
		return procs.scan()
	}
	var terms, rows []map[string]any
	for i, sid := range []string{sidA, sidB} {
		handle, tab, pid := fmt.Sprintf("term_%d", i+1), fmt.Sprintf("tab%d", i+1), 4201+i
		terms = append(terms, orcaTerm(handle, tab, "leaf1", "claude"))
		rows = append(rows, orcarttest.Agent(tab+":leaf1", "claude", "done"))
		procs.set(tab+":leaf1", orcaProc{pid: pid, agent: "claude"})
		writeClaudePID(t, home, pid, sid)
		writeTranscript(t, home, orcaCwd, sid, time.Now().Add(-time.Minute), `{"type":"custom-title","customTitle":"x"}`)
	}
	o.SetTerminals(terms...)
	o.SetWorktrees(orcarttest.Worktree("repo::"+orcaCwd, orcaCwd, "refs/heads/feature-x", rows...))

	hh := n.hosts.Get(host.Herdr)
	n.hosts = host.NewRegistry(hh, oh)
	c := proto.EngineChoice{Hosts: []string{"herdr", "orca"}, Default: "herdr"}
	n.eng.name, n.eng.set = proto.LegacyEngine(c), &c
	return n, f, hh, oh
}

// dueLooks makes the process lookups of kind's panes due (their pids were looked up over a minute ago).
func dueLooks(n *Node, kind string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for p, c := range n.panePIDs {
		if host.ParsePane(p).Kind == kind {
			c.checked = c.checked.Add(-2 * time.Minute)
			n.panePIDs[p] = c
		}
	}
}

// staleScan makes Orca's last process scan older than orcaProcsMaxAge: the next lookup scans.
func staleScan(oh *orcaHost) {
	oh.mu.Lock()
	oh.procsAt = time.Now().Add(-time.Minute)
	oh.mu.Unlock()
}

// PERSONA_BENCH_ENGINEHOSTS=1 measures what enabling Orca beside herdr costs herdr (#5816), on fakes: first the live
// measurement's rounds (a herdr refresh, a refresh of every host, a send into a herdr pane; Orca refreshed every second
// meanwhile, as its watcher does), then the two refreshes that asked the other host's panes what they run: herdr's
// while an Orca pane is due for a look and Orca's scan is stale, and Orca's while the herdr panes are due. Skipped
// otherwise.
func TestEngineHostsLatencyOnFakes(t *testing.T) {
	if os.Getenv("PERSONA_BENCH_ENGINEHOSTS") == "" {
		t.Skip("PERSONA_BENCH_ENGINEHOSTS not set")
	}
	ctx := context.Background()
	measure := func(name string, withOrca bool) {
		n, _, hh, oh := benchNode(t, 8)
		if !withOrca {
			n.hosts = host.NewRegistry(hh)
		}
		_ = n.refreshHosts(ctx) // warm caches
		stop, done := make(chan struct{}), make(chan struct{})
		var polls atomic.Int64
		go func() {
			defer close(done)
			if !withOrca {
				return
			}
			tick := time.NewTicker(time.Second)
			defer tick.Stop()
			for {
				select {
				case <-stop:
					return
				case <-tick.C:
					_ = n.refreshHosts(ctx, oh)
					polls.Add(1)
				}
			}
		}()
		var partial, full, send latencies
		for i := range benchRounds {
			t0 := time.Now()
			if err := n.refreshHosts(ctx, hh); err != nil {
				t.Fatalf("%s: herdr refresh: %v", name, err)
			}
			partial = append(partial, time.Since(t0))
			t0 = time.Now()
			_ = n.refreshHosts(ctx)
			full = append(full, time.Since(t0))
			t0 = time.Now()
			if err := n.sendText(ctx, "w1:p99", fmt.Sprintf("probe %d", i)); err != nil {
				t.Fatalf("%s: send: %v", name, err)
			}
			send = append(send, time.Since(t0))
			time.Sleep(benchGap)
		}
		close(stop)
		<-done
		t.Logf("%-14s herdr refresh (herdr event)  %s", name, partial)
		t.Logf("%-14s full refresh (all hosts)     %s", name, full)
		t.Logf("%-14s herdr send                   %s", name, send)
		if withOrca {
			t.Logf("%-14s Orca polls meanwhile: %d", name, polls.Load())
		}
	}
	measure("herdr only", false)
	measure("herdr + Orca", true)

	const trials = 40
	n, f, hh, oh := benchNode(t, 8)
	_ = n.refreshHosts(ctx)
	var herdrRefresh latencies
	for range trials {
		dueLooks(n, host.Orca)
		staleScan(oh)
		t0 := time.Now()
		if err := n.refreshHosts(ctx, hh); err != nil {
			t.Fatal(err)
		}
		herdrRefresh = append(herdrRefresh, time.Since(t0))
	}
	t.Logf("herdr refresh, an Orca pane due for a look, Orca's scan stale  %s", herdrRefresh)
	asked := 0
	var orcaRefresh latencies
	for range trials {
		dueLooks(n, host.Herdr)
		f.reset()
		t0 := time.Now()
		if err := n.refreshHosts(ctx, oh); err != nil {
			t.Fatal(err)
		}
		orcaRefresh = append(orcaRefresh, time.Since(t0))
		asked += len(f.trace("pane.process_info"))
	}
	t.Logf("Orca refresh, herdr panes due for a look                       %s; herdr asked %d times (%d refreshes)", orcaRefresh, asked, trials)
}
