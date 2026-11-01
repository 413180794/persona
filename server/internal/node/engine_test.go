package node

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/host"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// The herdr engine's hosts are what every node had before engines: one herdr host, nothing of herdr-core.
func TestEngineHostsOfHerdrAreHerdrOnly(t *testing.T) {
	hs := engineHosts(proto.EngineHerdr, nil)
	if len(hs) != 1 {
		t.Fatalf("hosts %v", hs)
	}
	if _, ok := hs[0].(*herdrHost); !ok {
		t.Fatalf("host %T", hs[0])
	}
	hs = engineHosts(proto.EngineHerdrCore, nil)
	if _, ok := hs[0].(*herdrCoreHost); len(hs) != 1 || !ok {
		t.Fatalf("herdr-core hosts %v", hs)
	}
}

// A node on herdr (the default) makes no herdr-core call while it lists, polls, watches, sends, reads and presses keys,
// even with a herdr-core running at the socket it would use.
func TestEngineHerdrMakesNoHerdrCoreCall(t *testing.T) {
	withSubmitGrace(t, 20*time.Millisecond)
	n, f, home := herdrNode(t)
	hc := newFakeHC(t)
	t.Setenv("HC_SOCKET", hc.sock)
	writeClaudePID(t, home, 4242, sidB)
	hc.setTerminals(hcTerminal("t1", "claude-code", "working", 4242, "claude", hcCwd, "", "", ""))
	seedAgents(t, n, f, claudeAgent(sidA, proto.StatusIdle, "w1:p1", "w1:t1", "w1", "/tmp/s", ""))
	_, stop := pollNode(t, n) // sets the hub connection and n.events, before anything reads them
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n.watchEngine(ctx)
	time.Sleep(1500 * time.Millisecond)
	stop()
	if m := byID(listSessions(t, n)); m[sidA].PaneID != "w1:p1" || m[sidB].PaneID != "" {
		t.Errorf("sessions %v", m)
	}
	for _, req := range []struct {
		method string
		params map[string]any
	}{
		{"session.send", map[string]any{"session_id": sidA, "text": "hi"}},
		{"session.screen", map[string]any{"session_id": sidA, "format": "ansi"}},
		{"session.keys", map[string]any{"session_id": sidA, "keys": []string{"esc"}}},
	} {
		if _, err := request(t, n, req.method, req.params); err != nil {
			t.Errorf("%s: %v", req.method, err)
		}
	}
	if kinds := hostKinds(n); strings.Join(kinds, ",") != host.Herdr {
		t.Errorf("hosts %v", kinds)
	}
	if tr := hc.trace("terminal.list", "doc.get", "terminal.get", "terminal.read", "terminal.send", "terminal.keys", "hello"); len(tr) != 0 {
		t.Errorf("herdr-core called: %v", tr)
	}
	select {
	case <-hc.subbed:
		t.Error("herdr-core subscribed to")
	default:
	}
}

func hostKinds(n *Node) []string {
	var out []string
	for _, h := range n.hosts.All() {
		out = append(out, h.Kind())
	}
	return out
}

// engine.use switches the node's one host at run time: the old engine's sessions go, the new one's are pushed, its
// event stream starts; switching back brings herdr's back. An unknown engine is refused, the same one changes nothing.
func TestEngineUseSwitches(t *testing.T) {
	n, f, home := herdrNode(t)
	hc := newFakeHC(t)
	t.Setenv("HC_SOCKET", hc.sock)
	t.Setenv("HERDR_SOCKET_PATH", f.sock)
	writeClaudePID(t, home, 4242, sidB)
	writeTranscript(t, home, hcCwd, sidB, time.Now().Add(-time.Minute), `{"type":"custom-title","customTitle":"b"}`)
	hc.setTerminals(hcTerminal("t1", "claude-code", "working", 4242, "claude", hcCwd, "", "", ""))
	seedAgents(t, n, f, claudeAgent(sidA, proto.StatusIdle, "w1:p1", "w1:t1", "w1", "/tmp/s", ""))
	pushes, _ := pollNode(t, n) // a hub connection (and n.events); the loop's own pushes are skipped below
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n.watchEngine(ctx)

	pushed := func(want func(map[string]proto.Session) bool) map[string]proto.Session {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case p := <-pushes:
				if p.Event != "sessions.changed" {
					continue
				}
				var v struct {
					Sessions []proto.Session `json:"sessions"`
				}
				_ = json.Unmarshal(p.Params, &v)
				if m := byID(v.Sessions); want(m) {
					return m
				}
			case <-deadline:
				t.Fatal("no such sessions.changed")
				return nil
			}
		}
	}
	live := func(m map[string]proto.Session, id string) bool {
		s, ok := m[id]
		return ok && s.Status != proto.StatusOffline && s.PaneID != ""
	}

	res, err := request(t, n, "engine.use", map[string]any{"engine": "herdr-core"})
	if err != nil || res["engine"] != "herdr-core" {
		t.Fatalf("engine.use = %v, %v", res, err)
	}
	m := pushed(func(m map[string]proto.Session) bool { return live(m, sidB) })
	if live(m, sidA) || m[sidB].PaneID != "herdr-core:t1" || m[sidB].Status != proto.StatusWorking {
		t.Errorf("after the switch: %v", m)
	}
	if kinds := hostKinds(n); strings.Join(kinds, ",") != host.HerdrCore {
		t.Errorf("hosts %v", kinds)
	}
	select {
	case <-hc.subbed:
	case <-time.After(3 * time.Second):
		t.Error("herdr-core's events not subscribed to")
	}
	if a, ok := n.agent(sidA); ok {
		t.Errorf("herdr's agent kept: %+v", a)
	}

	f.reset()
	if _, err := request(t, n, "engine.use", map[string]any{"engine": "herdr"}); err != nil {
		t.Fatal(err)
	}
	m = pushed(func(m map[string]proto.Session) bool { return live(m, sidA) })
	if live(m, sidB) {
		t.Errorf("herdr-core's session still live: %+v", m[sidB])
	}
	if kinds := hostKinds(n); strings.Join(kinds, ",") != host.Herdr {
		t.Errorf("hosts %v", kinds)
	}

	if _, err := request(t, n, "engine.use", map[string]any{"engine": "tmux"}); err == nil {
		t.Error("an unknown engine was taken")
	}
	if n.engine() != proto.EngineHerdr {
		t.Errorf("engine %s", n.engine())
	}
}

// node.info reports both engines: available, with their versions, when they answer.
func TestEngineDetection(t *testing.T) {
	n, f, _ := herdrNode(t)
	hc := newFakeHC(t)
	t.Setenv("HC_SOCKET", hc.sock)
	t.Setenv("HERDR_SOCKET_PATH", f.sock)
	hc.handle("hello", func(map[string]any) any { return map[string]any{"version": "0.1.16", "generation": 1} })
	f.reply("ping", map[string]any{"version": "0.9.3"})
	n.hosts.Set(engineHosts(proto.EngineHerdrCore, nil)...) // herdr not the engine: asked here
	got := n.engines(context.Background(), "")
	if len(got) != 2 || got[0] != (proto.Engine{ID: "herdr", Name: "herdr", Available: true, Version: "0.9.3"}) ||
		got[1] != (proto.Engine{ID: "herdr-core", Name: "Herdr App", Available: true, Version: "0.1.16"}) {
		t.Errorf("engines %+v", got)
	}
	t.Setenv("HC_SOCKET", "/tmp/jarvis-no-such-dir/c.sock")
	if got := n.engines(context.Background(), "0.9.3"); got[1].Available || got[0].Version != "0.9.3" {
		t.Errorf("without herdr-core: %+v", got)
	}
}
