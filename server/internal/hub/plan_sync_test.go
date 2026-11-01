package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
	"github.com/chyroc/jarvis/server/internal/rpc"
)

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
	valid[1].IsError = true // rejection / interruption ended the call too, but is never an approval
	if !planCallFinished(proto.Interaction{CreatedAt: at}, valid) {
		t.Fatal("an ended, rejected call must not leave an approval card")
	}
}

func planMachine(t *testing.T, h *Hub, read func(int) ([]proto.Item, bool, error)) *Machine {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h2nR, h2nW := io.Pipe()
	n2hR, n2hW := io.Pipe()
	peer, node := rpc.New("hub", h2nW), rpc.New("node", n2hW)
	node.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		if method != "session.items" {
			return nil, fmt.Errorf("unexpected %s", method)
		}
		var p struct {
			Before int `json:"before_seq"`
		}
		_ = json.Unmarshal(params, &p)
		items, more, err := read(p.Before)
		return map[string]any{"items": items, "has_more": more}, err
	}
	go func() { _ = peer.Serve(ctx, n2hR) }()
	go func() { _ = node.Serve(ctx, h2nR) }()
	t.Cleanup(func() { cancel(); _ = h2nW.Close(); _ = n2hW.Close() })
	m := newMachine(h, MachineConfig{ID: "machine", Name: "machine"})
	m.peer = peer
	h.byID[m.cfg.ID] = m
	return m
}

func TestTerminalPlanResultClearsTheCardWithoutApprovingAnything(t *testing.T) {
	for _, trigger := range []string{"sessions", "revs"} {
		t.Run(trigger, func(t *testing.T) {
			h := newTestHub(t)
			h.store = newMemStore()
			at := time.Now()
			m := planMachine(t, h, func(int) ([]proto.Item, bool, error) { return planItems(at), false, nil })
			// Like an old PermissionRequest: no tool_use_id, and the process never sent interaction.cancel.
			ix := proto.Interaction{ID: "plan", MachineID: "machine", SessionID: "session", Kind: proto.IxPlan,
				ToolName: "ExitPlanMode", CreatedAt: at, ExpiresAt: at.Add(time.Hour), Resumable: true}
			p := &pending{ix: ix, done: make(chan proto.Resolution, 1)}
			h.interactions[ix.ID] = p
			h.record(ix)
			m.sessions = []proto.Session{{SessionID: "session", MachineID: "machine", Status: proto.StatusWorking}}
			c := &client{out: make(chan []byte, 32)}
			h.clients[c] = struct{}{}
			if trigger == "sessions" {
				h.sessionsChanged(m)
			} else {
				m.onEvent("sessions.revs", json.RawMessage(`{"revs":{"session":1234}}`))
			}
			waitPending(t, h, 0)
			select {
			case r := <-p.done:
				if !r.Dismiss || r.Approve != nil || r.Allow != nil || r.Mode != "" {
					t.Fatalf("external completion must only release the obsolete hook: %+v", r)
				}
			case <-time.After(time.Second):
				t.Fatal("the old hook stayed blocked")
			}
			if s := h.decorated(m)[0]; s.PendingInteractions != 0 || s.State == proto.StateNeedsYou {
				t.Fatalf("session still asks for approval: %+v", s)
			}
			status, res, _ := h.store.InteractionOutcome(context.Background(), ix.ID)
			if status != "dismissed" || res != nil {
				t.Fatalf("must not record a made-up phone approval: %s %+v", status, res)
			}
			found := false
			deadline := time.Now().Add(time.Second)
			for !found && time.Now().Before(deadline) {
				select {
				case b := <-c.out:
					found = strings.Contains(string(b), `"type":"interaction_resolved"`)
				case <-time.After(5 * time.Millisecond):
				}
			}
			if !found {
				t.Fatal("the app never got interaction_resolved")
			}
		})
	}
}

func TestPlanReconcileCrossesAPageAndKeepsUnrelatedApprovals(t *testing.T) {
	h := newTestHub(t)
	at := time.Now()
	items := planItems(at)
	m := planMachine(t, h, func(before int) ([]proto.Item, bool, error) {
		if before == 0 {
			return items[1:], true, nil
		}
		return items[:1], false, nil
	})
	p := &pending{ix: proto.Interaction{ID: "old-plan", MachineID: "machine", SessionID: "session", Kind: proto.IxPlan,
		ToolName: "ExitPlanMode", CreatedAt: at}, done: make(chan proto.Resolution, 1)}
	permission := &pending{ix: proto.Interaction{ID: "permission", MachineID: "machine", SessionID: "session", Kind: proto.IxPermission}}
	question := &pending{ix: proto.Interaction{ID: "question", MachineID: "machine", SessionID: "session", Kind: proto.IxQuestion}}
	h.interactions = map[string]*pending{"old-plan": p, "permission": permission, "question": question}
	h.syncPlanInteractions(m)
	waitPending(t, h, 2)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.interactions["permission"] != permission || h.interactions["question"] != question {
		t.Fatal("finishing a plan must not answer unrelated questions or permissions")
	}
}

func TestAnInFlightPlanReadDoesNotDismissANewerCard(t *testing.T) {
	h := newTestHub(t)
	at := time.Now()
	started, release := make(chan struct{}), make(chan struct{})
	m := planMachine(t, h, func(int) ([]proto.Item, bool, error) {
		close(started)
		<-release
		return planItems(at), false, nil
	})
	old := &pending{ix: proto.Interaction{ID: "plan", Kind: proto.IxPlan, ToolName: "ExitPlanMode", CreatedAt: at}}
	h.interactions["plan"] = old
	done := make(chan struct{})
	go func() { h.checkPlanInteraction(m, old); close(done) }()
	<-started
	newer := &pending{ix: proto.Interaction{ID: "plan", Kind: proto.IxPlan, ToolName: "ExitPlanMode", CreatedAt: at.Add(time.Minute)}}
	h.mu.Lock()
	h.interactions["plan"] = newer
	h.mu.Unlock()
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("read did not finish")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.interactions["plan"] != newer {
		t.Fatal("an old read removed a new pending plan")
	}
}

func TestPlanProbeFailureKeepsTheCardAndIsThrottled(t *testing.T) {
	h := newTestHub(t)
	var calls atomic.Int32
	m := planMachine(t, h, func(int) ([]proto.Item, bool, error) {
		calls.Add(1)
		return nil, false, fmt.Errorf("node read failed")
	})
	p := &pending{ix: proto.Interaction{ID: "plan", MachineID: "machine", SessionID: "session", Kind: proto.IxPlan,
		ToolName: "ExitPlanMode", CreatedAt: time.Now()}, done: make(chan proto.Resolution, 1)}
	h.interactions["plan"] = p
	h.syncPlanInteractions(m)
	deadline := time.Now().Add(time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	for i := 0; i < 20; i++ {
		h.syncPlanInteractions(m)
	}
	if calls.Load() != 1 {
		t.Fatalf("one pending plan read, got %d", calls.Load())
	}
	waitPending(t, h, 1)
	select {
	case r := <-p.done:
		t.Fatalf("a failed read must not release the hook: %+v", r)
	default:
	}
}
