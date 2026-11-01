package node

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/herdr"
	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
)

// Characterization tests of how the node discovers sessions in herdr: which sessions a herdr state lists, and the
// herdr calls it takes to find them. They pin today's behaviour ahead of the terminal-host abstraction.

const (
	sidA = "0d000000-0000-4000-8000-00000000000a"
	sidB = "0d000000-0000-4000-8000-00000000000b"
	sidC = "0d000000-0000-4000-8000-00000000000c"
)

func byID(list []proto.Session) map[string]proto.Session {
	m := map[string]proto.Session{}
	for _, s := range list {
		m[s.SessionID] = s
	}
	return m
}

func ids(list []proto.Session) []string {
	var out []string
	for _, s := range list {
		out = append(out, s.SessionID)
	}
	sort.Strings(out)
	return out
}

// writeTranscript writes ~/.claude/projects/<cwd as claude names it>/<id>.jsonl with lines, modified at mtime.
func writeTranscript(t *testing.T, home, cwd, id string, mtime time.Time, lines ...string) string {
	t.Helper()
	dir := filepath.Join(home, ".claude", "projects", nonAlnum.ReplaceAllString(cwd, "-"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return p
}

// One snapshot: claude agents become sessions placed in their workspace (space) and tab (slot); agents of other
// kinds are not listed; a tab the snapshot does not name leaves the slot's name empty.
func TestHerdrListPlacesAgentsInSpacesAndSlots(t *testing.T) {
	n, f, _ := herdrNode(t)
	f.reply("session.snapshot", snapshot(
		[]any{map[string]any{"workspace_id": "w1", "label": "repo", "number": 1}},
		[]any{
			map[string]any{"tab_id": "w1:t1", "workspace_id": "w1", "label": "agent-1", "number": 1},
			map[string]any{"tab_id": "w1:t2", "workspace_id": "w1", "label": "review", "number": 2},
		},
		[]any{
			claudeAgent(sidA, proto.StatusWorking, "w1:p1", "w1:t1", "w1", "/tmp/repo", "fix the build"),
			claudeAgent(sidB, proto.StatusIdle, "w1:p2", "w1:t9", "w1", "/tmp/repo", "Claude Code"),
			map[string]any{"agent": "pi", "agent_status": "idle", "pane_id": "w1:p3", "tab_id": "w1:t2", "workspace_id": "w1",
				"agent_session": map[string]any{"kind": "id", "value": sidC}},
		},
		nil))

	list := n.sessionList(context.Background(), true)
	if got := ids(list); strings.Join(got, ",") != sidA+","+sidB {
		t.Fatalf("listed %v", got)
	}
	m := byID(list)
	a := m[sidA]
	if a.SpaceKind != "herdr" || a.SpaceID != "w1" || a.SpaceName != "repo" || a.SpaceIndex != 1 ||
		a.SlotID != "w1:t1" || a.SlotName != "agent-1" || a.SlotIndex != 1 {
		t.Errorf("A placed in %+v", a)
	}
	if a.Status != proto.StatusWorking || a.PaneID != "w1:p1" || a.TabID != "w1:t1" || a.WorkspaceID != "w1" ||
		a.Title != "fix the build" || a.Cwd != "/tmp/repo" || a.StartedByJarvis {
		t.Errorf("A = %+v", a)
	}
	b := m[sidB]
	// Not in the snapshot's tabs: slot id from the agent, no name / index. Title "Claude Code" stays without a
	// transcript (untitled only runs when there is one).
	if b.SlotID != "w1:t9" || b.SlotName != "" || b.SlotIndex != 0 || b.SpaceName != "repo" || b.Status != proto.StatusIdle ||
		b.Title != "Claude Code" {
		t.Errorf("B = %+v", b)
	}
	assertTrace(t, f.trace(), []string{call("session.snapshot", nil)})

	// Without refresh the list is built from what the last snapshot said: no herdr call.
	f.reset()
	if got := ids(n.sessionList(context.Background(), false)); len(got) != 2 {
		t.Fatalf("cached list %v", got)
	}
	assertTrace(t, f.trace(), nil)
}

// An older herdr without session.snapshot: the agent list is used instead, and the space / slot names stay what the
// last snapshot said (none here).
func TestHerdrListFallsBackToAgentListWithoutSnapshot(t *testing.T) {
	n, f, _ := herdrNode(t)
	f.reply("session.snapshot", herdrFail{Code: "internal", Message: "no snapshot"})
	f.reply("agent.list", map[string]any{"agents": []any{claudeAgent(sidA, proto.StatusDone, "w2:p1", "w2:t1", "w2", "/tmp/repo", "t")}})
	list := n.sessionList(context.Background(), true)
	if len(list) != 1 || list[0].SessionID != sidA || list[0].SpaceKind != "herdr" || list[0].SpaceID != "w2" ||
		list[0].SpaceName != "" || list[0].SlotID != "w2:t1" || list[0].Status != proto.StatusDone {
		t.Fatalf("listed %+v", list)
	}
	assertTrace(t, f.trace(), []string{call("session.snapshot", nil), call("agent.list", nil)})

	// Both fail: refreshHerdr reports the snapshot's error and keeps the agents it had.
	f.reply("agent.list", herdrFail{Code: "internal", Message: "down"})
	if err := n.refreshHerdr(context.Background()); err == nil || !strings.Contains(err.Error(), "no snapshot") {
		t.Fatalf("refresh: %v", err)
	}
	if _, ok := n.agent(sidA); !ok {
		t.Fatal("a failed refresh dropped the agents")
	}
}

// A claude herdr cannot name (no integration): its process (sessions/<pid>.json) first, then the pane's binding,
// then a guess — a transcript in its directory whose title is the terminal title, else the newest one nobody has.
func TestHerdrUnnamedClaudeIsGuessed(t *testing.T) {
	n, f, home := herdrNode(t)
	cwd := "/tmp/proj"
	old := time.Now().Add(-time.Hour)
	writeTranscript(t, home, cwd, sidA, old, `{"type":"custom-title","customTitle":"fix the flaky test","sessionId":"`+sidA+`"}`)
	writeTranscript(t, home, cwd, sidB, time.Now().Add(-time.Minute), `{"type":"custom-title","customTitle":"other","sessionId":"`+sidB+`"}`)
	writeTranscript(t, home, cwd, sidC, old.Add(-time.Hour), `{"type":"custom-title","customTitle":"bound","sessionId":"`+sidC+`"}`)
	n.panes["w1:p3"] = sidC // bound by an earlier start / hook

	f.reply("pane.process_info", map[string]any{"process_info": map[string]any{"foreground_processes": []any{
		map[string]any{"pid": 1 << 30, "name": "claude", "argv0": "claude"}}}}) // no sessions/<pid>.json for it
	f.reply("session.snapshot", snapshot(nil, nil, []any{
		claudeAgent("", proto.StatusIdle, "w1:p1", "w1:t1", "w1", cwd, "fix the flaky test"), // title match: A
		claudeAgent("", proto.StatusIdle, "w1:p2", "w1:t2", "w1", cwd, "something else"),    // newest unclaimed: B
		claudeAgent("", proto.StatusIdle, "w1:p3", "w1:t3", "w1", cwd, "fix the flaky test"), // its binding: C
		claudeAgent("", proto.StatusIdle, "w1:p4", "w1:t4", "w1", "", ""),                   // no cwd: nothing
	}, nil))
	if err := n.refreshHerdr(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{sidA: "w1:p1", sidB: "w1:p2", sidC: "w1:p3"}
	if len(n.agents) != len(want) {
		t.Fatalf("agents %v", n.agents)
	}
	for id, pane := range want {
		if n.agents[id].PaneID != pane {
			t.Errorf("%s in %q, want %s", id, n.agents[id].PaneID, pane)
		}
	}
	assertTrace(t, f.trace(), []string{
		call("session.snapshot", nil),
		call("pane.process_info", map[string]any{"pane_id": "w1:p1"}),
		call("pane.process_info", map[string]any{"pane_id": "w1:p2"}),
		call("pane.process_info", map[string]any{"pane_id": "w1:p3"}),
		call("pane.process_info", map[string]any{"pane_id": "w1:p4"}),
	})

	// The pid not found is remembered for panePIDRetry: the next snapshot asks herdr for no process.
	f.reset()
	if err := n.refreshHerdr(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertTrace(t, f.trace(), []string{call("session.snapshot", nil)})
}

// TRAEX panes (herdr sees no agent there) are found by the thread lock files and the pane in their process's
// environment; their status comes from the title's spinner without reading the screen. A pane herdr already reports
// as an agent is not taken for a TRAEX one.
func TestHerdrTraexPanesAreMergedIntoTheList(t *testing.T) {
	n, f, home := herdrNode(t)
	const thread, other = "01a181ac-9df1-7171-824c-cf63a7531555", "01a181ce-c31c-75b1-a332-5f26bd26bb6a"
	locks := filepath.Join(home, ".trae", "cli", "thread-writer-locks")
	if err := os.MkdirAll(locks, 0o755); err != nil {
		t.Fatal(err)
	}
	for th, pid := range map[string]int{thread: os.Getpid(), other: os.Getppid()} {
		b, _ := json.Marshal(map[string]any{"pid": pid})
		if err := os.WriteFile(filepath.Join(locks, th+".owner.json"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	traexCache.Lock()
	traexCache.panes[os.Getpid()] = "w3:p2"  // what HERDR_PANE_ID in its environment says
	traexCache.panes[os.Getppid()] = "w3:p1" // a pane herdr reports as a claude agent
	traexCache.Unlock()

	f.reply("session.snapshot", snapshot(
		[]any{map[string]any{"workspace_id": "w3", "label": "tools", "number": 3}},
		[]any{map[string]any{"tab_id": "w3:t2", "workspace_id": "w3", "label": "traex", "number": 2}},
		[]any{claudeAgent(sidA, proto.StatusIdle, "w3:p1", "w3:t1", "w3", "/tmp/tools", "claude here")},
		[]any{
			map[string]any{"pane_id": "w3:p1", "tab_id": "w3:t1", "workspace_id": "w3", "cwd": "/tmp/tools"},
			map[string]any{"pane_id": "w3:p2", "tab_id": "w3:t2", "workspace_id": "w3", "cwd": "/tmp/tools/sub",
				"terminal_title": "⠸ build the thing", "terminal_title_stripped": "build the thing"},
		}))
	list := n.sessionList(context.Background(), true)
	m := byID(list)
	if len(list) != 2 {
		t.Fatalf("listed %v", ids(list))
	}
	tr, ok := m[thread]
	if !ok {
		t.Fatalf("no TRAEX session: %v", ids(list))
	}
	// No rollout: the title falls back to the directory's name (decorate).
	if tr.Agent != traexAgent || tr.Status != proto.StatusWorking || tr.PaneID != "w3:p2" || tr.SpaceName != "tools" ||
		tr.SlotName != "traex" || tr.SlotIndex != 2 || tr.Title != "sub" || tr.Cwd != "/tmp/tools/sub" {
		t.Errorf("TRAEX session %+v", tr)
	}
	if _, ok := m[other]; ok {
		t.Error("a claude pane was taken for TRAEX")
	}
	assertTrace(t, f.trace(), []string{call("session.snapshot", nil)})
}

// Without a spinner a TRAEX pane's status is read off its screen (at most every 2 s per pane): "esc to interrupt"
// is working, a numbered choice blocked, anything else a finished turn; a failed read keeps the last status.
func TestHerdrTraexStatusFromScreen(t *testing.T) {
	n, f, _ := herdrNode(t)
	f.setScreen("w4:p1", "• Running tests (12s • esc to interrupt)\n")
	f.setScreen("w4:p2", "Would you like to run the following command?\n\n› 1. Yes, proceed (y)\n  2. No (esc)\n")
	f.setScreen("w4:p3", "◆ Done.\n\n❯ Run /review on my current changes\n")
	f.handle("pane.read", func(p map[string]any) any {
		if p["pane_id"] == "w4:p4" {
			return herdrFail{Code: "pane_not_found", Message: "gone"}
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		return map[string]any{"read": map[string]any{"text": f.screen[str(p["pane_id"])]}}
	})
	ctx := context.Background()
	for pane, want := range map[string]string{"w4:p1": proto.StatusWorking, "w4:p2": proto.StatusBlocked,
		"w4:p3": proto.StatusDone, "w4:p4": proto.StatusUnknown} {
		if got := n.traexStatus(ctx, herdr.Agent{PaneID: pane, RawTitle: "traex | /tmp/x"}); got != want {
			t.Errorf("%s: %s, want %s", pane, got, want)
		}
	}
	var reads []string
	for _, c := range f.calls() {
		reads = append(reads, c.String())
	}
	sort.Strings(reads)
	assertTrace(t, reads, []string{
		call("pane.read", map[string]any{"pane_id": "w4:p1", "source": "visible", "lines": 40}),
		call("pane.read", map[string]any{"pane_id": "w4:p2", "source": "visible", "lines": 40}),
		call("pane.read", map[string]any{"pane_id": "w4:p3", "source": "visible", "lines": 40}),
		call("pane.read", map[string]any{"pane_id": "w4:p4", "source": "visible", "lines": 40}),
	})
	// Within 2 s the cached status is used; a spinner says working without any read.
	f.reset()
	if got := n.traexStatus(ctx, herdr.Agent{PaneID: "w4:p2"}); got != proto.StatusBlocked {
		t.Errorf("cached: %s", got)
	}
	if got := n.traexStatus(ctx, herdr.Agent{PaneID: "w4:p3", RawTitle: "⠋ thinking"}); got != proto.StatusWorking {
		t.Errorf("spinner: %s", got)
	}
	assertTrace(t, f.trace(), nil)
	// A read failing after a known status keeps it.
	traexCache.Lock()
	traexCache.status["w4:p4"] = traexState{proto.StatusBlocked, time.Now().Add(-time.Minute)}
	traexCache.Unlock()
	if got := n.traexStatus(ctx, herdr.Agent{PaneID: "w4:p4"}); got != proto.StatusBlocked {
		t.Errorf("failed read: %s", got)
	}
}

// pollAgents: one snapshot per tick (no event stream), a sessions.changed with the whole list when something it
// shows changed, and only sessions.revs when just a transcript grew.
func TestHerdrPollPushesChangedThenRevs(t *testing.T) {
	n, f, home := herdrNode(t)
	cwd := "/tmp/poll"
	p := writeTranscript(t, home, cwd, sidA, time.Now(),
		`{"type":"user","timestamp":"2026-01-01T00:00:00Z","cwd":"/tmp/poll","message":{"role":"user","content":"hello"}}`)
	f.reply("session.snapshot", snapshot(nil, nil,
		[]any{claudeAgent(sidA, proto.StatusIdle, "w5:p1", "w5:t1", "w5", cwd, "poll")}, nil))

	pr, pw := io.Pipe()
	n.peer, n.events = rpc.New("n", pw), newHerdrEvents() // no event stream: snapshotDue every tick
	type note struct {
		Event  string          `json:"event"`
		Params json.RawMessage `json:"params"`
	}
	notes := make(chan note, 16)
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			var m note
			if json.Unmarshal(sc.Bytes(), &m) == nil {
				notes <- m
			}
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	t.Cleanup(func() { pw.Close() })
	next := func() note {
		t.Helper()
		select {
		case m := <-notes:
			return m
		case <-time.After(3 * time.Second):
			t.Fatal("no notification")
		}
		return note{}
	}
	go n.pollAgents(ctx)

	first := next()
	var changed struct {
		Sessions []proto.Session `json:"sessions"`
	}
	if first.Event != "sessions.changed" || json.Unmarshal(first.Params, &changed) != nil || len(changed.Sessions) != 1 ||
		changed.Sessions[0].SessionID != sidA {
		t.Fatalf("first push %s %s", first.Event, first.Params)
	}
	// The transcript grows by a line the list does not show: only its rev goes out.
	fh, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fh.WriteString(`{"type":"file-history-snapshot","messageId":"m1"}` + "\n")
	fh.Close()
	st, _ := os.Stat(p)
	second := next()
	var revs struct {
		Revs map[string]int64 `json:"revs"`
	}
	if second.Event != "sessions.revs" || json.Unmarshal(second.Params, &revs) != nil || revs.Revs[sidA] != st.Size() {
		t.Fatalf("second push %s %s (size %d)", second.Event, second.Params, st.Size())
	}
	cancel()
	for _, c := range f.calls() {
		if c.Method != "session.snapshot" {
			t.Errorf("poll called %s", c)
		}
	}
}
