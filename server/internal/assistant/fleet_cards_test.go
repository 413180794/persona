package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
	"github.com/nwverse/persona/server/internal/screen"
)

// The cards the manager makes itself (fleet_ix.go): a TUI dialog read off the screen, the Codex cards a node lists,
// a plan answered in the terminal. Moved from the hub's tests with them.

// Screens with a dialog the manager can read (screen.ParseDialog).
const (
	pickModel    = "  Pick a model\n\n› 1. gpt-6\n  2. gpt-6-mini\n\n  Press enter to confirm or esc to go back\n"
	pickEffort   = "  Pick a reasoning level\n\n› 1. low\n  2. high\n\n  Press enter to confirm or esc to go back\n"
	dialogScreen = "────────────────────\n Do you want to proceed?\n ❯ 1. Yes\n   2. Type something.\n   3. No\n"
)

// cardNode is a node that shows a screen (session.screen) and takes Codex answers (codex.answer).
type cardNode struct {
	mu      sync.Mutex
	answers []map[string]any
	err     error
	screen  string
}

func (c *cardNode) setScreen(s string) {
	c.mu.Lock()
	c.screen = s
	c.mu.Unlock()
}

// newCardFleet is a manager whose machine "mac" has a cardNode.
func newCardFleet(t *testing.T, store bool) (*fleetTest, *cardNode) {
	t.Helper()
	ft := newFleetTestNoStore(t)
	if store {
		ft = newFleetTest(t)
	}
	c := &cardNode{}
	ft.d.mu.Lock()
	ft.d.machines["mac"].call = func(method string, params json.RawMessage) (any, error) {
		var p map[string]any
		_ = json.Unmarshal(params, &p)
		c.mu.Lock()
		defer c.mu.Unlock()
		switch method {
		case "session.screen":
			return map[string]any{"text": c.screen}, nil
		case "codex.answer":
			c.answers = append(c.answers, p)
			if c.err != nil {
				return nil, c.err
			}
			return map[string]any{"ok": true}, nil
		}
		return map[string]any{}, nil
	}
	ft.d.mu.Unlock()
	return ft, c
}

func pendingIDs(s *Service) map[string]string {
	out := map[string]string{}
	for _, ix := range s.pendingInteractions() {
		out[ix.ID] = ix.Kind
	}
	return out
}

// waitCards waits up to d for n interactions and says how long it took.
func waitCards(s *Service, n int, d time.Duration) (map[string]string, time.Duration) {
	start := time.Now()
	for len(pendingIDs(s)) < n && time.Since(start) < d {
		time.Sleep(10 * time.Millisecond)
	}
	return pendingIDs(s), time.Since(start)
}

func blockSession(ft *fleetTest, sess proto.Session) {
	sess.Status = proto.StatusBlocked
	ft.d.setSessions("mac", []proto.Session{sess})
	ft.s.syncScreenInteractions("mac", []proto.Session{sess})
}

func listCodex(s *Service, ixs ...proto.Interaction) {
	b, _ := json.Marshal(map[string]any{"interactions": ixs})
	s.syncCodexInteractions("mac", b)
}

// endedCards records the cards the channels are told are over, for a test to ask about.
func endedCards(s *Service) func(id string) bool {
	var mu sync.Mutex
	seen := map[string]bool{}
	s.fleetEnded = func(ix proto.Interaction, _ *proto.Resolution) {
		mu.Lock()
		seen[ix.ID] = true
		mu.Unlock()
	}
	return func(id string) bool {
		mu.Lock()
		defer mu.Unlock()
		return seen[id]
	}
}

// ---- screen cards ----

// A Claude Code without Persona's hooks (the user's own profile) asks only on the screen: its card is made at once,
// not after the head start a hook's card gets (#751). One with hooks (a hook event came, or Persona started it) waits.
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
		{"started by Persona", proto.Session{SessionID: "s1", Agent: "claude", StartedByJarvis: true}, false, 1400 * time.Millisecond, 3 * time.Second},
	} {
		t.Run(c.name, func(t *testing.T) {
			ft, n := newCardFleet(t, false)
			n.setScreen(pickModel)
			if c.event {
				ft.event("mac", "hook.event", map[string]any{"session_id": "s1", "event": "UserPromptSubmit"})
			}
			blockSession(ft, c.s)
			got, took := waitCards(ft.s, 1, 5*time.Second)
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

// After a screen card's answer the agent asks again while herdr keeps it blocked: no status change comes, so the
// manager looks at the screen again and the new dialog gets its card. The answered dialog still on screen gets none.
func TestScreenCardAfterAnAnswerWhileStillBlocked(t *testing.T) {
	old, oldFor := screenRecheck, screenRecheckFor
	screenRecheck, screenRecheckFor = 50*time.Millisecond, 400*time.Millisecond
	t.Cleanup(func() { screenRecheck, screenRecheckFor = old, oldFor })
	ft, n := newCardFleet(t, false)
	s := ft.s
	n.setScreen(pickModel)
	blockSession(ft, proto.Session{SessionID: "s1", Agent: "claude"})
	got, _ := waitCards(s, 1, 2*time.Second)
	var first string
	for id := range got {
		first = id
	}
	if first == "" {
		t.Fatal("no card")
	}
	if err := s.resolve(first, proto.Resolution{Option: "1"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(600 * time.Millisecond)
	if got := pendingIDs(s); len(got) != 0 {
		t.Fatalf("a card again for the answered dialog: %v", got)
	}
	n.setScreen(pickEffort)
	blockSession(ft, proto.Session{SessionID: "s1", Agent: "claude"})
	got, took := waitCards(s, 1, 2*time.Second)
	second := ""
	for id := range got {
		second = id
	}
	if second == "" || second == first || took > time.Second {
		t.Fatalf("next dialog: %v after %v", got, took)
	}
	if err := s.resolve(second, proto.Resolution{Option: "2"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	n.setScreen("  ⏺ Thinking…\n")
	time.Sleep(80 * time.Millisecond)
	n.setScreen(pickModel)
	got, took = waitCards(s, 1, 2*time.Second)
	if len(got) != 1 || got[first] == "" || took > 300*time.Millisecond {
		t.Fatalf("after the second answer, with no status change: %v after %v", got, took)
	}
	// The keys went to the node with the card's revision.
	if keys := ft.d.callsTo("mac", "session.keys"); len(keys) != 2 || !strings.Contains(string(keys[0]), `"rev"`) {
		t.Fatalf("session.keys %s", keys)
	}
}

// Blocked a moment before the dialog is drawn: the card made then has no dialog. Once the dialog shows, its card
// replaces that one.
func TestScreenCardUpgradesWhenTheDialogShows(t *testing.T) {
	old, oldFor := screenUpgrade, screenUpgradeFor
	screenUpgrade, screenUpgradeFor = 50*time.Millisecond, time.Second
	t.Cleanup(func() { screenUpgrade, screenUpgradeFor = old, oldFor })
	ft, n := newCardFleet(t, false)
	s := ft.s
	n.setScreen("  ✢ Percolating… (running PreToolUse hooks… 1/2 · 3s)\n")
	blockSession(ft, proto.Session{SessionID: "s1", Agent: "claude"})
	got, _ := waitCards(s, 1, 2*time.Second)
	if len(got) != 1 || got["scr_mac_s1"] == "" {
		t.Fatalf("the card before the dialog: %v", got)
	}
	n.setScreen(pickModel)
	start := time.Now()
	for time.Since(start) < 2*time.Second {
		if ids := pendingIDs(s); len(ids) == 1 && ids["scr_mac_s1"] == "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	list := s.pendingInteractions()
	if len(list) != 1 || list[0].ID == "scr_mac_s1" || list[0].Dialog == nil || len(list[0].Dialog.Options) != 2 {
		t.Fatalf("after the dialog showed: %+v", list)
	}
}

// The dialog answered on the computer: the agent moves on and its screen card goes, cancelled in the history too.
func TestScreenCardGoneWhenTheAgentMovesOnIsRecorded(t *testing.T) {
	ft, n := newCardFleet(t, true)
	s := ft.s
	n.setScreen(pickModel)
	blockSession(ft, proto.Session{SessionID: "s1", Agent: "claude"})
	got, _ := waitCards(s, 1, 2*time.Second)
	var id string
	for k := range got {
		id = k
	}
	if id == "" {
		t.Fatal("no card")
	}
	ft.sessions("mac", proto.Session{SessionID: "s1", Agent: "claude", Status: proto.StatusWorking})
	if len(pendingIDs(s)) != 0 {
		t.Fatalf("card still pending: %v", pendingIDs(s))
	}
	if status, _, _ := ft.st.InteractionOutcome(t.Context(), id); status != "cancelled" {
		t.Fatalf("history says %q, want cancelled", status)
	}
}

// A runtime's session (docs/runtimes.md) has no terminal: blocked there, no screen is read; a session with a terminal
// still gets its card.
func TestNoScreenCardWithoutATerminal(t *testing.T) {
	ft, n := newCardFleet(t, false)
	n.setScreen(pickModel)
	runtime := proto.Session{SessionID: "session-dsh", Status: proto.StatusBlocked, Agent: "dsh", SourceKind: proto.SourceRuntime,
		Capabilities: &proto.SessionCapabilities{Read: true, Send: true, Approve: true}}
	ft.d.setSessions("mac", []proto.Session{runtime})
	ft.s.syncScreenInteractions("mac", []proto.Session{runtime})
	time.Sleep(500 * time.Millisecond)
	if got := pendingIDs(ft.s); len(got) != 0 {
		t.Fatalf("a screen card for a session without a terminal: %v", got)
	}
	terminal := proto.Session{SessionID: "s1", Status: proto.StatusBlocked}
	ft.d.setSessions("mac", []proto.Session{runtime, terminal})
	ft.s.syncScreenInteractions("mac", []proto.Session{runtime, terminal})
	got, _ := waitCards(ft.s, 1, 5*time.Second)
	if len(got) != 1 || got["scr_mac_s1_"+screen.ParseDialog(pickModel).Rev] != proto.IxScreen {
		t.Fatalf("want one screen card, for the terminal session: %v", got)
	}
}

func TestScreenAnswer(t *testing.T) {
	d := screen.ParseDialog(dialogScreen)
	p, err := screenAnswer("s1", d, proto.Resolution{Option: "3"})
	if err != nil || p["rev"] != d.Rev || p["option"] != "3" || !slices.Equal(p["keys"].([]string), []string{"3"}) {
		t.Fatalf("%v %v", p, err)
	}
	p, _ = screenAnswer("s1", d, proto.Resolution{Option: "2", Text: "use the staging db"})
	if !slices.Equal(p["pre_keys"].([]string), []string{"2"}) || !slices.Equal(p["keys"].([]string), []string{"enter"}) || p["text"] != "use the staging db" {
		t.Fatalf("type something: %v", p)
	}
	p, _ = screenAnswer("s1", d, proto.Resolution{Keys: []string{"1"}})
	if p["rev"] != d.Rev || p["option"] != nil || !slices.Equal(p["keys"].([]string), []string{"1"}) {
		t.Fatalf("legacy: %v", p)
	}
	if _, err := screenAnswer("s1", d, proto.Resolution{Option: "9"}); err == nil {
		t.Fatal("no such option")
	}
	if p, _ := screenAnswer("s1", nil, proto.Resolution{Keys: []string{"esc"}}); p["rev"] != nil {
		t.Fatalf("%v", p)
	}
}

// The dialog changed under the answer: nothing pressed, the old card is gone (a new one follows); over the route the
// hub relays, 409 with the code, 410 once gone.
func TestResolveScreenChanged(t *testing.T) {
	ft := newFleetTestNoStore(t)
	s := ft.s
	changed := &rpc.Error{Code: screen.PromptChanged, Message: "变了", Data: json.RawMessage(`{"delivered":"none"}`)}
	put := func(id string) {
		s.fleet.mu.Lock()
		s.ixMapLocked()[id] = &pending{ix: proto.Interaction{ID: id, Kind: proto.IxScreen}, local: func(proto.Resolution) error { return changed }}
		s.fleet.mu.Unlock()
	}
	put("scr_a")
	err := s.resolve("scr_a", proto.Resolution{Option: "1"})
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != screen.PromptChanged {
		t.Fatalf("got %v", err)
	}
	if len(s.pendingInteractions()) != 0 {
		t.Fatal("the old card stays gone (a new one follows)")
	}
	if err := s.resolve("scr_a", proto.Resolution{}); !errors.Is(err, errNotPending) {
		t.Fatalf("second answer: %v", err)
	}
	put("scr_b")
	h := s.fleetHandler(http.NotFoundHandler())
	for _, want := range []int{http.StatusConflict, http.StatusGone} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/interactions/scr_b/resolve", strings.NewReader(`{"option":"1"}`)))
		if w.Code != want {
			t.Fatalf("status %d, want %d: %s", w.Code, want, w.Body)
		}
		if want == http.StatusConflict && !strings.Contains(w.Body.String(), `"code":"prompt_changed"`) {
			t.Fatal(w.Body.String())
		}
	}
}

// ---- Codex cards ----

func TestCodexCardsFollowTheNodesList(t *testing.T) {
	ft, n := newCardFleet(t, false)
	s := ft.s
	s.fleet.mu.Lock()
	s.ixMapLocked()["scr_mac_s1"] = &pending{ix: proto.Interaction{ID: "scr_mac_s1", MachineID: "mac", SessionID: "s1", Kind: proto.IxScreen},
		local: func(proto.Resolution) error { return nil }}
	s.fleet.mu.Unlock()
	ended := endedCards(s)
	q := proto.Interaction{ID: "cxq_1", SessionID: "s1", Kind: proto.IxQuestion, Agent: "codex", ExpiresAt: time.Now().Add(time.Hour),
		Questions: []proto.Question{{Question: "Which color?", Options: []proto.QuestionOption{{Label: "Red"}}}}}
	a := proto.Interaction{ID: "cxa_1", SessionID: "s2", Kind: proto.IxPermission, Agent: "codex", Summary: "curl -sI https://example.com",
		ExpiresAt: time.Now().Add(time.Hour)}
	listCodex(s, q, a)
	if got := pendingIDs(s); len(got) != 2 || got["cxq_1"] != proto.IxQuestion || got["cxa_1"] != proto.IxPermission {
		t.Fatalf("pending %v", got)
	}
	if !ended("scr_mac_s1") {
		t.Fatal("the replaced screen card was never said to be over (the bot's card for it stays open)")
	}
	listCodex(s, q, a)
	listCodex(s, a)
	if got := pendingIDs(s); len(got) != 1 || got["cxa_1"] == "" {
		t.Fatalf("after the question was answered at the computer: %v", got)
	}
	yes := true
	if err := s.resolve("cxa_1", proto.Resolution{Allow: &yes, Always: true}); err != nil {
		t.Fatal(err)
	}
	n.mu.Lock()
	got := n.answers
	n.mu.Unlock()
	if len(got) != 1 || got[0]["id"] != "cxa_1" || got[0]["session_id"] != "s2" ||
		got[0]["resolution"].(map[string]any)["always"] != true {
		t.Fatalf("node asked %v", got)
	}
	if len(pendingIDs(s)) != 0 {
		t.Fatalf("still pending after the answer: %v", pendingIDs(s))
	}
}

// The node could not type the answer (a draft at the computer): the error reaches the app and the card stays; when the
// node says Codex no longer asks it, the card is gone.
func TestCodexCardAnswerRefused(t *testing.T) {
	ft, n := newCardFleet(t, false)
	s := ft.s
	listCodex(s, proto.Interaction{ID: "cxq_1", SessionID: "s1", Kind: proto.IxQuestion, Agent: "codex", ExpiresAt: time.Now().Add(time.Hour)})
	n.mu.Lock()
	n.err = &rpc.Error{Code: screen.InputDraft, Message: "draft"}
	n.mu.Unlock()
	err := s.resolve("cxq_1", proto.Resolution{Answers: map[string]string{"Which color?": "Red"}})
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != screen.InputDraft {
		t.Fatalf("err %v", err)
	}
	if pendingIDs(s)["cxq_1"] == "" {
		t.Fatal("the card did not come back after a refused answer")
	}
	n.mu.Lock()
	n.err = &rpc.Error{Code: screen.PromptChanged, Message: "gone"}
	n.mu.Unlock()
	_ = s.resolve("cxq_1", proto.Resolution{Answers: map[string]string{"Which color?": "Red"}})
	if len(pendingIDs(s)) != 0 {
		t.Fatalf("a card Codex no longer asks came back: %v", pendingIDs(s))
	}
}

// A refused answer leaves the card on the apps: the last thing the hub hears of it is the card again, not that it is
// over, and the bot's card for it stays open.
func TestCodexCardRefusedStaysOnTheApps(t *testing.T) {
	ft, n := newCardFleet(t, false)
	s := ft.s
	ended := endedCards(s)
	listCodex(s, proto.Interaction{ID: "cxq_1", SessionID: "s1", Kind: proto.IxQuestion, Agent: "codex", ExpiresAt: time.Now().Add(time.Hour)})
	n.mu.Lock()
	n.err = &rpc.Error{Code: screen.InputDraft, Message: "draft"}
	n.mu.Unlock()
	if err := s.resolve("cxq_1", proto.Resolution{Answers: map[string]string{"Which color?": "Red"}}); err == nil {
		t.Fatal("a refused answer reported no error")
	}
	last := ""
	ft.f.mu.Lock()
	for _, tl := range ft.f.told {
		switch tl.event {
		case assistantwire.EventFleetInteraction:
			if strings.Contains(string(tl.params), `"id":"cxq_1"`) {
				last = "interaction"
			}
		case assistantwire.EventFleetResolved:
			if strings.Contains(string(tl.params), `"id":"cxq_1"`) {
				last = "interaction_resolved"
			}
		}
	}
	ft.f.mu.Unlock()
	if last != "interaction" {
		t.Fatalf("the hub last heard %q of a card that is still open", last)
	}
	if ended("cxq_1") {
		t.Fatal("the bot was told a card that is still open is over")
	}
}

// A node that reconnects lists its open cards again: they come back without ringing the phone a second time.
func TestCodexCardsRelistedQuietly(t *testing.T) {
	ft, _ := newCardFleet(t, false)
	s := ft.s
	q := proto.Interaction{ID: "cxq_1", SessionID: "s1", Kind: proto.IxQuestion, Agent: "codex", ExpiresAt: time.Now().Add(time.Hour)}
	listCodex(s, q)
	s.NodeDetached("mac") // the node went away
	if len(pendingIDs(s)) != 0 {
		t.Fatal("not cancelled with its node")
	}
	listCodex(s, q)
	if pendingIDs(s)["cxq_1"] == "" {
		t.Fatal("not listed again")
	}
	if got := ft.pushesOf(assistantwire.NotifyInput, 2, 200*time.Millisecond); len(got) != 1 {
		t.Fatalf("pushed %d times", len(got))
	}
}

// An async question is not on the terminal's keyboard: no screen card for the bare input box, one for a real dialog,
// and an existing screen card stays when the question's card comes.
func TestCodexAsyncQuestionLeavesTheTerminalCard(t *testing.T) {
	ft, n := newCardFleet(t, false)
	s := ft.s
	q := proto.Interaction{ID: "cxq_1", SessionID: "s1", Kind: proto.IxQuestion, Agent: "codex", ToolName: "request_user_input_async",
		ExpiresAt: time.Now().Add(time.Hour)}
	listCodex(s, q)
	blocked := []proto.Session{{SessionID: "s1", Status: proto.StatusBlocked}}
	ft.d.setSessions("mac", blocked)
	s.syncScreenInteractions("mac", blocked)
	time.Sleep(500 * time.Millisecond)
	if got := pendingIDs(s); len(got) != 1 {
		t.Fatalf("a screen card beside the async question for a bare box: %v", got)
	}
	n.setScreen(pickModel)
	s.syncScreenInteractions("mac", blocked)
	deadline := time.Now().Add(5 * time.Second)
	for len(pendingIDs(s)) < 2 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	got := pendingIDs(s)
	screens := 0
	for _, kind := range got {
		if kind == proto.IxScreen {
			screens++
		}
	}
	if screens != 1 || got["cxq_1"] == "" {
		t.Fatalf("a dialog on screen beside the async question: %v", got)
	}
	listCodex(s, q, proto.Interaction{ID: "cxq_2", SessionID: "s1", Kind: proto.IxQuestion, Agent: "codex",
		ToolName: "request_user_input_async", ExpiresAt: time.Now().Add(time.Hour)})
	if got := pendingIDs(s); len(got) != 3 {
		t.Fatalf("the screen card went: %v", got)
	}
}

// ---- plans answered in the terminal ----

func planItems(at time.Time) []proto.Item {
	return []proto.Item{
		{ID: "call#0", Seq: 10, TS: at.Add(-time.Millisecond), Role: "assistant", Kind: proto.KindToolUse,
			ToolName: "ExitPlanMode", ToolUseID: "plan-call"},
		{ID: "result#0", Seq: 11, TS: at.Add(time.Second), Role: "user", Kind: proto.KindToolResult,
			ToolUseID: "plan-call", Text: "The plan was answered in the terminal."},
	}
}

func TestPlanCallFinishedRequiresTheMatchingToolResult(t *testing.T) {
	at := time.Now()
	ix := proto.Interaction{CreatedAt: at}
	valid := planItems(at)
	for _, tc := range []struct {
		name  string
		items []proto.Item
		id    string
		want  bool
	}{
		{"id omitted by PermissionRequest", valid, "", true},
		{"exact tool id", valid, "plan-call", true},
		{"wrong tool id", valid, "other-call", false},
		{"still waiting, even if working", valid[:1], "", false},
		{"model says approved, not a result", []proto.Item{valid[0], {Role: "assistant", Kind: proto.KindText, Text: "User approved."}}, "", false},
		{"human says approved, not a result", []proto.Item{valid[0], {Role: "user", Kind: proto.KindText, Text: "Approved."}}, "", false},
		{"another tool finished", []proto.Item{valid[0], {Role: "user", Kind: proto.KindToolResult, ToolUseID: "bash", TS: at.Add(time.Second)}}, "", false},
		{"old plan", planItems(at.Add(-time.Minute)), "", false},
		{"plan created later", planItems(at.Add(time.Minute)), "", false},
		{"two candidate calls", append(append([]proto.Item{}, valid...), proto.Item{Role: "assistant", Kind: proto.KindToolUse,
			ToolName: "ExitPlanMode", ToolUseID: "other-plan", TS: at.Add(-2 * time.Millisecond)}), "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ix.ToolUseID = tc.id
			if got := planCallFinished(ix, tc.items); got != tc.want {
				t.Fatalf("finished = %v, want %v", got, tc.want)
			}
		})
	}
	for _, mutate := range []func([]proto.Item){
		func(x []proto.Item) { x[0].TS = time.Time{} },
		func(x []proto.Item) { x[1].TS = time.Time{} },
		func(x []proto.Item) { x[1].TS = at.Add(-time.Second) },
		func(x []proto.Item) { x[1].Role = "assistant" },
	} {
		x := planItems(at)
		mutate(x)
		if planCallFinished(proto.Interaction{CreatedAt: at}, x) {
			t.Fatal("missing or earlier completion cannot answer a pending plan")
		}
	}
	valid[1].IsError = true
	if !planCallFinished(proto.Interaction{CreatedAt: at}, valid) {
		t.Fatal("an ended, rejected call must not leave an approval card")
	}
}

// planFleet is a manager whose machine's node reads its transcript pages with read.
func planFleet(t *testing.T, store bool, read func(int) ([]proto.Item, bool, error)) *fleetTest {
	t.Helper()
	ft := newFleetTestNoStore(t)
	if store {
		ft = newFleetTest(t)
	}
	ft.d.mu.Lock()
	ft.d.machines["mac"].call = func(method string, params json.RawMessage) (any, error) {
		if method != "session.items" {
			return nil, fmt.Errorf("unexpected %s", method)
		}
		var p struct {
			Before int `json:"before_seq"`
		}
		_ = json.Unmarshal(params, &p)
		items, more, err := read(p.Before)
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": items, "has_more": more}, nil
	}
	ft.d.mu.Unlock()
	return ft
}

func putPending(s *Service, p *pending) {
	s.fleet.mu.Lock()
	s.ixMapLocked()[p.ix.ID] = p
	s.fleet.mu.Unlock()
}

func TestTerminalPlanResultClearsTheCardWithoutApprovingAnything(t *testing.T) {
	for _, trigger := range []string{"sessions", "revs"} {
		t.Run(trigger, func(t *testing.T) {
			at := time.Now()
			ft := planFleet(t, true, func(int) ([]proto.Item, bool, error) { return planItems(at), false, nil })
			s := ft.s
			ix := proto.Interaction{ID: "plan", MachineID: "mac", SessionID: "session", Kind: proto.IxPlan,
				ToolName: "ExitPlanMode", CreatedAt: at, ExpiresAt: at.Add(time.Hour), Resumable: true}
			p := &pending{ix: ix, done: make(chan proto.Resolution, 1)}
			putPending(s, p)
			s.record(ix)
			if trigger == "sessions" {
				ft.sessions("mac", proto.Session{SessionID: "session", Status: proto.StatusWorking})
			} else {
				ft.d.setSessions("mac", []proto.Session{{SessionID: "session", Status: proto.StatusWorking}})
				if !ft.event("mac", "sessions.revs", map[string]any{"revs": map[string]int{"session": 1234}}) {
					t.Fatal("the hub no longer gets the revs")
				}
			}
			ft.waitPending(0)
			select {
			case r := <-p.done:
				if !r.Dismiss || r.Approve != nil || r.Allow != nil || r.Mode != "" {
					t.Fatalf("external completion must only release the obsolete hook: %+v", r)
				}
			case <-time.After(time.Second):
				t.Fatal("the old hook stayed blocked")
			}
			if sess := s.decorated("mac")[0]; sess.PendingInteractions != 0 || sess.State == proto.StateNeedsYou {
				t.Fatalf("session still asks for approval: %+v", sess)
			}
			status, res, _ := ft.st.InteractionOutcome(context.Background(), ix.ID)
			if status != "dismissed" || res != nil {
				t.Fatalf("must not record a made-up phone approval: %s %+v", status, res)
			}
			if len(ft.toldOf(assistantwire.EventFleetResolved)) == 0 {
				t.Fatal("the hub never got interaction_resolved")
			}
		})
	}
}

func TestPlanReconcileCrossesAPageAndKeepsUnrelatedApprovals(t *testing.T) {
	at := time.Now()
	items := planItems(at)
	ft := planFleet(t, false, func(before int) ([]proto.Item, bool, error) {
		if before == 0 {
			return items[1:], true, nil
		}
		return items[:1], false, nil
	})
	s := ft.s
	putPending(s, &pending{ix: proto.Interaction{ID: "old-plan", MachineID: "mac", SessionID: "session", Kind: proto.IxPlan,
		ToolName: "ExitPlanMode", CreatedAt: at}, done: make(chan proto.Resolution, 1)})
	permission := &pending{ix: proto.Interaction{ID: "permission", MachineID: "mac", SessionID: "session", Kind: proto.IxPermission}}
	question := &pending{ix: proto.Interaction{ID: "question", MachineID: "mac", SessionID: "session", Kind: proto.IxQuestion}}
	putPending(s, permission)
	putPending(s, question)
	s.syncPlanInteractions("mac")
	ft.waitPending(2)
	s.fleet.mu.Lock()
	defer s.fleet.mu.Unlock()
	if s.fleet.interactions["permission"] != permission || s.fleet.interactions["question"] != question {
		t.Fatal("finishing a plan must not answer unrelated questions or permissions")
	}
}

// #3563: Claude Code does not end a PermissionRequest hook whose dialog was answered in the terminal: the tool's card
// stayed on the phone until the hook's timeout. Its tool call's result ends it, as a plan's does; a permission card
// that names no tool call is never guessed at, and one the phone just answered keeps its answer.
func TestTerminalPermissionResultClearsTheCard(t *testing.T) {
	at := time.Now()
	items := []proto.Item{
		{ID: "call#0", Seq: 10, TS: at.Add(-time.Millisecond), Role: "assistant", Kind: proto.KindToolUse, ToolName: "Bash", ToolUseID: "bash-call"},
		{ID: "result#0", Seq: 11, TS: at.Add(time.Second), Role: "user", Kind: proto.KindToolResult, ToolUseID: "bash-call", Text: "ok"},
	}
	ft := planFleet(t, true, func(int) ([]proto.Item, bool, error) { return items, false, nil })
	s := ft.s
	ix := proto.Interaction{ID: "perm", MachineID: "mac", SessionID: "session", Kind: proto.IxPermission, ToolName: "Bash",
		ToolUseID: "bash-call", CreatedAt: at, ExpiresAt: at.Add(time.Hour), Resumable: true}
	p := &pending{ix: ix, done: make(chan proto.Resolution, 1)}
	putPending(s, p)
	s.record(ix)
	noID := &pending{ix: proto.Interaction{ID: "perm-no-id", MachineID: "mac", SessionID: "session", Kind: proto.IxPermission,
		ToolName: "Bash", CreatedAt: at}, done: make(chan proto.Resolution, 1)}
	putPending(s, noID)
	ft.sessions("mac", proto.Session{SessionID: "session", Status: proto.StatusWorking})
	ft.waitPending(1)
	select {
	case r := <-p.done:
		if !r.Dismiss || r.Allow != nil || r.Always {
			t.Fatalf("a terminal answer must only release the hook: %+v", r)
		}
	case <-time.After(time.Second):
		t.Fatal("the hook stayed blocked")
	}
	if status, res, _ := ft.st.InteractionOutcome(context.Background(), ix.ID); status != "dismissed" || res != nil {
		t.Fatalf("must not record a made-up phone answer: %s %+v", status, res)
	}
	s.fleet.mu.Lock()
	kept := s.fleet.interactions["perm-no-id"] == noID
	s.fleet.mu.Unlock()
	if !kept {
		t.Fatal("a permission card without its tool call's id was dismissed by another call's result")
	}
}

func TestPermissionCallIsNeverGuessed(t *testing.T) {
	at := time.Now()
	// A plan's call and result right before and after the card: a plan without an id takes them, a permission does not.
	if planCallFinished(proto.Interaction{Kind: proto.IxPermission, ToolName: "Bash", CreatedAt: at}, planItems(at)) {
		t.Fatal("a permission card without its tool call's id was ended by another call's result")
	}
	for _, p := range []*pending{
		{ix: proto.Interaction{Kind: proto.IxPermission, ToolUseID: "c", Resumable: true, Agent: "codex"}},
		{ix: proto.Interaction{Kind: proto.IxPermission, ToolUseID: "c"}},
		{ix: proto.Interaction{Kind: proto.IxPermission, Resumable: true}},
	} {
		if terminalReconciled(p) {
			t.Fatalf("only a Claude Code hook's permission naming its call is reconciled: %+v", p.ix)
		}
	}
}

func TestTerminalReconcileKeepsAPhoneAnswer(t *testing.T) {
	at := time.Now()
	ft := planFleet(t, false, func(int) ([]proto.Item, bool, error) { return planItems(at), false, nil })
	s := ft.s
	p := &pending{ix: proto.Interaction{ID: "plan", MachineID: "mac", SessionID: "session", Kind: proto.IxPlan,
		ToolName: "ExitPlanMode", ToolUseID: "plan-call", CreatedAt: at}, done: make(chan proto.Resolution, 1), claimed: true}
	putPending(s, p)
	s.checkPlanInteraction("mac", p)
	s.fleet.mu.Lock()
	still := s.fleet.interactions["plan"] == p
	s.fleet.mu.Unlock()
	if !still {
		t.Fatal("a card the phone answered was dismissed under its answer")
	}
	select {
	case r := <-p.done:
		t.Fatalf("the hook got a dismiss instead of the phone's answer: %+v", r)
	default:
	}
}

func TestAnInFlightPlanReadDoesNotDismissANewerCard(t *testing.T) {
	at := time.Now()
	started, release := make(chan struct{}), make(chan struct{})
	ft := planFleet(t, false, func(int) ([]proto.Item, bool, error) {
		close(started)
		<-release
		return planItems(at), false, nil
	})
	s := ft.s
	old := &pending{ix: proto.Interaction{ID: "plan", Kind: proto.IxPlan, ToolName: "ExitPlanMode", CreatedAt: at}}
	putPending(s, old)
	done := make(chan struct{})
	go func() { s.checkPlanInteraction("mac", old); close(done) }()
	<-started
	newer := &pending{ix: proto.Interaction{ID: "plan", Kind: proto.IxPlan, ToolName: "ExitPlanMode", CreatedAt: at.Add(time.Minute)}}
	putPending(s, newer)
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("read did not finish")
	}
	s.fleet.mu.Lock()
	defer s.fleet.mu.Unlock()
	if s.fleet.interactions["plan"] != newer {
		t.Fatal("an old read removed a new pending plan")
	}
}

func TestPlanProbeFailureKeepsTheCardAndIsThrottled(t *testing.T) {
	var calls atomic.Int32
	ft := planFleet(t, false, func(int) ([]proto.Item, bool, error) {
		calls.Add(1)
		return nil, false, fmt.Errorf("node read failed")
	})
	s := ft.s
	p := &pending{ix: proto.Interaction{ID: "plan", MachineID: "mac", SessionID: "session", Kind: proto.IxPlan,
		ToolName: "ExitPlanMode", CreatedAt: time.Now()}, done: make(chan proto.Resolution, 1)}
	putPending(s, p)
	s.syncPlanInteractions("mac")
	deadline := time.Now().Add(time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	for i := 0; i < 20; i++ {
		s.syncPlanInteractions("mac")
	}
	if calls.Load() != 1 {
		t.Fatalf("one pending plan read, got %d", calls.Load())
	}
	ft.waitPending(1)
	select {
	case r := <-p.done:
		t.Fatalf("a failed read must not release the hook: %+v", r)
	default:
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
	ft, n := newCardFleet(t, false)
	s := ft.s
	n.setScreen(noticeUnderNumberedAnswer)
	blockSession(ft, proto.Session{SessionID: "s1", Agent: "claude"})
	got, _ := waitCards(s, 1, 2*time.Second)
	if _, ok := got["scr_mac_s1"]; !ok {
		t.Fatalf("no card for the notice: %v", got)
	}
	for _, ix := range s.pendingInteractions() {
		if ix.SessionID == "s1" && ix.Dialog != nil {
			t.Fatalf("the answer's numbered list became the card's options: %q %+v", ix.Dialog.Question, ix.Dialog.Options)
		}
	}
}

// The same Codex question asked again rings the phone again (#3766): after it was handed back to the computer, and
// after it was answered at the computer and asked again a while later. A reconnect's relisting stays quiet.
func TestCodexCardAskedAgainIsAnnounced(t *testing.T) {
	old := codexAskedAgain
	codexAskedAgain = 50 * time.Millisecond
	t.Cleanup(func() { codexAskedAgain = old })
	ft, _ := newCardFleet(t, false)
	s := ft.s
	a := proto.Interaction{ID: "cxa_1", SessionID: "s2", Kind: proto.IxPermission, Agent: "codex", Summary: "npm test",
		ExpiresAt: time.Now().Add(time.Hour)}
	listCodex(s, a)
	if got := ft.pushesOf(assistantwire.NotifyInput, 1, time.Second); len(got) != 1 {
		t.Fatalf("first: pushed %d times", len(got))
	}

	// Handed back to the computer, then asked again: a push.
	if err := s.resolve("cxa_1", proto.Resolution{Dismiss: true}); err != nil {
		t.Fatal(err)
	}
	listCodex(s)
	listCodex(s, a)
	if got := ft.pushesOf(assistantwire.NotifyInput, 2, time.Second); len(got) != 2 {
		t.Fatalf("after the hand-back: pushed %d times", len(got))
	}

	// Gone from the list for a moment (a failed read, a reconnect): listed again quietly.
	listCodex(s)
	listCodex(s, a)
	if got := ft.pushesOf(assistantwire.NotifyInput, 3, 200*time.Millisecond); len(got) != 2 {
		t.Fatalf("a quick relisting pushed: %d", len(got))
	}

	// Answered at the computer, asked again a while later: a push.
	listCodex(s)
	time.Sleep(2 * codexAskedAgain)
	listCodex(s, a)
	if got := ft.pushesOf(assistantwire.NotifyInput, 3, time.Second); len(got) != 3 {
		t.Fatalf("asked again later: pushed %d times", len(got))
	}
}
