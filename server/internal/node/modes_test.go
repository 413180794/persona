package node

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestTuiMode(t *testing.T) {
	const box = "────\n❯ \n────\n"
	cases := map[string]string{
		box + "  ⏸ manual mode on · ? for shortcuts · ← for agents": "default",
		box + "  ? for shortcuts":                               "default",
		box + "  ⏵⏵ accept edits on (shift+tab to cycle)":       "acceptEdits",
		box + "  ⏸ plan mode on (shift+tab to cycle)\n\n":       "plan",
		box + "  ⏵⏵ auto mode on (shift+tab to cycle)":          "auto",
		box + "  ⏵⏵ bypass permissions on (shift+tab to cycle)": "bypassPermissions",
		// Chat text mentioning a mode, far above the footer, does not count.
		"I turned plan mode on for you\n" + box + "\n\n\n\n\n": "",
	}
	for screen, want := range cases {
		if got := tuiMode(screen); got != want {
			t.Errorf("tuiMode(%q) = %q, want %q", screen, got, want)
		}
	}
}

func TestTuiPromptEmpty(t *testing.T) {
	for screen, want := range map[string]bool{
		"────\n❯ \n────\n  ? for shortcuts":                                         true,
		"────\n❯ Try \"create a util logging.py that...\"\n────\n  ? for shortcuts": true,
		"────\n❯ half-typed draft\n────\n  ? for shortcuts":                         false,
		"no prompt here": false,
	} {
		if got := tuiPromptEmpty(screen); got != want {
			t.Errorf("tuiPromptEmpty(%q) = %v", screen, got)
		}
	}
}

func TestValidModesAndFlags(t *testing.T) {
	if err := validModes("plan", "xhigh"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][2]string{{"yolo", ""}, {"", "ultra"}, {"plan; rm -rf ~", ""}} {
		if validModes(bad[0], bad[1]) == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if got := modeFlags("acceptEdits", "low"); got != " --permission-mode acceptEdits --effort low" {
		t.Errorf("flags %q", got)
	}
	if modeFlags("", "") != "" {
		t.Error("empty flags")
	}
}

func TestCurrentModesPrefersNewer(t *testing.T) {
	n := &Node{modes: map[string]sessionModes{}}
	old := time.Now().Add(-time.Minute)
	m := quickMeta{mode: "default", modeAt: old, effort: "high", effortAt: old}
	if mode, effort := n.currentModes("s", m); mode != "default" || effort != "high" {
		t.Fatalf("transcript only: %s %s", mode, effort)
	}
	n.noteModes("s", "plan", "") // set from the app after the transcript's last word
	if mode, effort := n.currentModes("s", m); mode != "plan" || effort != "high" {
		t.Fatalf("app wins: %s %s", mode, effort)
	}
	m.mode, m.modeAt = "auto", time.Now().Add(time.Second) // then the terminal changed it again
	if mode, _ := n.currentModes("s", m); mode != "auto" {
		t.Fatalf("newer transcript wins: %s", mode)
	}
}

// Claude Code 2.1.206's /effort is a slider (←/→, Enter), not the list with "for this session only".
func TestEffortPickerAndOutcome(t *testing.T) {
	b, err := os.ReadFile("testdata/claude-2.1.206-effort-slider-46.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got := effortPicker(string(b)); got != "slider" {
		t.Fatalf("slider: %q", got)
	}
	if got := effortPicker("Choose effort\n  ● high  for this session only (s)"); got != "list" {
		t.Fatalf("list: %q", got)
	}
	if got := effortPicker("❯ \n  ⏸ manual mode on"); got != "" {
		t.Fatalf("none: %q", got)
	}
	// A launcher's CLAUDE_CODE_EFFORT_LEVEL wins over the session's: told, not claimed as done.
	env := "❯ /effort\n  ⎿  CLAUDE_CODE_EFFORT_LEVEL=max overrides\n     this session — clear it and xhigh takes\n     over"
	if err := effortOutcome(env, "xhigh"); err == nil || !strings.Contains(err.Error(), "CLAUDE_CODE_EFFORT_LEVEL=max") {
		t.Fatalf("env override: %v", err)
	}
	if err := effortOutcome("  ⎿  Set effort level to high (this session)", "high"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := effortOutcome("❯ \n", "low"); err == nil {
		t.Fatal("nothing said: should not count as changed")
	}
}

// lineSink records what the node writes to an assistant process's stdin.
type lineSink struct {
	mu     sync.Mutex
	b      strings.Builder
	closed bool
}

func (s *lineSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *lineSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *lineSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// fakeAssistant puts a running Claude process (no real one) in the node's table.
func fakeAssistant(n *Node, id, startMode string) (*assistantProc, *lineSink) {
	sink := &lineSink{}
	p := &assistantProc{id: id, agent: "claude", launcher: "claude", cmd: &exec.Cmd{}, stdin: sink, exited: make(chan struct{}),
		ready: make(chan struct{}), mode: startMode, canBypass: startMode == proto.ModeBypass}
	n.mu.Lock()
	n.procs[id] = p
	n.mu.Unlock()
	return p, sink
}

// The mode the app set on an assistant conversation wins over its transcript, however new the transcript's word,
// and survives the node (persisted with the session state): an old "default" there locked the main one in (#2416).
func TestAssistantAppModeWinsOverTranscript(t *testing.T) {
	n := assistantNode(t)
	n.modes = map[string]sessionModes{}
	const id = "11111111-2222-3333-4444-555555555555"
	var saved []sessionState
	n.state.persist = func(_ string, v sessionState) { saved = append(saved, v) }
	n.state.put(id, sessionState{Launcher: "claude", Cwd: "/home/u/.persona/assistant"})

	if err := n.setModes(context.Background(), id, "acceptEdits", "high"); err != nil {
		t.Fatal(err)
	}
	if len(saved) != 2 || saved[1].AppMode != "acceptEdits" || saved[1].AppEffort != "high" || saved[1].Launcher != "claude" {
		t.Fatalf("not persisted with the session state: %+v", saved)
	}
	n.modes = map[string]sessionModes{} // the node restarted: only the hub's state is left
	m := quickMeta{mode: "default", modeAt: time.Now().Add(time.Hour), effort: "low", effortAt: time.Now().Add(time.Hour)}
	if mode, effort := n.currentModes(id, m); mode != "acceptEdits" || effort != "high" {
		t.Fatalf("transcript won over the app: %s %s", mode, effort)
	}
	// Setting the same again writes nothing.
	n.persistModes(id, "acceptEdits", "")
	if len(saved) != 2 {
		t.Fatalf("rewrote an unchanged mode: %d", len(saved))
	}
}

// bypassPermissions can't be set with a control request in a process started without it: an idle one ends, so the
// next message resumes in that mode; a busy one is left alone and no control request claims the switch.
func TestSetModesBypassOnAssistant(t *testing.T) {
	n := assistantNode(t)
	n.modes = map[string]sessionModes{}
	const id = "11111111-2222-3333-4444-555555555555"
	n.state.put(id, sessionState{Launcher: "claude", Cwd: "/home/u/.persona/assistant"})

	// Other modes: a control request to the running process.
	p, sink := fakeAssistant(n, id, "")
	if err := n.setModes(context.Background(), id, "acceptEdits", ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sink.String(), `"mode":"acceptEdits"`) || !strings.Contains(sink.String(), "set_permission_mode") {
		t.Fatalf("no control request: %q", sink.String())
	}

	// Busy: kept, and nothing sent for bypassPermissions.
	p.mu.Lock()
	p.busy = true
	p.mu.Unlock()
	before := sink.String()
	if err := n.setModes(context.Background(), id, proto.ModeBypass, ""); err != nil {
		t.Fatal(err)
	}
	n.mu.Lock()
	kept := n.procs[id] == p
	n.mu.Unlock()
	if !kept || sink.String() != before {
		t.Fatalf("busy process: kept %v, sent %q", kept, strings.TrimPrefix(sink.String(), before))
	}
	if !p.needsRestartFor(proto.ModeBypass) {
		t.Fatal("the switch should wait for a new process")
	}
	if mode, _ := n.modesOf(id); mode != proto.ModeBypass {
		t.Fatalf("mode shown: %s", mode)
	}

	// Idle: the process ends; the next message starts one with --permission-mode bypassPermissions (spawnClaude reads
	// modesOf).
	p.mu.Lock()
	p.busy = false
	p.mu.Unlock()
	if err := n.setModes(context.Background(), id, proto.ModeBypass, ""); err != nil {
		t.Fatal(err)
	}
	n.mu.Lock()
	_, running := n.procs[id]
	n.mu.Unlock()
	if running || !sink.closed {
		t.Fatalf("idle process not stopped: running %v, stdin closed %v", running, sink.closed)
	}
	if st, _ := n.state.get(id); st.AppMode != proto.ModeBypass {
		t.Fatalf("not persisted: %+v", st)
	}

	// A process started in bypassPermissions switches away and back with control requests.
	q, sink2 := fakeAssistant(n, id, proto.ModeBypass)
	if err := n.setModes(context.Background(), id, "default", ""); err != nil {
		t.Fatal(err)
	}
	if err := n.setModes(context.Background(), id, proto.ModeBypass, ""); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(sink2.String(), "set_permission_mode"); got != 2 || q.needsRestartFor(proto.ModeBypass) {
		t.Fatalf("started in bypass: %d control requests %q", got, sink2.String())
	}
}
