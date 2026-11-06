package node

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/transcript"
)

// #5396: a rewind done in the terminal (/rewind or Esc Esc, "Restore conversation") writes nothing to the transcript
// and leaves the prompt in the input box, which holds off Claude's recap: the session list kept the rewound turn (its
// API error, its last words) until the user sent something, 18 minutes in a live test. The node now sees it on the
// screen of a resting Claude (dialogstatus.go's read) and leaves the turn out at once.

const terminalRewindID = "0d000000-0000-4000-8000-000000005396"

func terminalRewindLines() []string {
	return []string{
		`{"type":"user","uuid":"u1","parentUuid":null,"timestamp":"2026-11-05T12:00:00Z","message":{"role":"user","content":"一"}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","timestamp":"2026-11-05T12:00:05Z","message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"答一"}]}}`,
		`{"type":"system","subtype":"turn_duration","uuid":"t1","parentUuid":"a1","timestamp":"2026-11-05T12:00:06Z","durationMs":6000}`,
		`{"type":"user","uuid":"u2","parentUuid":"t1","timestamp":"2026-11-05T12:10:00Z","message":{"role":"user","content":"按这个做"}}`,
		`{"type":"assistant","uuid":"e2","parentUuid":"u2","timestamp":"2026-11-05T12:10:09Z","isApiErrorMessage":true,"message":{"id":"m3","role":"assistant","model":"<synthetic>","content":[{"type":"text","text":"API Error: the reply ended during reasoning"}]}}`,
		`{"type":"system","subtype":"turn_duration","uuid":"t2","parentUuid":"e2","timestamp":"2026-11-05T12:10:10Z","durationMs":10000}`,
	}
}

// terminalRewindNode is a node with one resting Claude in a herdr pane (sim) and its transcript.
func terminalRewindNode(t *testing.T, sim *claudeSim, lines []string) (*Node, string, func() proto.Session) {
	t.Helper()
	old := paneDialogEvery
	paneDialogEvery = 0
	t.Cleanup(func() { paneDialogEvery = old })
	p := filepath.Join(t.TempDir(), terminalRewindID+".jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	n := rewindNode(t, sim, "claude", proto.StatusDone)
	n.agents = map[string]host.Agent{terminalRewindID: {Agent: "claude", PaneID: "w1:p1", Cwd: "/work/repo", AgentStatus: proto.StatusDone}}
	n.paths, n.metas, n.bgScans = map[string]string{terminalRewindID: p}, map[string]quickMeta{}, map[string]*bgScan{}
	n.files, n.watches = map[string]*transcript.File{}, map[string]*watch{}
	row := func() proto.Session {
		t.Helper()
		for _, s := range n.sessionList(context.Background(), false) {
			if s.SessionID == terminalRewindID {
				return s
			}
		}
		t.Fatal("no row")
		return proto.Session{}
	}
	return n, p, row
}

// userRewinds is the user at the computer: /rewind, Up to the last prompt, Enter, "Restore conversation".
func userRewinds(sim *claudeSim) {
	sim.mu.Lock()
	defer sim.mu.Unlock()
	sim.box = "/rewind"
	for _, k := range []string{"enter", "up", "enter", "enter"} {
		sim.key(k)
	}
	sim.keys = nil
}

func TestTerminalRewindLeavesTheTurnOutOfTheList(t *testing.T) {
	sim := newClaudeSim(simPrompt{"一", noCode}, simPrompt{"按这个做", noCode})
	n, _, row := terminalRewindNode(t, sim, terminalRewindLines())
	if s := row(); !strings.HasPrefix(s.LastText, "API Error:") {
		t.Fatalf("before: %q", s.LastText)
	}
	userRewinds(sim)
	var s proto.Session
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if s = row(); s.LastText == "答一" {
			break
		}
	}
	if s.LastText != "答一" || s.TurnError != "" {
		t.Fatalf("after the rewind: %q (turn error %q)", s.LastText, s.TurnError)
	}
	f := n.files[terminalRewindID]
	items, _, span := f.Page(0, 100)
	if got := itemTexts(items); got != "一|答一" || !strings.HasSuffix(span.History, ".r1") {
		t.Fatalf("transcript: %q, history %q", got, span.History)
	}
	if got := sim.pressed(); got != "" {
		t.Fatalf("the node pressed %q in the user's terminal", got)
	}
	// Read again and again: nothing more happens.
	for i := 0; i < 5; i++ {
		row()
		time.Sleep(10 * time.Millisecond)
	}
	if _, _, again := f.Page(0, 100); again.History != span.History {
		t.Fatalf("history moved again: %q → %q", span.History, again.History)
	}
}

// The same words typed again as a new message (the prompt's row is still above the box) is no rewind; neither is a
// screen whose transcript went on since (a turn of the prompt still running, or another line written after it).
func TestTerminalRewindNotSeenOnLookalikes(t *testing.T) {
	t.Run("typed again", func(t *testing.T) {
		sim := newClaudeSim(simPrompt{"一", noCode}, simPrompt{"按这个做", noCode})
		sim.box = "按这个做"
		_, _, row := terminalRewindNode(t, sim, terminalRewindLines())
		for i := 0; i < 20; i++ {
			if s := row(); !strings.HasPrefix(s.LastText, "API Error:") {
				t.Fatalf("taken for a rewind: %q", s.LastText)
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
	t.Run("unanswered", func(t *testing.T) {
		sim := newClaudeSim(simPrompt{"一", noCode}, simPrompt{"按这个做", noCode})
		lines := terminalRewindLines()[:4] // the prompt has no answer (Esc before the reply, #3036)
		n, _, row := terminalRewindNode(t, sim, lines)
		userRewinds(sim)
		for i := 0; i < 20; i++ {
			row()
			time.Sleep(10 * time.Millisecond)
		}
		n.mu.Lock()
		f := n.files[terminalRewindID]
		n.mu.Unlock()
		if f != nil {
			if _, _, span := f.Page(0, 100); strings.Contains(span.History, ".r") {
				t.Fatalf("taken for a rewind: %q", span.History)
			}
		}
	})
}

// A wrong guess is undone: Claude goes on from the turn taken for rewound.
func TestTerminalRewindUndoneWhenClaudeGoesOn(t *testing.T) {
	sim := newClaudeSim(simPrompt{"一", noCode}, simPrompt{"按这个做", noCode})
	lines := terminalRewindLines()
	n, p, row := terminalRewindNode(t, sim, lines)
	userRewinds(sim)
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end) && row().LastText != "答一"; time.Sleep(20 * time.Millisecond) {
	}
	if s := row(); s.LastText != "答一" {
		t.Fatalf("not seen: %q", s.LastText)
	}
	more := append(lines,
		`{"type":"user","uuid":"u3","parentUuid":"t2","timestamp":"2026-11-05T12:20:00Z","message":{"role":"user","content":"再试一次"}}`,
		`{"type":"assistant","uuid":"a3","parentUuid":"u3","timestamp":"2026-11-05T12:20:05Z","message":{"id":"m4","role":"assistant","content":[{"type":"text","text":"好了"}]}}`)
	if err := os.WriteFile(p, []byte(strings.Join(more, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	n.mu.Lock()
	m := n.metas[p]
	m.readAt = time.Time{} // not the few seconds a growing file's row may lag (metaMinInterval)
	n.metas[p] = m
	n.mu.Unlock()
	var s proto.Session
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if s = row(); s.LastText == "好了" {
			break
		}
	}
	if s.LastText != "好了" {
		t.Fatalf("after Claude went on: %q", s.LastText)
	}
	n.refresh(terminalRewindID, n.files[terminalRewindID])
	if items, _, span := n.files[terminalRewindID].Page(0, 100); itemTexts(items) != "一|答一|按这个做|API Error: the reply ended during reasoning|再试一次|好了" || strings.Contains(span.History, ".r") {
		t.Fatalf("transcript: %q, history %q", itemTexts(items), span.History)
	}
}

// A resting pane whose box holds something that is not the last prompt (Claude's suggestion, an old draft) is read
// with colors once for a screen, not on every dialog read.
func TestTerminalRewindReadsAStillScreenOnce(t *testing.T) {
	sim := newClaudeSim(simPrompt{"一", noCode}, simPrompt{"按这个做", noCode})
	sim.box = "别的事"
	_, _, row := terminalRewindNode(t, sim, terminalRewindLines())
	for i := 0; i < 30; i++ {
		row()
		time.Sleep(10 * time.Millisecond)
	}
	sim.mu.Lock()
	reads := sim.ansiReads
	sim.mu.Unlock()
	if reads != 1 {
		t.Fatalf("%d reads with colors of the same screen", reads)
	}
}

func itemTexts(items []proto.Item) string {
	var out []string
	for _, it := range items {
		if it.Kind == proto.KindText {
			out = append(out, it.Text)
		}
	}
	return strings.Join(out, "|")
}
