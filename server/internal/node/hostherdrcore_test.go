package node

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/herdrcore"
	"github.com/chyroc/jarvis/server/internal/host"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// herdr-core as the machine's engine (hostherdrcore.go, engine.go), driven through the node's requests against a fake
// herdr-core socket.

const hcCwd = "/tmp/hcproj"

// The claude terminals are live sessions, each named by its foreground claude's sessions/<pid>.json; other programs,
// another machine's terminals and finished ones are not; places come from the Herdr app's labels and document.
func TestHerdrCoreListsClaudeTerminals(t *testing.T) {
	n, f, hc, home := hcNode(t)
	for _, id := range []string{sidA, sidB, sidC} {
		writeTranscript(t, home, hcCwd, id, time.Now().Add(-time.Minute), `{"type":"custom-title","customTitle":"x"}`)
	}
	writeClaudePID(t, home, 4242, sidA)
	writeClaudePID(t, home, 4343, sidB)
	writeClaudePID(t, home, 4444, sidC)
	writeClaudePID(t, home, 4545, "0d000000-0000-4000-8000-0000000000ff") // another machine's: never asked about
	finished := hcTerminal("t6", "claude-code", "done", 4646, "claude", hcCwd, "", "", "")
	finished["state"] = "finished"
	hc.setTerminals(
		hcTerminal("t1", "claude-code", "working", 4242, "claude", hcCwd, "S1", "T1", "proj / 1"),
		hcTerminal("t2", "grok-build", "idle", 5000, "grok", hcCwd, "S1", "T2", "proj / 2"),
		hcTerminal("t1b", "", "", 4343, "/usr/local/bin/claude", hcCwd, "", "", ""), // has not reported yet
		hcTerminal("t3", "claude-code", "error", 4444, "claude", hcCwd, "S1", "T2", "proj / 2"),
		hcTerminal("m/t7", "claude-code", "working", 4545, "claude", hcCwd, "", "", ""),
		finished,
	)
	hc.doc = map[string]any{"spaces": []any{map[string]any{"id": "S1", "name": "", "folder": hcCwd,
		"tabs": []any{map[string]any{"id": "T1", "name": ""}, map[string]any{"id": "T2", "name": "review"}}}}}

	m := byID(listSessions(t, n))
	if len(m) != 3 {
		t.Fatalf("listed %v", ids(listSessions(t, n)))
	}
	a, b, c := m[sidA], m[sidB], m[sidC]
	if a.PaneID != "herdr-core:t1" || a.Status != proto.StatusWorking || a.SpaceKind != host.HerdrCore ||
		a.SpaceID != "S1" || a.SpaceName != "proj" || a.SpaceIndex != 1 || a.SlotID != "T1" || a.SlotName != "1" || a.SlotIndex != 1 {
		t.Errorf("t1: %+v", a)
	}
	if b.PaneID != "herdr-core:t1b" || b.Status != proto.StatusUnknown || b.SpaceID != dirSpace+hcCwd || b.SpaceName != "hcproj" || b.SlotID != "" {
		t.Errorf("t1b: %+v", b)
	}
	if c.PaneID != "herdr-core:t3" || c.Status != proto.StatusUnknown || c.SlotName != "review" || c.SlotIndex != 2 {
		t.Errorf("t3: %+v", c)
	}
	// Only the terminals that could be claude's are asked about, and only for their processes; herdr is not asked.
	for _, call := range hc.trace() {
		if !strings.HasPrefix(call, "terminal.get ") || strings.Contains(call, "m/t7") || strings.Contains(call, `"t6"`) {
			t.Errorf("herdr-core call %s", call)
		}
	}
	if tr := f.trace(); len(tr) != 0 {
		t.Errorf("herdr called: %v", tr)
	}
}

func TestHerdrCoreStatusAndAgent(t *testing.T) {
	cases := []struct {
		app, state, argv0 string
		agent, status     string
	}{
		{"claude-code", "working", "", "claude", proto.StatusWorking},
		{"claude-code", "blocked", "", "claude", proto.StatusBlocked},
		{"claude-code", "done", "", "claude", proto.StatusDone},
		{"claude-code", "idle", "", "claude", proto.StatusIdle},
		{"claude-code", "error", "", "claude", proto.StatusUnknown},
		{"codex", "working", "", "codex", proto.StatusWorking},
		{"grok-build", "idle", "", "grok-build", proto.StatusIdle},
		{"", "", "claude", "claude", proto.StatusUnknown},
		{"", "", "/opt/bin/codex", "codex", proto.StatusUnknown},
		{"", "", "zsh", "", ""},
	}
	for _, c := range cases {
		var tm herdrcore.Terminal
		tm.Address, tm.Cwd = "t1", hcCwd
		if c.app != "" {
			tm.Status = &herdrcore.Status{App: c.app, State: c.state}
		}
		if c.argv0 != "" {
			tm.Foreground = &herdrcore.Foreground{Pid: 1, Argv: []string{c.argv0}}
		}
		a := fromTerminal(tm)
		if a.Agent != c.agent || a.AgentStatus != c.status || a.PaneID != "herdr-core:t1" {
			t.Errorf("%s/%s/%s: agent %q status %q pane %q", c.app, c.state, c.argv0, a.Agent, a.AgentStatus, a.PaneID)
		}
	}
	if got := stripSpinner("✳ fix the build"); got != "fix the build" {
		t.Errorf("stripSpinner = %q", got)
	}
	if got := stripSpinner("a fix"); got != "a fix" {
		t.Errorf("stripSpinner = %q", got)
	}
}

// hcSession is a node with one claude session in herdr-core terminal t1.
func hcSession(t *testing.T) (*Node, *fakeHC) {
	t.Helper()
	n, _, hc, home := hcNode(t)
	writeTranscript(t, home, hcCwd, sidA, time.Now().Add(-time.Minute), `{"type":"custom-title","customTitle":"x"}`)
	writeClaudePID(t, home, 4242, sidA)
	hc.setTerminals(hcTerminal("t1", "claude-code", "idle", 4242, "claude", hcCwd, "", "", ""))
	if m := byID(listSessions(t, n)); m[sidA].PaneID != "herdr-core:t1" {
		t.Fatalf("not listed: %v", m)
	}
	hc.reset()
	return n, hc
}

// A send reads the plain screen once (no colors: only a dialog refuses it, a draft cannot be told from the
// placeholder) and pastes and submits in one terminal.send; nothing is pressed after it.
func TestHerdrCoreSendChecksForADialogThenSends(t *testing.T) {
	withSubmitGrace(t, 50*time.Millisecond)
	n, hc := hcSession(t)
	hc.setScreen("t1", screenFixture(t, "claude-2.1.312-typed-162.txt"))
	if _, err := request(t, n, "session.send", map[string]any{"session_id": sidA, "text": "hello"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond) // past the grace: ensureSubmitted has nothing colored to read
	assertTrace(t, hc.trace(), []string{
		call("terminal.read", map[string]any{"terminal": "t1", "lines": 0}),
		call("terminal.send", map[string]any{"terminal": "t1", "text": "hello"}),
	})

	hc.reset()
	hc.setScreen("t1", "Do you want to proceed?\n❯ 1. Yes\n  2. No\n\nEsc to cancel\n")
	_, err := request(t, n, "session.send", map[string]any{"session_id": sidA, "text": "hello"})
	if code, _ := refusalOf(t, err); code != "dialog_open" {
		t.Fatalf("send into a dialog: %v", err)
	}
	assertTrace(t, hc.trace(), []string{call("terminal.read", map[string]any{"terminal": "t1", "lines": 0})})
}

// herdr-core refusing a blocked program reaches the caller as an error, after which nothing more is sent.
func TestHerdrCoreSendBlocked(t *testing.T) {
	n, hc := hcSession(t)
	hc.handle("terminal.send", func(map[string]any) any { return hcFail{"blocked"} })
	_, err := request(t, n, "session.send", map[string]any{"session_id": sidA, "text": "hello"})
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("send = %v", err)
	}
}

// Keys go through as named; text without a submit is not something herdr-core can type.
func TestHerdrCoreKeysAndText(t *testing.T) {
	n, hc := hcSession(t)
	if _, err := request(t, n, "session.keys", map[string]any{"session_id": sidA, "keys": []string{"esc", "shift+tab"}}); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, hc.trace(), []string{call("terminal.keys", map[string]any{"terminal": "t1", "keys": []any{"esc", "shift+tab"}})})
	hc.reset()
	_, err := request(t, n, "session.keys", map[string]any{"session_id": sidA, "text": "/effort"})
	if err == nil || !strings.HasPrefix(err.Error(), "unsupported") || !errors.Is(err, host.ErrUnsupported) {
		t.Fatalf("typing text: %v", err)
	}
	if tr := hc.trace(); len(tr) != 0 {
		t.Errorf("typing text called %v", tr)
	}
}

// A colored screen read gets the plain rows (CRLF-ended like colored ones); "recent" asks for history.
func TestHerdrCoreScreenIsPlain(t *testing.T) {
	n, hc := hcSession(t)
	hc.setScreen("t1", "line one\nline two\n")
	res, err := request(t, n, "session.screen", map[string]any{"session_id": sidA, "format": "ansi"})
	if err != nil {
		t.Fatal(err)
	}
	if res["text"] != "line one\r\nline two\r\n" || res["status"] != proto.StatusIdle {
		t.Errorf("screen = %q", res)
	}
	if _, err := request(t, n, "session.screen", map[string]any{"session_id": sidA, "source": "recent", "lines": 500}); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, hc.trace(), []string{
		call("terminal.read", map[string]any{"terminal": "t1", "lines": 0}),
		call("terminal.read", map[string]any{"terminal": "t1", "lines": 500}),
	})
}

// A live stream is not available: the hub falls back to screen reads.
func TestHerdrCoreHasNoTerminalStream(t *testing.T) {
	n, _ := hcSession(t)
	// herdr can stream here (the process-wide cache terminal_test.go sets the same way): only herdr-core cannot.
	observeSupport.once.Do(func() { observeSupport.ok = true })
	_, err := request(t, n, "terminal.watch", map[string]any{"session_id": sidA})
	if err == nil || !strings.HasPrefix(err.Error(), "unsupported") {
		t.Fatalf("terminal.watch = %v", err)
	}
}

// Events from herdr-core take a snapshot at once: the subscription's start, then each change but size / keep and
// another machine's.
func TestHerdrCoreEventsKickARefresh(t *testing.T) {
	n, _, hc, _ := hcNode(t)
	n.events = newHerdrEvents()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go n.watchHost(ctx, n.hosts.Get(host.HerdrCore).(host.Watcher))
	select {
	case <-hc.subbed:
	case <-time.After(3 * time.Second):
		t.Fatal("never subscribed")
	}
	kicked := func() bool {
		select {
		case <-n.events.kick:
			return true
		case <-time.After(300 * time.Millisecond):
			return false
		}
	}
	if !kicked() {
		t.Fatal("no snapshot once the stream started")
	}
	hc.push("event", map[string]any{"type": "size", "terminal": "t1", "seq": 1, "data": map[string]any{}})
	hc.push("event", map[string]any{"type": "status", "terminal": "m/t1", "seq": 2, "data": map[string]any{}})
	hc.push("heartbeat", map[string]any{"seq": 2})
	if kicked() {
		t.Fatal("a size / another machine's / heartbeat event took a snapshot")
	}
	hc.push("event", map[string]any{"type": "status", "terminal": "t1", "seq": 3, "data": map[string]any{}})
	if !kicked() {
		t.Fatal("a status event took no snapshot")
	}
	hc.push("missed", map[string]any{"seq": 9, "terminals": []any{}})
	if !kicked() {
		t.Fatal("missed events took no snapshot")
	}
}

// herdr-core not running: no sessions, at once, nothing logged (and the refresh succeeds: nothing runs there); one
// that went away takes its sessions with it.
func TestHerdrCoreAbsentOrGone(t *testing.T) {
	n, _, hc, home := hcNode(t)
	writeClaudePID(t, home, 4242, sidB)
	writeTranscript(t, home, hcCwd, sidB, time.Now().Add(-time.Minute), `{"type":"custom-title","customTitle":"x"}`)
	hc.setTerminals(hcTerminal("t1", "claude-code", "idle", 4242, "claude", hcCwd, "", "", ""))
	if err := n.refreshHosts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := n.agent(sidB); !ok {
		t.Fatal("herdr-core's session not found")
	}
	if err := os.Remove(hc.sock); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second) // past the failed list kept for Agents
	if err := n.refreshHosts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := n.agent(sidB); ok {
		t.Error("herdr-core stopped, its session is still live")
	}

	var logged []string
	var mu sync.Mutex
	c := &herdrcore.Client{Socket: "/tmp/jarvis-no-such-dir/c.sock", Log: func(format string, args ...any) {
		mu.Lock()
		logged = append(logged, fmt.Sprintf(format, args...))
		mu.Unlock()
	}}
	n.hosts.Set(&herdrCoreHost{c: c})
	start := time.Now()
	for range 3 {
		if err := n.refreshHosts(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("3 refreshes took %s", took)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(logged) != 0 {
		t.Errorf("logged %q", logged)
	}
}

// With herdr-core as the engine, a terminal running classic herdr's client is not claude's, whatever state passes
// through it.
func TestHerdrCoreSkipsANestedHerdr(t *testing.T) {
	n, _, hc, home := hcNode(t)
	writeClaudePID(t, home, 4242, sidA)
	writeTranscript(t, home, hcCwd, sidA, time.Now().Add(-time.Minute), `{"type":"custom-title","customTitle":"x"}`)
	nested := hcTerminal("t1", "claude-code", "working", 4242, "/usr/local/bin/herdr", hcCwd, "", "", "")
	hc.setTerminals(nested)
	if list := listSessions(t, n); len(byID(list)) != 0 && byID(list)[sidA].Status != proto.StatusOffline {
		t.Errorf("a nested herdr became a session: %+v", byID(list)[sidA])
	}
	if _, ok := n.agent(sidA); ok {
		t.Error("a nested herdr is an agent")
	}
	for _, fg := range []herdrcore.Foreground{{Pid: 1, Name: "herdr"}, {Pid: 1, Name: "x", Argv: []string{"herdr"}}} {
		tm := herdrcore.Terminal{Address: "t2", Status: &herdrcore.Status{App: "claude-code", State: "idle"}, Foreground: &fg}
		if a := fromTerminal(tm); a.Agent != "" {
			t.Errorf("%+v: agent %q", fg, a.Agent)
		}
	}
}

func TestHookPane(t *testing.T) {
	env := func(kv ...string) func(string) string {
		return func(k string) string {
			for i := 0; i+1 < len(kv); i += 2 {
				if kv[i] == k {
					return kv[i+1]
				}
			}
			return ""
		}
	}
	for _, c := range []struct {
		env  func(string) string
		want string
	}{
		{env("HERDR_PANE_ID", "w1:p2", "HC_TERMINAL_ID", "t7"), "w1:p2"},
		{env("HC_TERMINAL_ID", "t7"), "herdr-core:t7"},
		{env(), ""},
	} {
		if got := HookPane(c.env); got != c.want {
			t.Errorf("HookPane = %q, want %q", got, c.want)
		}
	}
}
