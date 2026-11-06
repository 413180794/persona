package assistant

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func alwaysIx(id, session string, keys ...string) proto.Interaction {
	return proto.Interaction{ID: id, MachineID: "mac", SessionID: session, Kind: proto.IxPermission, ToolName: "Bash",
		ExpiresAt: time.Now().Add(time.Hour), Resumable: true, AlwaysKeys: keys}
}

// ask is a node's interaction.create, answered with r once its card shows; got is what the node hears.
func ask(t *testing.T, s *Service, ix proto.Interaction, r *proto.Resolution) (got proto.Resolution) {
	t.Helper()
	out := make(chan proto.Resolution, 1)
	go func() { res, _ := s.nodeInteraction(context.Background(), ix); out <- res }()
	if r != nil {
		deadline := time.Now().Add(5 * time.Second)
		for s.resolve(ix.ID, *r) != nil {
			if time.Now().After(deadline) {
				t.Fatalf("%s never showed", ix.ID)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	select {
	case got = <-out:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: no answer", ix.ID)
	}
	return got
}

// "Always Allow in Session" on an assistant conversation's card outlasts the agent's process: the manager answers the same card of
// the resumed process itself (#2202), and only that conversation's, only for what it granted.
func TestAssistantAlwaysAllowKept(t *testing.T) {
	ft := newFleetTest(t)
	s := ft.s
	yes, no := true, false
	ask(t, s, alwaysIx("ix_1", "conv", "tool:Bash"), &proto.Resolution{Allow: &yes})
	ft.waitPending(0)
	if _, ok := s.alwaysAllowed(context.Background(), alwaysIx("ix_x", "conv", "tool:Bash")); ok {
		t.Fatal("allowing once was kept as always")
	}
	ask(t, s, alwaysIx("ix_2", "agent"), &proto.Resolution{Allow: &yes, Always: true})
	if _, ok := s.alwaysAllowed(context.Background(), alwaysIx("ix_x", "agent", "tool:Bash")); ok {
		t.Fatal("a card without keys kept a grant")
	}
	ask(t, s, alwaysIx("ix_3", "conv", "tool:Edit"), &proto.Resolution{Allow: &no, Always: true})
	if first := ask(t, s, alwaysIx("ix_4", "conv", "tool:Bash"), &proto.Resolution{Allow: &yes, Always: true}); first.Remembered {
		t.Fatalf("the card's own answer was taken for a kept grant: %+v", first)
	}
	ft.waitPending(0)
	got := ask(t, s, alwaysIx("ix_5", "conv", "tool:Bash"), nil)
	if got.Allow == nil || !*got.Allow || !got.Always || !got.Remembered { // remembered: no card was answered for it
		t.Fatalf("kept grant answered %+v", got)
	}
	if n := len(s.pendingInteractions()); n != 0 {
		t.Fatalf("%d cards shown for a granted tool", n)
	}
	for _, ix := range []proto.Interaction{
		alwaysIx("ix_6", "conv", "tool:Edit"),
		alwaysIx("ix_7", "other", "tool:Bash"),
		alwaysIx("ix_8", "conv", "tool:Bash", "lark:POST /im/v1/messages"),
	} {
		if _, ok := s.alwaysAllowed(context.Background(), ix); ok {
			t.Fatalf("%s allowed without a grant", ix.ID)
		}
	}
}

// A card of the same conversation waiting when another one is answered with "Always Allow in Session" is answered with it.
func TestAssistantAlwaysAnswersWaitingCards(t *testing.T) {
	ft := newFleetTest(t)
	s := ft.s
	waiting := make(chan proto.Resolution, 1)
	go func() {
		r, _ := s.nodeInteraction(context.Background(), alwaysIx("ix_b", "conv", "tool:Bash"))
		waiting <- r
	}()
	go func() { _, _ = s.nodeInteraction(context.Background(), alwaysIx("ix_c", "conv", "tool:Edit")) }()
	ft.waitPending(2)
	yes := true
	ask(t, s, alwaysIx("ix_a", "conv", "tool:Bash"), &proto.Resolution{Allow: &yes, Always: true})
	select {
	case r := <-waiting:
		if r.Allow == nil || !*r.Allow || !r.Remembered {
			t.Fatalf("waiting card got %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiting card for the same tool still waits")
	}
	if p := ft.waitPending(1); p[0].ID != "ix_c" {
		t.Fatalf("pending %+v", p)
	}
}

// The same completion must not push twice: a Stop whose reply was already pushed for, with no prompt since, is the same
// turn end seen again (the status flapping around it). A new prompt, a different reply, or a Stop with no reply
// (nothing to tell it by) is a new one.
func TestStopDuplicate(t *testing.T) {
	ft := newFleetTestNoStore(t)
	s := ft.s
	event := func(sid, name, sig string) {
		ft.event("mac", "hook.event", map[string]any{"session_id": sid, "event": name, "stop_sig": sig})
	}
	for _, step := range []struct {
		name string
		do   func()
		sid  string
		dup  bool
	}{
		{"a session without hooks", func() {}, "plain", false},
		{"the first Stop", func() { event("s1", "Stop", "aaaa") }, "s1", false},
		{"the same Stop again", func() { event("s1", "Stop", "aaaa") }, "s1", true},
		{"another session is its own", func() { event("s2", "Stop", "aaaa") }, "s2", false},
		{"the same reply after a new prompt", func() { event("s1", "UserPromptSubmit", ""); event("s1", "Stop", "aaaa") }, "s1", false},
		{"and again", func() {}, "s1", true},
		{"a different reply", func() { event("s1", "Stop", "bbbb") }, "s1", false},
		{"a Stop with no reply is never held back", func() { event("s1", "Stop", "") }, "s1", false},
		{"nor again", func() {}, "s1", false},
	} {
		step.do()
		if got := s.stopDuplicate("mac", step.sid); got != step.dup {
			t.Errorf("%s: duplicate %v, want %v", step.name, got, step.dup)
		}
	}
}

// A report-only hook (a session the user started, node/globalhooks.go) is not Persona's hooks running in the session:
// it must not hold screen cards back (hasHooks), but its Stop still feeds the duplicate check; a hook of Persona's own
// still counts. The hub still gets the event (it shows it to the apps).
func TestReportOnlyStopDedupesWithoutMarkingHooks(t *testing.T) {
	ft := newFleetTestNoStore(t)
	s := ft.s
	b, _ := json.Marshal(map[string]any{"session_id": "s1", "event": "Stop", "stop_sig": "aaaa", "report_only": true})
	if !s.NodeEvent("mac", "hook.event", b) {
		t.Error("the hub no longer gets the hook event")
	}
	if s.hasHooks("mac", proto.Session{SessionID: "s1"}) {
		t.Error("a report-only event marked the session as having Persona's hooks")
	}
	if s.stopDuplicate("mac", "s1") {
		t.Error("the first push was held back")
	}
	if !s.stopDuplicate("mac", "s1") {
		t.Error("the same reply pushed twice")
	}
	ft.event("mac", "hook.event", map[string]any{"session_id": "s-own", "event": "Stop"})
	if !s.hasHooks("mac", proto.Session{SessionID: "s-own"}) {
		t.Error("a hook event of Persona's own no longer counts")
	}
}
