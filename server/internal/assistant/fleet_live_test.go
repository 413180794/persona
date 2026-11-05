package assistant

import (
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// The Live Activity's content as the manager works it out (fleet_live.go), moved from the hub's tests.

func TestBuildFleet(t *testing.T) {
	t0 := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	agent := func(id, state string, mod func(*proto.Session)) fleetInput {
		s := proto.Session{MachineID: "m", SessionID: id, Title: "agent " + id, State: state, UpdatedAt: t0, SpaceName: "web", SlotName: "2", SlotIndex: 2}
		if mod != nil {
			mod(&s)
		}
		return fleetInput{Machine: "box", Session: s}
	}
	agents := []fleetInput{
		agent("w1", proto.StateWorking, nil),
		agent("w2", proto.StateWorking, nil),
		agent("p", proto.StateNeedsYou, nil),
		agent("u", proto.StateUnread, func(s *proto.Session) { s.LastText = "All tests pass.\nDetails…" }),
		agent("idle", proto.StateIdle, nil),
		agent("off", proto.StateOffline, nil),
		agent("loop", proto.StateUnread, func(s *proto.Session) { s.Loop = &proto.LoopInfo{Kind: "wakeup"} }),
		agent("asst", proto.StateWorking, func(s *proto.Session) { s.Assistant = true }),
	}
	agents[0].TurnStart = t0.Add(-5 * time.Minute) // w1 runs longer
	agents[1].TurnStart = t0.Add(-1 * time.Minute)
	muted := agent("muted", proto.StateWorking, nil)
	muted.Muted = true
	agents = append(agents, muted)
	ixs := []proto.Interaction{{ID: "ix1", MachineID: "m", SessionID: "p", Kind: proto.IxPermission, Summary: "Bash: go test ./...", CreatedAt: t0}}

	st := buildFleet(agents, ixs)
	if st.Phase != "needsYou" || st.NeedsYou != 1 || st.Working != 2 || st.Unread != 1 {
		t.Fatalf("counts %+v", st)
	}
	if len(st.Items) != fleetMaxItems {
		t.Fatalf("items %d", len(st.Items))
	}
	first := st.Items[0]
	if first.ID != "m/p" || first.Kind != proto.IxPermission || first.Interaction != "ix1" || first.Detail != "Bash: go test ./..." {
		t.Fatalf("first %+v", first)
	}
	if first.Place != "box · web" { // slot "2" is just the index: not repeated
		t.Fatalf("place %q", first.Place)
	}
	if st.Items[1].ID != "m/w1" || st.Items[2].ID != "m/w2" {
		t.Fatalf("working order %+v", st.Items)
	}
	if st.WorkingSince != float64(t0.Add(-5*time.Minute).Unix()) {
		t.Fatalf("working since %v", st.WorkingSince)
	}

	// Without the request and the workers: only the finished one, its first line.
	st = buildFleet([]fleetInput{agents[3], agents[4]}, nil)
	if st.Phase != "done" || len(st.Items) != 1 || st.Items[0].Detail != "All tests pass." {
		t.Fatalf("done %+v", st)
	}
	if st := buildFleet([]fleetInput{agents[4], agents[5], agents[6], agents[7], muted}, nil); st.Phase != "" || len(st.Items) != 0 {
		t.Fatalf("should be empty: %+v", st)
	}
	// A blocked agent with no interaction yet counts as a terminal prompt.
	st = buildFleet([]fleetInput{agent("b", proto.StateNeedsYou, nil)}, nil)
	if st.Phase != "needsYou" || st.Items[0].Kind != proto.IxScreen {
		t.Fatalf("screen %+v", st)
	}
	// An interaction on a muted agent is left out.
	if st := buildFleet([]fleetInput{muted}, []proto.Interaction{{ID: "x", MachineID: "m", SessionID: "muted", Kind: proto.IxPlan}}); st.Phase != "" {
		t.Fatalf("muted interaction %+v", st)
	}
}

func TestRound2(t *testing.T) {
	for in, want := range map[int64]int64{7: 7, 99: 99, 987: 990, 12345: 12000, 41614712: 42000000} {
		if got := round2(in); got != want {
			t.Errorf("round2(%d) = %d, want %d", in, got, want)
		}
	}
}

// A working item whose turn ended for its background work says so, and what runs (#2935).
func TestBuildFleetBackground(t *testing.T) {
	run := proto.Workflow{TaskID: "w1", Name: "parity-audit", Running: true, Status: proto.WorkflowRunning, Phase: "Verify",
		AgentsStarted: 185, AgentsDone: 169}
	agents := []fleetInput{
		{Machine: "box", Session: proto.Session{MachineID: "m", SessionID: "wf", State: proto.StateWorking, BackgroundWorking: true,
			Workflows: []proto.Workflow{run}}},
		{Machine: "box", Session: proto.Session{MachineID: "m", SessionID: "sh", State: proto.StateWorking, BackgroundWorking: true,
			Background: []proto.BackgroundTask{{ID: "b1", Kind: "shell"}, {ID: "b2", Kind: "monitor"}}}},
		{Machine: "box", Session: proto.Session{MachineID: "m", SessionID: "busy", State: proto.StateWorking}},
	}
	st := buildFleetIn("zh-Hans", agents, nil)
	got := map[string]FleetItem{}
	for _, it := range st.Items {
		got[it.ID] = it
	}
	if it := got["m/wf"]; !it.Background || it.Detail != "parity-audit · Verify · 169/185" {
		t.Errorf("workflow: %+v", it)
	}
	if it := got["m/sh"]; !it.Background || it.Detail != "后台 2 个任务在跑" {
		t.Errorf("background tasks: %+v", it)
	}
	if it := got["m/busy"]; it.Background || it.Detail != "" {
		t.Errorf("a plain working one: %+v", it)
	}
}

// An agent with two cards up is one agent that needs you, as in the Agents list: one item, its oldest card (#3571).
func TestBuildFleetOneItemPerAgent(t *testing.T) {
	t0 := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	agents := []fleetInput{
		{Machine: "box", Session: proto.Session{MachineID: "m", SessionID: "a", State: proto.StateNeedsYou}},
		{Machine: "box", Session: proto.Session{MachineID: "m", SessionID: "b", State: proto.StateNeedsYou}},
		{Machine: "box", Session: proto.Session{MachineID: "m", SessionID: "w", State: proto.StateWorking}},
	}
	ixs := []proto.Interaction{ // oldest first, as pendingInteractions gives them
		{ID: "a1", MachineID: "m", SessionID: "a", Kind: proto.IxPermission, Summary: "first", CreatedAt: t0},
		{ID: "a2", MachineID: "m", SessionID: "a", Kind: proto.IxPermission, Summary: "second", CreatedAt: t0.Add(time.Second)},
		{ID: "b1", MachineID: "m", SessionID: "b", Kind: proto.IxQuestion, CreatedAt: t0.Add(2 * time.Second)},
		{ID: "a3", MachineID: "m", SessionID: "a", Kind: proto.IxPlan, CreatedAt: t0.Add(3 * time.Second)},
	}
	st := buildFleet(agents, ixs)
	if st.NeedsYou != 2 || st.Working != 1 {
		t.Fatalf("counts %+v", st)
	}
	if len(st.Items) != 3 || st.Items[0].ID != "m/a" || st.Items[0].Interaction != "a1" || st.Items[1].ID != "m/b" || st.Items[2].ID != "m/w" {
		t.Fatalf("items %+v", st.Items)
	}
	// An elicitation is left out before the dedup: the agent's next card still stands for it.
	st = buildFleet(agents[:1], []proto.Interaction{
		{ID: "e1", MachineID: "m", SessionID: "a", Kind: proto.IxElicitation, CreatedAt: t0},
		{ID: "a1", MachineID: "m", SessionID: "a", Kind: proto.IxPermission, CreatedAt: t0.Add(time.Second)},
	})
	if st.NeedsYou != 1 || len(st.Items) != 1 || st.Items[0].Interaction != "a1" {
		t.Fatalf("after an elicitation %+v", st.Items)
	}
}
