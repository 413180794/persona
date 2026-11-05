package hub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
	"github.com/nwverse/persona/server/internal/screen"
)

// codexNodeFake answers codex.answer with err (nil: taken) and records what it was asked.
type codexNodeFake struct {
	mu      sync.Mutex
	answers []map[string]any
	err     error
	screen  string // what session.screen shows
}

func newCodexMachine(t *testing.T, h *Hub) (*Machine, *codexNodeFake) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h2nR, h2nW := io.Pipe()
	n2hR, n2hW := io.Pipe()
	hubSide, nodeSide := rpc.New("h", h2nW), rpc.New("n", n2hW)
	f := &codexNodeFake{}
	nodeSide.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		var p map[string]any
		_ = json.Unmarshal(params, &p)
		f.mu.Lock()
		defer f.mu.Unlock()
		if method == "session.screen" {
			return map[string]any{"text": f.screen}, nil
		}
		if method != "codex.answer" {
			return map[string]any{}, nil
		}
		f.answers = append(f.answers, p)
		if f.err != nil {
			return nil, f.err
		}
		return map[string]any{"ok": true}, nil
	}
	go func() { _ = hubSide.Serve(ctx, n2hR) }()
	go func() { _ = nodeSide.Serve(ctx, h2nR) }()
	t.Cleanup(func() { cancel(); _ = h2nW.Close(); _ = n2hW.Close() })
	m := newMachine(h, MachineConfig{ID: "server", Name: "server", Kind: "local"})
	m.peer = hubSide
	h.machines = append(h.machines, m)
	h.byID["server"] = m
	return m, f
}

func listCodex(h *Hub, m *Machine, ixs ...proto.Interaction) {
	b, _ := json.Marshal(map[string]any{"interactions": ixs})
	h.syncCodexInteractions(m, b)
}

func pendingIDs(h *Hub) map[string]string {
	out := map[string]string{}
	for _, ix := range h.pendingInteractions() {
		out[ix.ID] = ix.Kind
	}
	return out
}

// endedCards records the cards the hub tells the assistant are over (interaction.ended), for a test to ask about.
func endedCards(h *Hub) func(id string) bool {
	var mu sync.Mutex
	seen := map[string]bool{}
	h.chans.ended = func(ix proto.Interaction, _ *proto.Resolution) {
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

func TestCodexCardsFollowTheNodesList(t *testing.T) {
	h := newTestHub(t)
	m, f := newCodexMachine(t, h)
	// The generic screen card the hub made for the blocked Codex gives way to the node's reading.
	h.mu.Lock()
	h.interactions["scr_server_s1"] = &pending{ix: proto.Interaction{ID: "scr_server_s1", MachineID: "server", SessionID: "s1", Kind: proto.IxScreen},
		local: func(proto.Resolution) error { return nil }}
	h.mu.Unlock()
	ended := endedCards(h) // the assistant hears it is over: the Feishu bot's card for it says so
	q := proto.Interaction{ID: "cxq_1", SessionID: "s1", Kind: proto.IxQuestion, Agent: "codex", ExpiresAt: time.Now().Add(time.Hour),
		Questions: []proto.Question{{Question: "Which color?", Options: []proto.QuestionOption{{Label: "Red"}}}}}
	a := proto.Interaction{ID: "cxa_1", SessionID: "s2", Kind: proto.IxPermission, Agent: "codex", Summary: "curl -sI https://example.com",
		ExpiresAt: time.Now().Add(time.Hour)}
	listCodex(h, m, q, a)
	if got := pendingIDs(h); len(got) != 2 || got["cxq_1"] != proto.IxQuestion || got["cxa_1"] != proto.IxPermission {
		t.Fatalf("pending %v", got)
	}
	if !ended("scr_server_s1") {
		t.Fatal("the replaced screen card was never said to be over (the bot's card for it stays open)")
	}
	// The same list again changes nothing; one no longer listed (answered at the computer) ends.
	listCodex(h, m, q, a)
	listCodex(h, m, a)
	if got := pendingIDs(h); len(got) != 1 || got["cxa_1"] == "" {
		t.Fatalf("after the question was answered at the computer: %v", got)
	}

	// An answer goes to the node as it was given, for that card.
	yes := true
	if err := h.resolve("cxa_1", proto.Resolution{Allow: &yes, Always: true}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	got := f.answers
	f.mu.Unlock()
	if len(got) != 1 || got[0]["id"] != "cxa_1" || got[0]["session_id"] != "s2" ||
		got[0]["resolution"].(map[string]any)["always"] != true {
		t.Fatalf("node asked %v", got)
	}
	if len(pendingIDs(h)) != 0 {
		t.Fatalf("still pending after the answer: %v", pendingIDs(h))
	}
}

// The node could not type the answer (a draft at the computer): the error reaches the app and the card stays; when the
// node says Codex no longer asks it, the card is gone and the node's next list says what is there now.
func TestCodexCardAnswerRefused(t *testing.T) {
	h := newTestHub(t)
	m, f := newCodexMachine(t, h)
	q := proto.Interaction{ID: "cxq_1", SessionID: "s1", Kind: proto.IxQuestion, Agent: "codex", ExpiresAt: time.Now().Add(time.Hour)}
	listCodex(h, m, q)
	f.mu.Lock()
	f.err = &rpc.Error{Code: screen.InputDraft, Message: "draft"}
	f.mu.Unlock()
	err := h.resolve("cxq_1", proto.Resolution{Answers: map[string]string{"Which color?": "Red"}})
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != screen.InputDraft {
		t.Fatalf("err %v", err)
	}
	if pendingIDs(h)["cxq_1"] == "" {
		t.Fatal("the card did not come back after a refused answer")
	}
	f.mu.Lock()
	f.err = &rpc.Error{Code: screen.PromptChanged, Message: "gone"}
	f.mu.Unlock()
	_ = h.resolve("cxq_1", proto.Resolution{Answers: map[string]string{"Which color?": "Red"}})
	if len(pendingIDs(h)) != 0 {
		t.Fatalf("a card Codex no longer asks came back: %v", pendingIDs(h))
	}
}

// A refused answer leaves the card on the apps: the last thing they hear of it is the card again, not that it is over,
// and the bot's card for it stays open.
func TestCodexCardRefusedStaysOnTheApps(t *testing.T) {
	h := newTestHub(t)
	m, f := newCodexMachine(t, h)
	ended := endedCards(h)
	q := proto.Interaction{ID: "cxq_1", SessionID: "s1", Kind: proto.IxQuestion, Agent: "codex", ExpiresAt: time.Now().Add(time.Hour)}
	listCodex(h, m, q)
	app := &client{out: make(chan []byte, 256)}
	h.mu.Lock()
	h.clients[app] = struct{}{}
	h.mu.Unlock()
	f.mu.Lock()
	f.err = &rpc.Error{Code: screen.InputDraft, Message: "draft"}
	f.mu.Unlock()
	if err := h.resolve("cxq_1", proto.Resolution{Answers: map[string]string{"Which color?": "Red"}}); err == nil {
		t.Fatal("a refused answer reported no error")
	}
	if pendingIDs(h)["cxq_1"] == "" {
		t.Fatal("the card did not come back after a refused answer")
	}
	last := ""
	for drained := false; !drained; {
		select {
		case raw := <-app.out:
			var ev struct {
				Type        string            `json:"type"`
				ID          string            `json:"id"`
				Interaction proto.Interaction `json:"interaction"`
			}
			if json.Unmarshal(raw, &ev) != nil {
				continue
			}
			if (ev.Type == "interaction" && ev.Interaction.ID == "cxq_1") || (ev.Type == "interaction_resolved" && ev.ID == "cxq_1") {
				last = ev.Type
			}
		default:
			drained = true
		}
	}
	if last != "interaction" {
		t.Fatalf("the apps last heard %q of a card that is still open", last)
	}
	if ended("cxq_1") {
		t.Fatal("the bot was told a card that is still open is over")
	}
}

// A node that reconnects lists its open cards again: they come back without ringing the phone a second time.
func TestCodexCardsRelistedQuietly(t *testing.T) {
	h := newTestHub(t)
	m, _ := newCodexMachine(t, h)
	q := proto.Interaction{ID: "cxq_1", SessionID: "s1", Kind: proto.IxQuestion, Agent: "codex", ExpiresAt: time.Now().Add(time.Hour)}
	listCodex(h, m, q)
	h.cancelMachineInteractions("server") // the node went away
	if len(pendingIDs(h)) != 0 {
		t.Fatal("not cancelled with its node")
	}
	listCodex(h, m, q)
	h.mu.Lock()
	_, announced := h.codexAnnounced["cxq_1"]
	h.mu.Unlock()
	if pendingIDs(h)["cxq_1"] == "" || !announced {
		t.Fatal("not listed again")
	}
}

func TestCodexPushTitles(t *testing.T) {
	q := interactionPayload(proto.Interaction{Kind: proto.IxQuestion, Agent: "codex", Questions: []proto.Question{{Question: "Which color?"}}}, "server")
	p := interactionPayload(proto.Interaction{Kind: proto.IxPermission, Agent: "traex", Summary: "rm -rf build"}, "server")
	alert := func(m map[string]any) map[string]any { return m["aps"].(map[string]any)["alert"].(map[string]any) }
	if alert(q)["title"] != "Codex 在问你" || alert(q)["body"] != "Which color?" || alert(p)["title"] != "TRAEX 需要你授权" {
		t.Errorf("%v / %v", alert(q), alert(p))
	}
}

// An async question is not on the terminal's keyboard: herdr calls Codex blocked while it is open, but its card does not
// stand in for a dialog the terminal shows — no screen card for the bare input box, one for a real dialog, and an
// existing screen card stays when the question's card comes.
func TestCodexAsyncQuestionLeavesTheTerminalCard(t *testing.T) {
	h := newTestHub(t)
	m, f := newCodexMachine(t, h)
	q := proto.Interaction{ID: "cxq_1", SessionID: "s1", Kind: proto.IxQuestion, Agent: "codex", ToolName: "request_user_input_async",
		ExpiresAt: time.Now().Add(time.Hour)}
	listCodex(h, m, q)
	blocked := []proto.Session{{SessionID: "s1", Status: proto.StatusBlocked}}
	m.mu.Lock()
	m.sessions = blocked
	m.mu.Unlock()
	h.syncScreenInteractions(m, blocked)
	time.Sleep(2500 * time.Millisecond)
	if got := pendingIDs(h); len(got) != 1 {
		t.Fatalf("a screen card beside the async question for a bare box: %v", got)
	}
	f.mu.Lock()
	f.screen = "  Pick a model\n\n› 1. gpt-6\n  2. gpt-6-mini\n\n  Press enter to confirm or esc to go back\n"
	f.mu.Unlock()
	h.syncScreenInteractions(m, blocked)
	deadline := time.Now().Add(5 * time.Second)
	for len(pendingIDs(h)) < 2 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	got := pendingIDs(h)
	screens := 0
	for _, kind := range got {
		if kind == proto.IxScreen {
			screens++
		}
	}
	if screens != 1 || got["cxq_1"] == "" {
		t.Fatalf("a dialog on screen beside the async question: %v", got)
	}
	// Listed again with a second async question: the screen card stays.
	listCodex(h, m, q, proto.Interaction{ID: "cxq_2", SessionID: "s1", Kind: proto.IxQuestion, Agent: "codex",
		ToolName: "request_user_input_async", ExpiresAt: time.Now().Add(time.Hour)})
	if got := pendingIDs(h); len(got) != 3 {
		t.Fatalf("the screen card went: %v", got)
	}
}
