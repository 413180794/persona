package hub

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// Screens with a dialog the hub can read (screen.ParseDialog).
const (
	pickModel  = "  Pick a model\n\n› 1. gpt-6\n  2. gpt-6-mini\n\n  Press enter to confirm or esc to go back\n"
	pickEffort = "  Pick a reasoning level\n\n› 1. low\n  2. high\n\n  Press enter to confirm or esc to go back\n"
)

// waitCards waits up to d for the hub to hold n interactions and says how long it took.
func waitCards(h *Hub, n int, d time.Duration) (map[string]string, time.Duration) {
	start := time.Now()
	for len(pendingIDs(h)) < n && time.Since(start) < d {
		time.Sleep(10 * time.Millisecond)
	}
	return pendingIDs(h), time.Since(start)
}

func blockSession(h *Hub, m *Machine, s proto.Session) {
	s.Status = proto.StatusBlocked
	m.mu.Lock()
	m.sessions = []proto.Session{s}
	m.mu.Unlock()
	h.syncScreenInteractions(m, []proto.Session{s})
}

// A Claude Code without Jarvis's hooks (the user's own profile) asks only on the screen: its card is made at once, not
// after the head start a hook's card gets (#751). One with hooks (a hook event came, or Jarvis started it) waits.
func TestScreenCardWaitsOnlyForHooks(t *testing.T) {
	for _, c := range []struct {
		name  string
		s     proto.Session
		event bool
		min   time.Duration
		max   time.Duration
	}{
		{"no hooks", proto.Session{SessionID: "s1", Agent: "claude"}, false, 0, 300 * time.Millisecond},
		{"a hook event", proto.Session{SessionID: "s1", Agent: "claude"}, true, 1400 * time.Millisecond, 3 * time.Second},
		{"started by Jarvis", proto.Session{SessionID: "s1", Agent: "claude", StartedByJarvis: true}, false, 1400 * time.Millisecond, 3 * time.Second},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newTestHub(t)
			m, f := newCodexMachine(t, h)
			f.screen = pickModel
			if c.event {
				b, _ := json.Marshal(map[string]any{"session_id": "s1", "event": "UserPromptSubmit"})
				m.onEvent("hook.event", b)
			}
			blockSession(h, m, c.s)
			got, took := waitCards(h, 1, 5*time.Second)
			if len(got) != 1 || took < c.min || took > c.max {
				t.Fatalf("%v after %v, want one card in [%v, %v]", got, took, c.min, c.max)
			}
		})
	}
}

func TestScreenCardWait(t *testing.T) {
	claude, codex := proto.Session{Agent: "claude"}, proto.Session{Agent: "codex"}
	if screenCardWait(claude, false) != 0 || screenCardWait(claude, true) != 1500*time.Millisecond ||
		screenCardWait(codex, false) != 3*time.Second {
		t.Fatal("waits")
	}
}

// After a screen card's answer the agent asks again while herdr keeps it blocked (the next question, the next
// permission): no status change comes, so the hub looks at the screen again and the new dialog gets its card. The
// answered dialog still on screen (the keys not landed yet) gets none.
func TestScreenCardAfterAnAnswerWhileStillBlocked(t *testing.T) {
	old, oldFor := screenRecheck, screenRecheckFor
	screenRecheck, screenRecheckFor = 50*time.Millisecond, 400*time.Millisecond
	t.Cleanup(func() { screenRecheck, screenRecheckFor = old, oldFor })
	h := newTestHub(t)
	m, f := newCodexMachine(t, h)
	f.screen = pickModel
	blockSession(h, m, proto.Session{SessionID: "s1", Agent: "claude"})
	got, _ := waitCards(h, 1, 2*time.Second)
	var first string
	for id := range got {
		first = id
	}
	if first == "" {
		t.Fatal("no card")
	}
	// The keys have not landed: the same dialog is still there until the rechecks give up.
	if err := h.resolve(first, proto.Resolution{Option: "1"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(600 * time.Millisecond)
	if got := pendingIDs(h); len(got) != 0 {
		t.Fatalf("a card again for the answered dialog: %v", got)
	}
	// The next one is up, still blocked.
	f.mu.Lock()
	f.screen = pickEffort
	f.mu.Unlock()
	blockSession(h, m, proto.Session{SessionID: "s1", Agent: "claude"}) // as a list rebuild would
	got, took := waitCards(h, 1, 2*time.Second)
	second := ""
	for id := range got {
		second = id
	}
	if second == "" || second == first || took > time.Second {
		t.Fatalf("next dialog: %v after %v", got, took)
	}
	// The answer lands a moment after the keys: the answered dialog, then the screen between, then the next one.
	if err := h.resolve(second, proto.Resolution{Option: "2"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	f.mu.Lock()
	f.screen = "  ⏺ Thinking…\n"
	f.mu.Unlock()
	time.Sleep(80 * time.Millisecond)
	f.mu.Lock()
	f.screen = pickModel
	f.mu.Unlock()
	// Nothing changes on the list this time: only the recheck can find it.
	got, took = waitCards(h, 1, 2*time.Second)
	if len(got) != 1 || got[first] == "" || took > 300*time.Millisecond {
		t.Fatalf("after the second answer, with no status change: %v after %v", got, took)
	}
}

// Blocked a moment before the dialog is drawn (Orca calls a claude blocked from its PreToolUse hook; on Windows the
// hooks take seconds): the card made then has no dialog. Once the dialog shows, its card replaces that one; a card
// answered or gone, or an agent no longer blocked, stops the looking.
func TestScreenCardUpgradesWhenTheDialogShows(t *testing.T) {
	old, oldFor := screenUpgrade, screenUpgradeFor
	screenUpgrade, screenUpgradeFor = 50*time.Millisecond, time.Second
	t.Cleanup(func() { screenUpgrade, screenUpgradeFor = old, oldFor })
	h := newTestHub(t)
	m, f := newCodexMachine(t, h)
	f.screen = "  ✢ Percolating… (running PreToolUse hooks… 1/2 · 3s)\n"
	blockSession(h, m, proto.Session{SessionID: "s1", Agent: "claude"})
	got, _ := waitCards(h, 1, 2*time.Second)
	if len(got) != 1 || got["scr_"+m.cfg.ID+"_s1"] == "" {
		t.Fatalf("the card before the dialog: %v", got)
	}
	f.mu.Lock()
	f.screen = pickModel
	f.mu.Unlock()
	start := time.Now()
	for time.Since(start) < 2*time.Second {
		if ids := pendingIDs(h); len(ids) == 1 && ids["scr_"+m.cfg.ID+"_s1"] == "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	ids := pendingIDs(h)
	h.mu.Lock()
	var dialog bool
	for _, p := range h.interactions {
		dialog = p.ix.Dialog != nil && len(p.ix.Dialog.Options) == 2
	}
	h.mu.Unlock()
	if len(ids) != 1 || ids["scr_"+m.cfg.ID+"_s1"] != "" || !dialog {
		t.Fatalf("after the dialog showed: %v (dialog %v)", ids, dialog)
	}
}

// The dialog answered on the computer: the agent moves on and its screen card goes. The history says so too
// (cancelled), not pending until the next hub restart (2026-11-04: a card answered at the computer ten seconds after
// it was made stayed pending in the history).
func TestScreenCardGoneWhenTheAgentMovesOnIsRecorded(t *testing.T) {
	h := newTestHub(t)
	st := newMeshStore()
	h.store = st
	m, f := newCodexMachine(t, h)
	f.screen = pickModel
	blockSession(h, m, proto.Session{SessionID: "s1", Agent: "claude"})
	got, _ := waitCards(h, 1, 2*time.Second)
	var id string
	for k := range got {
		id = k
	}
	if id == "" {
		t.Fatal("no card")
	}
	s := proto.Session{SessionID: "s1", Agent: "claude", Status: proto.StatusWorking}
	m.mu.Lock()
	m.sessions = []proto.Session{s}
	m.mu.Unlock()
	h.syncScreenInteractions(m, []proto.Session{s})
	if len(pendingIDs(h)) != 0 {
		t.Fatalf("card still pending: %v", pendingIDs(h))
	}
	if status, _, _ := st.InteractionOutcome(t.Context(), id); status != "cancelled" {
		t.Fatalf("history says %q, want cancelled", status)
	}
}

// #2872: Claude's answer above a notice is often a numbered list. Its numbers are the conversation's words, not the
// notice's options: the card for that screen must carry no dialog (it would offer the prose as buttons, and answering
// one types a digit into whatever holds the terminal's keyboard), and it stays a card upgradeScreenCard can improve.
func TestScreenCardIgnoresANumberedAnswer(t *testing.T) {
	const noticeUnderNumberedAnswer = `❯ fix the flaky test

⏺ I'll do three things:
  1. Fix the race in the watcher
  2. Add a regression test
  3. Push the branch

✻ Cooked for 12s
────────────────────────────────────
 We're changing auto mode to no longer charge for classifier requests in Claude Code.

 Enter to continue · Esc to cancel
`
	h := newTestHub(t)
	m, f := newCodexMachine(t, h)
	f.screen = noticeUnderNumberedAnswer
	blockSession(h, m, proto.Session{SessionID: "s1", Agent: "claude"})
	got, _ := waitCards(h, 1, 2*time.Second)
	if _, ok := got["scr_"+m.cfg.ID+"_s1"]; !ok {
		t.Fatalf("no card for the notice: %v", got)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, p := range h.interactions {
		if p.ix.SessionID != "s1" {
			continue
		}
		if p.ix.Dialog != nil {
			t.Fatalf("the answer's numbered list became the card's options: %q %+v", p.ix.Dialog.Question, p.ix.Dialog.Options)
		}
	}
}
