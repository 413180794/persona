package node

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/host"
	"github.com/chyroc/jarvis/server/internal/orcart"
	"github.com/chyroc/jarvis/server/internal/orcart/orcarttest"
	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
)

// Orca as the machine's engine (hostorca.go, engine.go), driven through the node's requests against a fake Orca
// runtime socket (orcarttest) and a fake process table. No test here talks to a real Orca.

func TestMain(m *testing.M) {
	// No test may reach the user's running Orca: the default metadata path points nowhere.
	os.Setenv("ORCA_USER_DATA_PATH", "/tmp/jarvis-test-no-orca")
	os.Exit(m.Run())
}

const orcaCwd = "/tmp/orcaproj"

// fakeProcs is the process table the Orca host scans: agent processes by pane key.
type fakeProcs struct {
	mu    sync.Mutex
	procs map[string][]orcaProc
	err   error
	scans int
}

func (p *fakeProcs) scan() (map[string][]orcaProc, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.scans++
	out := map[string][]orcaProc{}
	for k, v := range p.procs {
		out[k] = slices.Clone(v)
	}
	return out, p.err
}

func (p *fakeProcs) set(pane string, procs ...orcaProc) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.procs == nil {
		p.procs = map[string][]orcaProc{}
	}
	if len(procs) == 0 {
		delete(p.procs, pane)
		return
	}
	p.procs[pane] = procs
}

// orcaNode is a Node whose engine is Orca, on a fake runtime and process table; the fake herdr it also gets
// (herdrNode's) is not registered, so a call to it would show in its trace.
func orcaNode(t *testing.T) (*Node, *fakeHerdr, *orcarttest.Fake, *fakeProcs, string) {
	t.Helper()
	n, f, home := herdrNode(t)
	o := orcarttest.New(t)
	procs := &fakeProcs{}
	h := newOrcaHost(&orcart.Client{MetaPath: o.MetaPath})
	h.scan = procs.scan
	n.hosts = host.NewRegistry(h)
	n.eng.name = proto.EngineOrca
	return n, f, o, procs, home
}

func orcaTerm(handle, tab, leaf, identity string) map[string]any {
	return orcarttest.Terminal(handle, tab, leaf, "repo::"+orcaCwd, orcaCwd, "refs/heads/feature-x", "✳ "+handle, identity)
}

// The claude terminals are live sessions, each named by its claude process's sessions/<pid>.json (found by the pane key
// in its environment); a terminal whose claude exited (Orca still says claude), a shell and another machine's terminal
// are not; spaces are worktrees named by branch, slots their tabs; states come from worktree.ps (waiting → blocked).
func TestOrcaListsClaudeTerminals(t *testing.T) {
	n, f, o, procs, home := orcaNode(t)
	for _, id := range []string{sidA, sidB, sidC} {
		writeTranscript(t, home, orcaCwd, id, time.Now().Add(-time.Minute), `{"type":"custom-title","customTitle":"x"}`)
	}
	writeClaudePID(t, home, 4242, sidA)
	writeClaudePID(t, home, 4343, sidB)
	writeClaudePID(t, home, 4444, sidC)
	remote := orcaTerm("term_r", "tabR", "leafR", "claude")
	remote["executionHostId"] = "ssh-example-machine"
	o.SetTerminals(
		orcaTerm("term_1", "tab1", "leaf1", "claude"),
		orcaTerm("term_2", "tab1", "leaf2", "claude"),
		orcaTerm("term_3", "tab2", "leaf3", "claude"), // no row yet: unknown
		orcaTerm("term_4", "tab2", "leaf4", "claude"), // its claude exited
		orcaTerm("term_5", "tab3", "leaf5", ""),       // a shell
		remote,
	)
	o.SetWorktrees(orcarttest.Worktree("repo::"+orcaCwd, orcaCwd, "refs/heads/feature-x",
		orcarttest.Agent("tab1:leaf1", "claude", "working"),
		orcarttest.Agent("tab1:leaf2", "claude", "waiting"),
		orcarttest.Agent("tab2:leaf4", "claude", "done"),
	))
	procs.set("tab1:leaf1", orcaProc{pid: 4242, agent: "claude"})
	procs.set("tab1:leaf2", orcaProc{pid: 4343, agent: "claude"})
	procs.set("tab2:leaf3", orcaProc{pid: 4444, agent: "claude"})
	procs.set("tabR:leafR", orcaProc{pid: 4545, agent: "claude"}) // cannot happen, but must not matter

	list := listSessions(t, n)
	m := byID(list)
	a, b, c := m[sidA], m[sidB], m[sidC]
	if a.PaneID != "orca:tab1:leaf1" || a.Status != proto.StatusWorking || a.SpaceKind != host.Orca ||
		a.SpaceID != "repo::"+orcaCwd || a.SpaceName != "feature-x" || a.SpaceIndex != 1 || a.SlotID != "tab1" ||
		a.SlotName != "tab tab1" || a.SlotIndex != 1 {
		t.Errorf("term_1: %+v", a)
	}
	if b.PaneID != "orca:tab1:leaf2" || b.Status != proto.StatusBlocked {
		t.Errorf("term_2: %+v", b)
	}
	if c.PaneID != "orca:tab2:leaf3" || c.Status != proto.StatusUnknown || c.SlotIndex != 2 {
		t.Errorf("term_3: %+v", c)
	}
	live := 0
	for _, s := range list {
		if s.PaneID != "" {
			live++
		}
	}
	if live != 3 {
		t.Errorf("%d live sessions: %v", live, ids(list))
	}
	// Listing reads; it neither types nor reads screens; herdr is not asked.
	if tr := o.Trace(); len(tr) != 0 {
		t.Errorf("orca calls beyond listing: %v", tr)
	}
	if tr := f.trace(); len(tr) != 0 {
		t.Errorf("herdr called: %v", tr)
	}
}

// When the processes cannot be listed, Orca's own word decides: its agent identity and a live agent row.
func TestOrcaWithoutProcessScan(t *testing.T) {
	v := &orcaView{rows: map[string]orcart.AgentRow{
		"t:a": {PaneKey: "t:a", AgentType: "claude", State: "done", HasLivePty: true},
		"t:b": {PaneKey: "t:b", AgentType: "claude", State: "working"},
	}}
	term := func(leaf, identity string) orcart.Terminal {
		return orcart.Terminal{Handle: "h", TabID: "t", LeafID: leaf, AgentIdentity: identity}
	}
	if a := v.agent(term("a", "claude")); a.Agent != "claude" || a.AgentStatus != proto.StatusDone {
		t.Errorf("a: %+v", a)
	}
	for _, tm := range []orcart.Terminal{term("b", "claude"), term("a", ""), term("c", "claude")} {
		if a := v.agent(tm); a.Agent != "" {
			t.Errorf("%s: an agent %+v", tm.LeafID, a)
		}
	}
	// With the scan, a codex process is codex; its row's state counts only when the row is codex's.
	v = &orcaView{procsKnown: true, procs: map[string][]orcaProc{"t:a": {{pid: 1, agent: "codex"}}},
		rows: map[string]orcart.AgentRow{"t:a": {AgentType: "claude", State: "working"}}}
	if a := v.agent(term("a", "codex")); a.Agent != "codex" || a.AgentStatus != proto.StatusUnknown {
		t.Errorf("codex: %+v", a)
	}
	// The row folds in its subagents (working); the pane's own claude finished its turn.
	row := orcart.AgentRow{AgentType: "claude", State: "working"}
	row.MainAgent = &struct {
		State string `json:"state"`
	}{State: "done"}
	v = &orcaView{procsKnown: true, procs: map[string][]orcaProc{"t:a": {{pid: 1, agent: "claude"}}},
		rows: map[string]orcart.AgentRow{"t:a": row}}
	if a := v.agent(term("a", "claude")); a.AgentStatus != proto.StatusDone {
		t.Errorf("main agent done, children working: %+v", a)
	}
}

// orcaSession is a node with one claude session in Orca's terminal term_1 (pane tab1:leaf1).
func orcaSession(t *testing.T) (*Node, *orcarttest.Fake, *fakeProcs) {
	t.Helper()
	n, _, o, procs, home := orcaNode(t)
	writeTranscript(t, home, orcaCwd, sidA, time.Now().Add(-time.Minute), `{"type":"custom-title","customTitle":"x"}`)
	writeClaudePID(t, home, 4242, sidA)
	o.SetTerminals(orcaTerm("term_1", "tab1", "leaf1", "claude"))
	o.SetWorktrees(orcarttest.Worktree("repo::"+orcaCwd, orcaCwd, "refs/heads/feature-x",
		orcarttest.Agent("tab1:leaf1", "claude", "done")))
	procs.set("tab1:leaf1", orcaProc{pid: 4242, agent: "claude"})
	if m := byID(listSessions(t, n)); m[sidA].PaneID != "orca:tab1:leaf1" {
		t.Fatalf("not listed: %v", m)
	}
	o.Reset()
	return n, o, procs
}

// A send reads the plain screen once (only a dialog refuses it: no colors to tell a draft from the placeholder),
// checks the claude still runs in the pane, and goes as one agent prompt (Orca pastes and submits); nothing after.
func TestOrcaSendChecksForADialogThenPrompts(t *testing.T) {
	withSubmitGrace(t, 50*time.Millisecond)
	n, o, _ := orcaSession(t)
	o.SetScreen("term_1", screenFixture(t, "claude-2.1.312-typed-162.txt"))
	if _, err := request(t, n, "session.send", map[string]any{"session_id": sidA, "text": "hello"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond) // past the grace: nothing colored to check, no enter pressed after
	assertTrace(t, o.Trace(), []string{
		`terminal.read {"screen":true,"terminal":"term_1"}`,
		orcarttest.SendCall("term_1", map[string]any{"text": "hello", "enter": true, "agentPrompt": true}),
	})

	o.Reset()
	o.SetScreen("term_1", "Do you want to proceed?\n❯ 1. Yes\n  2. No\n\nEsc to cancel\n")
	_, err := request(t, n, "session.send", map[string]any{"session_id": sidA, "text": "hello"})
	if code, _ := refusalOf(t, err); code != "dialog_open" {
		t.Fatalf("send into a dialog: %v", err)
	}
	if tr := o.Trace("terminal.send"); len(tr) != 0 {
		t.Errorf("typed into a dialog: %v", tr)
	}
}

// Orca turning the input down (nothing written) is a refusal the app can retry: a permission dialog is dialog_open,
// no agent in front / a gone or read-only terminal is not_delivered.
func TestOrcaSendRefusedByOrca(t *testing.T) {
	n, o, _ := orcaSession(t)
	o.SetScreen("term_1", "❯ \n")
	for _, c := range []struct {
		answer any
		code   string
	}{
		{map[string]any{"send": map[string]any{"accepted": false, "bytesWritten": 0, "refusedReason": "permission"}}, "dialog_open"},
		{map[string]any{"send": map[string]any{"accepted": false, "bytesWritten": 0, "refusedReason": "no-agent"}}, "not_delivered"},
		{orcarttest.Fail{Code: "terminal_not_writable"}, "not_delivered"},
	} {
		o.Handle("terminal.send", func(map[string]any) any { return c.answer })
		_, err := request(t, n, "session.send", map[string]any{"session_id": sidA, "text": "hello"})
		if got, data := refusalOf(t, err); got != c.code || data["delivered"] != "none" {
			t.Errorf("%v: %v %v", c.answer, got, data)
		}
	}
	// An error that leaves open whether Orca wrote: not a clean refusal.
	o.Handle("terminal.send", func(map[string]any) any { return orcarttest.Fail{Code: "runtime_error"} })
	_, err := request(t, n, "session.send", map[string]any{"session_id": sidA, "text": "hello"})
	var re *rpc.Error
	if err == nil || errors.As(err, &re) && re.Code == "not_delivered" {
		t.Errorf("runtime_error: %v", err)
	}
}

// The claude exited and the pane is back at its shell (Orca may still name claude): the shell does not get the
// message as a command — refused before anything is typed, and the session is no longer live.
func TestOrcaSendAfterTheAgentExited(t *testing.T) {
	n, o, procs := orcaSession(t)
	o.SetScreen("term_1", "$ \n")
	procs.set("tab1:leaf1")
	_, err := request(t, n, "session.send", map[string]any{"session_id": sidA, "text": "rm -rf build"})
	if code, _ := refusalOf(t, err); code != "not_delivered" {
		t.Fatalf("send to an exited agent: %v", err)
	}
	if tr := o.Trace("terminal.send"); len(tr) != 0 {
		t.Errorf("typed: %v", tr)
	}
	time.Sleep(orcaProcsMaxAge)
	if m := byID(listSessions(t, n)); m[sidA].PaneID != "" {
		t.Errorf("an exited agent still listed live: %+v", m[sidA])
	}
}

// Keys go as their bytes, one write each; text is typed as is, then its keys; an unknown key is refused before
// anything is pressed.
func TestOrcaKeysAndText(t *testing.T) {
	n, o, _ := orcaSession(t)
	if _, err := request(t, n, "session.keys", map[string]any{"session_id": sidA, "keys": []string{"esc", "shift+tab", "ctrl+u", "up"}}); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, o.Trace(), []string{
		orcarttest.SendCall("term_1", map[string]any{"text": "\x1b"}),
		orcarttest.SendCall("term_1", map[string]any{"text": "\x1b[Z"}),
		orcarttest.SendCall("term_1", map[string]any{"text": "\x15"}),
		orcarttest.SendCall("term_1", map[string]any{"text": "\x1b[A"}),
	})
	o.Reset()
	if _, err := request(t, n, "session.keys", map[string]any{"session_id": sidA, "text": "yes please", "keys": []string{"enter"}}); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, o.Trace(), []string{
		orcarttest.SendCall("term_1", map[string]any{"text": "yes please"}),
		orcarttest.SendCall("term_1", map[string]any{"text": "\r"}),
	})
	o.Reset()
	if _, err := request(t, n, "session.keys", map[string]any{"session_id": sidA, "keys": []string{"enter", "hyper+q"}}); err == nil {
		t.Error("an unknown key was taken")
	}
	if tr := o.Trace(); len(tr) != 0 {
		t.Errorf("pressed before refusing: %v", tr)
	}
	for k, want := range map[string]string{"enter": "\r", "backspace": "\x7f", "ctrl+c": "\x03", "tab": "\t", "1": "1",
		"y": "y", "down": "\x1b[B", "space": " "} {
		if got, ok := orcaKeyBytes(k); !ok || got != want {
			t.Errorf("%s = %q", k, got)
		}
	}
}

// A colored screen read gets the plain rows (CRLF-ended like colored ones); "recent" reads the screen too.
func TestOrcaScreenIsPlain(t *testing.T) {
	n, o, _ := orcaSession(t)
	o.SetScreen("term_1", "line one\nline two\n")
	res, err := request(t, n, "session.screen", map[string]any{"session_id": sidA, "format": "ansi"})
	if err != nil {
		t.Fatal(err)
	}
	if res["text"] != "line one\r\nline two\r\n" || res["status"] != proto.StatusDone {
		t.Errorf("screen = %q", res)
	}
	if _, err := request(t, n, "session.screen", map[string]any{"session_id": sidA, "source": "recent", "lines": 500}); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, o.Trace(), []string{
		`terminal.read {"screen":true,"terminal":"term_1"}`,
		`terminal.read {"screen":true,"terminal":"term_1"}`,
	})
	if _, err := request(t, n, "terminal.watch", map[string]any{"session_id": sidA}); err == nil ||
		!strings.HasPrefix(err.Error(), "unsupported") {
		t.Errorf("terminal.watch = %v", err)
	}
}

// A handle Orca no longer knows (the terminal came back under a new one) is looked up again by the pane key.
func TestOrcaStaleHandle(t *testing.T) {
	n, o, _ := orcaSession(t)
	o.SetTerminals(orcaTerm("term_1b", "tab1", "leaf1", "claude"))
	o.SetScreen("term_1b", "hi\n")
	res, err := request(t, n, "session.screen", map[string]any{"session_id": sidA})
	if err != nil || res["text"] != "hi\n" {
		t.Fatalf("screen = %v, %v", res, err)
	}
	assertTrace(t, o.Trace("terminal.read"), []string{
		`terminal.read {"screen":true,"terminal":"term_1"}`,
		`terminal.read {"screen":true,"terminal":"term_1b"}`,
	})
}

// session.close: ctrl+c twice, then terminal.close (Orca closes the pane, its tab when it was the only one). A
// terminal already gone counts as closed.
func TestOrcaClose(t *testing.T) {
	n, o, _ := orcaSession(t)
	if _, err := request(t, n, "session.close", map[string]any{"session_id": sidA}); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, o.Trace(), []string{
		orcarttest.SendCall("term_1", map[string]any{"text": "\x03"}),
		orcarttest.SendCall("term_1", map[string]any{"text": "\x03"}),
		`terminal.close {"terminal":"term_1"}`,
	})

	n, o, _ = orcaSession(t)
	o.SetTerminals() // gone before the close
	if _, err := request(t, n, "session.close", map[string]any{"session_id": sidA}); err != nil {
		t.Errorf("close of a gone terminal: %v", err)
	}
	if tr := o.Trace("terminal.close"); len(tr) != 0 {
		t.Errorf("closed a terminal that is not there: %v", tr)
	}
}

// Orca owns its worktrees' and tabs' names: renaming them is unsupported, with nothing sent to Orca.
func TestOrcaRenameUnsupported(t *testing.T) {
	n, o, _ := orcaSession(t)
	for _, req := range []struct {
		method string
		params map[string]any
	}{
		{"space.rename", map[string]any{"workspace_id": "repo::" + orcaCwd, "name": "backend"}},
		{"session.rename", map[string]any{"session_id": sidA, "tab_name": "review"}},
	} {
		_, err := request(t, n, req.method, req.params)
		if err == nil || !strings.Contains(err.Error(), "unsupported") || !strings.Contains(err.Error(), "不能从手机改名") {
			t.Errorf("%s: %v", req.method, err)
		}
	}
	if tr := o.Trace(); len(tr) != 0 {
		t.Errorf("orca called: %v", tr)
	}
}

// Orca not running (no metadata, or its socket gone): no sessions at once, nothing logged, the refresh succeeds.
func TestOrcaAbsentOrGone(t *testing.T) {
	n, _, o, procs, home := orcaNode(t)
	writeClaudePID(t, home, 4242, sidB)
	writeTranscript(t, home, orcaCwd, sidB, time.Now().Add(-time.Minute), `{"type":"custom-title","customTitle":"x"}`)
	o.SetTerminals(orcaTerm("term_1", "tab1", "leaf1", "claude"))
	procs.set("tab1:leaf1", orcaProc{pid: 4242, agent: "claude"})
	if err := n.refreshHosts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := n.agent(sidB); !ok {
		t.Fatal("Orca's session not found")
	}
	o.Stop()
	time.Sleep(time.Second) // past the failed list kept for Agents
	if err := n.refreshHosts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := n.agent(sidB); ok {
		t.Error("Orca quit, its session is still live")
	}

	var logged []string
	var mu sync.Mutex
	c := &orcart.Client{MetaPath: "/tmp/jarvis-no-such-dir/orca-runtime.json", Log: func(format string, args ...any) {
		mu.Lock()
		logged = append(logged, fmt.Sprintf(format, args...))
		mu.Unlock()
	}}
	h := newOrcaHost(c)
	h.scan = procs.scan
	n.hosts.Set(h)
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

// Orca pushes nothing on its socket: with Orca as the engine no change stream starts, and the poll takes a snapshot
// every second (not herdr's slow snapshot, which relies on its events).
func TestOrcaPollsEverySecond(t *testing.T) {
	n, _, o, _, _ := orcaNode(t)
	_, stop := pollNode(t, n)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n.watchEngine(ctx)
	time.Sleep(3200 * time.Millisecond)
	stop()
	if got := o.Count("terminal.list"); got < 3 {
		t.Errorf("%d snapshots in 3 s", got)
	}
	if n.events.isLive() {
		t.Error("a change stream counts as live for Orca")
	}
}

// node.info finds Orca when its runtime answers with a protocol Jarvis speaks; an incompatible one is there, not
// available, with a note.
func TestOrcaEngineDetection(t *testing.T) {
	o := orcarttest.New(t)
	e := orcaEngine(context.Background(), &orcart.Client{MetaPath: o.MetaPath})
	if e != (proto.Engine{ID: "orca", Name: "Orca", Available: true, Version: "1.4.245"}) {
		t.Errorf("engine %+v", e)
	}
	o.SetStatus(map[string]any{"runtimeProtocolVersion": 2})
	e = orcaEngine(context.Background(), &orcart.Client{MetaPath: o.MetaPath})
	if e.Available || e.Note == "" || e.Version != "1.4.245" {
		t.Errorf("incompatible: %+v", e)
	}
	e = orcaEngine(context.Background(), &orcart.Client{MetaPath: "/tmp/jarvis-no-such-dir/orca-runtime.json"})
	if e != (proto.Engine{ID: "orca", Name: "Orca"}) {
		t.Errorf("absent: %+v", e)
	}
	if hs := engineHosts(proto.EngineOrca, nil); len(hs) != 1 || hs[0].Kind() != host.Orca {
		t.Errorf("orca hosts %v", hs)
	}
}

// engine.use orca swaps the hosts for Orca's and lists its sessions; back to herdr drops them.
func TestOrcaEngineUse(t *testing.T) {
	n, _, home := herdrNode(t)
	o := orcarttest.New(t)
	t.Setenv("ORCA_USER_DATA_PATH", strings.TrimSuffix(o.MetaPath, "/orca-runtime.json"))
	writeClaudePID(t, home, 4242, sidB)
	writeTranscript(t, home, orcaCwd, sidB, time.Now().Add(-time.Minute), `{"type":"custom-title","customTitle":"x"}`)
	o.SetTerminals(orcaTerm("term_1", "tab1", "leaf1", "claude"))
	o.SetWorktrees(orcarttest.Worktree("repo::"+orcaCwd, orcaCwd, "", orcarttest.Agent("tab1:leaf1", "claude", "working")))
	procs := &fakeProcs{}
	procs.set("tab1:leaf1", orcaProc{pid: 4242, agent: "claude"})
	old := orcaScan
	orcaScan = procs.scan
	t.Cleanup(func() { orcaScan = old })
	if _, err := request(t, n, "engine.use", map[string]any{"engine": "orca"}); err != nil {
		t.Fatal(err)
	}
	h, ok := n.hosts.Get(host.Orca).(*orcaHost)
	if !ok || len(n.hosts.All()) != 1 {
		t.Fatalf("hosts %v", hostKinds(n))
	}
	if m := byID(listSessions(t, n)); m[sidB].PaneID != "orca:tab1:leaf1" || m[sidB].Status != proto.StatusWorking ||
		m[sidB].SpaceName != "orcaproj" {
		t.Errorf("sessions %v", m)
	}
	if n.defaultHost() != host.Host(h) {
		t.Error("Orca is not the default host")
	}
	if _, err := request(t, n, "engine.use", map[string]any{"engine": "herdr"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := n.agent(sidB); ok {
		t.Error("Orca's session kept after switching back")
	}
}

// The process scan keeps only the pane key out of an environment listing (never the hook token), finds claude and
// codex under node too, falls back on ORCA_AGENT_PANE, and lists claude first.
func TestOrcaProcessParsing(t *testing.T) {
	ps := strings.Join([]string{
		"  101 /usr/local/bin/claude --resume x TERM=xterm ORCA_AGENT_HOOK_TOKEN=secret ORCA_PANE_KEY=tab1:leaf1 HOME=/h",
		"  102 node /opt/lib/node_modules/@anthropic-ai/claude-code/cli.js ORCA_AGENT_PANE=tab2:leaf2",
		"  103 /opt/bin/codex ORCA_PANE_KEY=tab1:leaf1",
		"  104 -zsh ORCA_PANE_KEY=tab3:leaf3",
		"  105 /usr/local/bin/claude HOME=/h",
		"  100 node /opt/lib/node_modules/@openai/codex/bin/codex.js ORCA_PANE_KEY=tab4:leaf4",
		"garbage",
	}, "\n")
	got := parseOrcaPS(ps)
	want := map[string][]orcaProc{
		"tab1:leaf1": {{pid: 101, agent: "claude"}, {pid: 103, agent: "codex"}},
		"tab2:leaf2": {{pid: 102, agent: "claude"}},
		"tab4:leaf4": {{pid: 100, agent: "codex"}},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("parsed %v\nwant %v", got, want)
	}
	if strings.Contains(fmt.Sprintf("%+v", got), "secret") {
		t.Error("the hook token was kept")
	}
}

func TestHookPaneOrca(t *testing.T) {
	env := func(kv map[string]string) func(string) string { return func(k string) string { return kv[k] } }
	for _, c := range []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"ORCA_PANE_KEY": "tab1:leaf1"}, "orca:tab1:leaf1"},
		{map[string]string{"ORCA_AGENT_PANE": "tab1:leaf1"}, "orca:tab1:leaf1"},
		{map[string]string{"HERDR_PANE_ID": "w1:p2", "ORCA_PANE_KEY": "tab1:leaf1"}, "w1:p2"},
		{map[string]string{"HC_TERMINAL_ID": "t7", "ORCA_PANE_KEY": "tab1:leaf1"}, "herdr-core:t7"},
	} {
		if got := HookPane(env(c.env)); got != c.want {
			t.Errorf("%v: HookPane = %q, want %q", c.env, got, c.want)
		}
	}
	if r := host.ParsePane("orca:tab1:leaf1"); r != (host.PaneRef{Kind: host.Orca, ID: "tab1:leaf1"}) {
		t.Errorf("ParsePane = %+v", r)
	}
}
