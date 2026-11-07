package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// changedTo reports whether an inbox.changed announcement for id went out.
func changedTo(h *inboxHub, id string) bool {
	for _, c := range h.in.changes() {
		for _, x := range c.Items {
			if x.ID == id {
				return true
			}
		}
	}
	return false
}

// stateOf is the item's stored form, under the inbox lock.
func stateOf(h *inboxHub, it *InboxItem) string {
	h.inbox.mu.Lock()
	defer h.inbox.mu.Unlock()
	return string(itemState(it))
}

// handlingItem is a Needs you item whose processing conversation s1 is looking into it.
func handlingItem(h *inboxHub, id string) *InboxItem {
	now := time.Now()
	it := actionItem(id, now)
	h.inbox.mu.Lock()
	defer h.inbox.mu.Unlock()
	h.inbox.items[id] = it
	it.Agent = &InboxAgent{State: AgentRunning, MachineID: "server", SessionID: "s1", Of: it.Last, StartedAt: &now}
	h.inbox.noteHandling(it, "server", "s1")
	h.inbox.setHandling(it, HandlingLooking, "", now)
	return it
}

// Every pass that writes inbox rows (#4366): with the store down the item gets back what its row holds (chain,
// handling, suggestion and filing included), no change event goes out, and the next pass converges once the store is
// back. The start is the exception: its conversation is live on a machine.
func TestInboxSaveFailureRollback(t *testing.T) {
	ctx := context.Background()
	t.Run("escalation", func(t *testing.T) {
		h, st := newBulkFailHub(t)
		now := time.Now()
		it := actionItem("a", now)
		h.inbox.items["a"] = it
		before := stateOf(h, it)
		st.failAll = true
		h.saveWrites(ctx, h.inbox.escalate(now.Add(-time.Hour), now), "its escalation")
		if stateOf(h, it) != before || it.Agent != nil || it.Handling != nil || st.hasRow("a") || changedTo(h, "a") {
			t.Fatalf("the failed escalation stuck: agent %+v handling %+v chain %d", it.Agent, it.Handling, len(it.Chain))
		}
		st.failAll = false
		if ws := h.inbox.escalate(now.Add(-time.Hour), now); len(ws) != 1 || len(h.saveWrites(ctx, ws, "")) != 0 || !st.hasRow("a") {
			t.Fatal("the next pass did not escalate it again")
		}
	})
	t.Run("adoption", func(t *testing.T) {
		h, st := newBulkFailHub(t)
		it := actionItem("a", time.Now())
		it.Agent = &InboxAgent{State: AgentQueued, Task: "t"}
		h.inbox.items["a"] = it
		before := stateOf(h, it)
		h.originSet(ctx, "server", "s-found", OriginInbox, it.ID)
		h.f.setSessions("server", proto.Session{SessionID: "s-found", Status: proto.StatusIdle})
		st.failAll = true
		h.startInboxAgents(ctx, InboxAgentConfig{Enabled: true})
		if stateOf(h, it) != before || it.Agent.SessionID != "" || h.f.made != 0 {
			t.Fatalf("the failed adoption stuck: %+v", it.Agent)
		}
		st.failAll = false
		h.startInboxAgents(ctx, InboxAgentConfig{Enabled: true})
		if it.Agent.SessionID != "s-found" || !st.hasRow("a") || h.f.made != 0 {
			t.Fatalf("the next pass did not adopt it: %+v", it.Agent)
		}
	})
	t.Run("start keeps the live conversation", func(t *testing.T) {
		h, st := newBulkFailHub(t)
		it := actionItem("a", time.Now())
		it.Agent = &InboxAgent{State: AgentQueued, Task: "t"}
		h.inbox.items["a"] = it
		st.failAll = true
		h.startInboxAgents(ctx, InboxAgentConfig{Enabled: true})
		// Undone, the next pass would start a second conversation with a fresh attempt budget.
		if a := it.Agent; a.State != AgentRunning || a.SessionID == "" || a.Attempts != 1 || h.f.made != 1 || st.hasRow("a") {
			t.Fatalf("the failed start lost the live conversation: %+v (made %d)", a, h.f.made)
		}
		st.failAll = false
		h.startInboxAgents(ctx, InboxAgentConfig{Enabled: true})
		if h.f.made != 1 {
			t.Fatalf("the next pass started another conversation: %d", h.f.made)
		}
	})
	t.Run("start refused for now", func(t *testing.T) {
		h, st := newBulkFailHub(t)
		it := actionItem("a", time.Now())
		it.Agent = &InboxAgent{State: AgentQueued, Task: "t"}
		h.inbox.items["a"] = it
		h.f.mu.Lock()
		h.f.failAssistant = errors.New("network down") // an unknown result: a conversation may exist
		h.f.mu.Unlock()
		st.failAll = true
		h.startInboxAgents(ctx, InboxAgentConfig{Enabled: true})
		// Its backoff and spent attempt survive the failed row: the attempt budget holds.
		if a := it.Agent; a.State != AgentQueued || a.RetryAfter == nil || a.Attempts != 1 {
			t.Fatalf("the temporary refusal lost its backoff: %+v", a)
		}
	})
	t.Run("sweep", func(t *testing.T) {
		h, st := newBulkFailHub(t)
		it := handlingItem(h, "a")
		old := time.Now().Add(-time.Hour)
		it.Agent.StartedAt = &old
		before := stateOf(h, it)
		st.failAll = true
		h.sweepInboxAgents(ctx, InboxAgentConfig{}, time.Now())
		if stateOf(h, it) != before || it.Handling.State != HandlingLooking || st.hasRow("a") {
			t.Fatalf("the failed timeout stuck: %+v %+v", it.Agent, it.Handling)
		}
		st.failAll = false
		h.sweepInboxAgents(ctx, InboxAgentConfig{}, time.Now())
		if it.Handling.State != HandlingFailed || !st.hasRow("a") {
			t.Fatalf("the next sweep did not settle it: %+v", it.Handling)
		}
	})
	t.Run("suggestion", func(t *testing.T) {
		h, st := newBulkFailHub(t)
		it := handlingItem(h, "a")
		agent := it.Agent
		before := stateOf(h, it)
		st.failAll = true
		if _, _, err := h.suggest(ctx, "s1", suggestIn{Kind: SuggestReply, Text: "好的，周五前给"}); !errors.Is(err, errInboxNotPersisted) {
			t.Fatalf("the conversation was not told the suggestion failed: %v", err)
		}
		if stateOf(h, it) != before || it.Suggestion != nil || it.Handling.State != HandlingLooking || it.Agent != agent || changedTo(h, "a") {
			t.Fatalf("the failed suggestion stuck: %+v %+v", it.Suggestion, it.Handling)
		}
		// The older inbox_draft says the same, not that the conversation is not an inbox one.
		if _, err := h.runTool(ctx, "server", "s1", "inbox_draft", json.RawMessage(`{"text":"好的"}`)); err == nil ||
			strings.Contains(err.Error(), "not started for an inbox item") || stateOf(h, it) != before {
			t.Fatalf("inbox_draft with the store down: %v", err)
		}
		st.failAll = false
		if _, _, err := h.suggest(ctx, "s1", suggestIn{Kind: SuggestReply, Text: "好的，周五前给"}); err != nil || it.Suggestion == nil || !st.hasRow("a") {
			t.Fatalf("handing it in again: %v %+v", err, it.Suggestion)
		}
	})
	t.Run("triage", func(t *testing.T) {
		h, st := newBulkFailHub(t)
		h.in.setModel(fakeModel(t, `{"tier":"fyi","summary":"s","reason":"r"}`))
		it := actionItem("a", time.Now())
		it.Tier, it.TriagedAt, it.Read = "", "", true // read elsewhere: triage files it, and a failed save reopens it
		h.inbox.items["a"] = it
		before := stateOf(h, it)
		st.failAll = true
		w := h.triage(ctx, it)
		h.saveWrites(ctx, []*inboxWrite{w}, "its triage")
		if w == nil || stateOf(h, it) != before || !it.open() || it.TriagedAt != "" || st.hasRow("a") || changedTo(h, "a") {
			t.Fatalf("the failed triage stuck: %+v", it)
		}
		st.failAll = false
		h.saveWrites(ctx, []*inboxWrite{h.triage(ctx, it)}, "its triage")
		if it.open() || !st.hasRow("a") {
			t.Fatalf("the next pass did not converge: open=%v", it.open())
		}
	})
	t.Run("group keeps its events", func(t *testing.T) {
		// The batch is spent once its events are written: undone, they would never come back (and a connector with
		// meta would get stale meta). Memory keeps them; the row catches up at their next save.
		h, st := newBulkFailHub(t)
		now := time.Now()
		old := &InboxItem{ID: "example:th#m1", Source: "example", Kind: KindGroup, Tier: TierFYI, Last: "m0",
			TriagedAt: "m0", UpdatedAt: now, Context: "(m1)", Group: &InboxGroup{Connector: "example", Thread: "th"}}
		h.inbox.items[old.ID] = old
		c := assistantwire.InboxSource{ID: "example", Name: "Example"}
		thing := connector.InboxThing{Kind: KindGroup, Thread: "th", At: now, Last: "batch",
			Group: &connector.ThingGroup{New: []string{"m1", "m2"}}, Context: "(m1)\n(m2)"}
		plan := &groupPlan{model: true, events: []plannedEvent{
			{existing: "example:th#m1", messages: []string{"m1"}, title: "T1", tier: TierFYI, reason: "r"},
			{messages: []string{"m2"}, title: "T2", tier: TierFYI, reason: "r"},
		}}
		st.failAll = true
		h.writeGroup(ctx, c, thing, plan)
		if h.inbox.items["example:th#m2"] == nil || old.Last != "batch" || st.hasRow("example:th#m1") {
			t.Fatalf("the events were undone: m2=%v old.Last=%q", h.inbox.items["example:th#m2"], old.Last)
		}
		st.failAll = false
		h.saveInbox(ctx, []*InboxItem{old})
		if !st.hasRow("example:th#m1") {
			t.Fatal("the next save did not catch the row up")
		}
	})
	t.Run("group racing a dismiss", func(t *testing.T) {
		h, st := newBulkFailHub(t)
		mux := h.routes()
		now := time.Now()
		id := "example:th#m1"
		old := &InboxItem{ID: id, Source: "example", Kind: KindGroup, Tier: TierFYI, Last: "m0",
			TriagedAt: "m0", UpdatedAt: now, Context: "(m1)", Group: &InboxGroup{Connector: "example", Thread: "th"}}
		h.inbox.items[id] = old
		c := assistantwire.InboxSource{ID: "example", Name: "Example"}
		thing := connector.InboxThing{Kind: KindGroup, Thread: "th", At: now, Last: "batch",
			Group: &connector.ThingGroup{New: []string{"m1", "m2"}}, Context: "(m1)\n(m2)"}
		plan := &groupPlan{model: true, events: []plannedEvent{
			{existing: id, messages: []string{"m1", "m2"}, title: "T1", tier: TierFYI, reason: "r"},
		}}
		second := make(chan int, 1)
		st.failOnce = map[string]bool{id: true}
		st.failHook = func(failedID string) {
			go func() {
				code, _ := inboxCall(t, mux, "POST", "/api/v1/inbox/"+url.PathEscape(failedID)+"/done", `{"done":true,"dismiss":true}`)
				second <- code
			}()
			waitForItem(t, h, failedID, func(it *InboxItem) bool { return it.DoneBy == "dismissed" })
		}
		h.writeGroup(ctx, c, thing, plan)
		if code := <-second; code != 200 {
			t.Fatalf("the concurrent dismiss: %d, want 200", code)
		}
		if it := h.inbox.items[id]; it.open() || it.DoneBy != "dismissed" || st.doneAt(id) == nil {
			t.Fatalf("the event's rollback rewound the dismissal: %+v", it)
		}
	})
	t.Run("mute", func(t *testing.T) {
		h, st := newBulkFailHub(t)
		mux := h.routes()
		seedFailItems(h, "a")
		before := stateOf(h, h.inbox.items["a"])
		st.failAll = true
		if code, _ := inboxCall(t, mux, "POST", "/api/v1/inbox/mutes", `{"item_id":"a","tag":"sender"}`); code != http.StatusInternalServerError {
			t.Fatalf("mute with the store down: %d, want 500", code)
		}
		if it := h.inbox.items["a"]; !it.open() || stateOf(h, it) != before || st.hasRow("a") {
			t.Fatalf("the failed mute filed the item in memory (chain %d) or wrote a row", len(it.Chain))
		}
		st.failAll = false
		if code, out := inboxCall(t, mux, "POST", "/api/v1/inbox/mutes", `{"item_id":"a","tag":"sender"}`); code != 200 || out["settled"] != float64(1) {
			t.Fatalf("the idempotent retry: %d %v", code, out)
		}
	})
	t.Run("mute in a round", func(t *testing.T) {
		h, st := newBulkFailHub(t)
		seedFailItems(h, "a")
		it := h.inbox.items["a"]
		o := mutesOf(it)[0]
		h.inbox.mutes = []InboxMute{{ID: "m1", Source: it.Source, Tag: o.Tag, Value: o.Value, Name: "x"}}
		before := stateOf(h, it)
		st.failAll = true
		if err := h.InboxCollected(ctx, assistantwire.InboxRound{Round: roundPoll}); err != nil {
			t.Fatal(err)
		}
		if !it.open() || stateOf(h, it) != before || st.hasRow("a") {
			t.Fatalf("the round's failed mute filing stuck: done %v chain %d", it.DoneAt, len(it.Chain))
		}
		st.failAll = false
		if err := h.InboxCollected(ctx, assistantwire.InboxRound{Round: roundPoll}); err != nil || it.open() || !st.hasRow("a") {
			t.Fatalf("the next round did not file it: %v open=%v", err, it.open())
		}
	})
	t.Run("task link joins the escalation", func(t *testing.T) {
		h, st := newBulkFailHub(t)
		h.in.setModel(fakeModel(t, `{"task_id":"todo_1"}`))
		now := time.Now()
		it := actionItem("a", now)
		h.inbox.items["a"] = it
		before := stateOf(h, it)
		ws := h.inbox.escalate(now.Add(-time.Hour), now)
		if len(ws) != 1 || !h.linkTaskFor(ctx, it, []prompts.InboxTask{{ID: "todo_1"}}, ws[0]) || it.Agent.TodoID != "todo_1" {
			t.Fatalf("not linked: %+v", it.Agent)
		}
		st.failAll = true
		h.saveWrites(ctx, ws, "its escalation")
		if stateOf(h, it) != before || it.Agent != nil {
			t.Fatalf("the linked escalation was not undone: %+v", it.Agent)
		}
	})
	t.Run("mute racing a dismiss", func(t *testing.T) {
		h, st := newBulkFailHub(t)
		mux := h.routes()
		seedFailItems(h, "a")
		second := make(chan int, 1)
		st.failOnce = map[string]bool{"a": true}
		st.failHook = func(id string) {
			go func() {
				code, _ := inboxCall(t, mux, "POST", "/api/v1/inbox/a/done", `{"done":true,"dismiss":true}`)
				second <- code
			}()
			waitForItem(t, h, id, func(it *InboxItem) bool { return it.DoneBy == "dismissed" })
		}
		if code, _ := inboxCall(t, mux, "POST", "/api/v1/inbox/mutes", `{"item_id":"a","tag":"sender"}`); code != http.StatusInternalServerError {
			t.Fatalf("the failed mute: %d, want 500", code)
		}
		if code := <-second; code != 200 {
			t.Fatalf("the concurrent dismiss: %d, want 200", code)
		}
		if it := h.inbox.items["a"]; it.open() || it.DoneBy != "dismissed" || st.doneAt("a") == nil {
			t.Fatalf("the rollback rewound the dismissal: %+v", it)
		}
	})
}

// restore gives back the stored form and keeps the agent and handling records the item had (code holding them, like
// deliverNotes, still finds them), with the in-memory note count of before.
func TestInboxWriteRestoreKeepsRecords(t *testing.T) {
	h, _ := newBulkFailHub(t)
	it := handlingItem(h, "a")
	agent, handling := it.Agent, it.Handling
	h.inbox.mu.Lock()
	w := beginWrite(it)
	h.inbox.tell(it, inboxNote{Name: "new_messages", Context: "x"}, true, time.Now())
	h.inbox.failHandling(it, "timeout", time.Now())
	h.inbox.settle(w)
	held := h.inbox.holds(w)
	h.inbox.restore(w)
	h.inbox.mu.Unlock()
	if !held || it.Agent != agent || it.Handling != handling || it.Handling.State != HandlingLooking || len(it.Agent.Pending) != 0 || it.Agent.told != 0 {
		t.Fatalf("restore: held=%v agent %+v handling %+v", held, it.Agent, it.Handling)
	}
	// Something else moved it since: it is not the change's to undo.
	h.inbox.mu.Lock()
	w = beginWrite(it)
	it.Recap = "a"
	h.inbox.settle(w)
	it.Recap = "b"
	held = h.inbox.holds(w)
	h.inbox.mu.Unlock()
	if held {
		t.Fatal("a moved item still holds the change")
	}
}
