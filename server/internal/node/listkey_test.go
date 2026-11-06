package node

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// listKeyFmt is listKey as it was written with fmt.Fprintf (before #3207), kept to check that the faster one
// changes exactly when it did.
func listKeyFmt(list []proto.Session) string {
	var b strings.Builder
	for _, s := range list {
		fmt.Fprintf(&b, "%s|%s|%s|%s|%s|%s|%s|%s|%d|%q|%s|%s\n", s.SessionID, s.Status, s.Title, s.PaneID, s.SpaceName, s.ParentSpaceID, s.SlotName, s.LastText, s.UpdatedAt.Unix()/10, s.Queued, s.Suggestion, s.SpawnFrom)
		fmt.Fprintf(&b, "hierarchy:%q/%q/%q/%v/%q/%q/%q\n", s.ParentThreadID, s.ParentSessionID, s.RootSessionID, s.IsSubagent, s.RepoRoot, s.SpaceID, s.SpaceKind)
		if s.Activity != "" {
			fmt.Fprintf(&b, "activity:%s/%d\n", s.Activity, unixOf(s.ActivityAt))
		}
		fmt.Fprintf(&b, "attention:%q/%q/%s\n", s.AttentionReason, s.FailureReason, s.ExternalURL)
		if s.Chat != "" || len(s.Dispatched) > 0 {
			fmt.Fprintf(&b, "botmux:%s/%s/%q\n", s.Chat, s.Topic, s.Dispatched)
		}
		pane := "none"
		if s.PaneIndex != nil {
			pane = fmt.Sprint(*s.PaneIndex)
		}
		fmt.Fprintf(&b, "pane:%s/%d/%s\n", s.SlotID, s.SlotIndex, pane)
		if c := s.Compaction; c != nil {
			fmt.Fprintf(&b, "compaction:%s/%d\n", c.State, c.StartedAt.Unix())
		}
		if len(s.Efforts) > 0 {
			fmt.Fprintf(&b, "effort:%s/%v\n", s.Effort, s.Efforts)
		}
		fmt.Fprintf(&b, "source:%s/%s/%s/%s/%v/%v/%s/%s\n", s.SourceKind, s.SourceThreadID, s.SourceInstanceID, s.ReadOnlyReason, s.Capabilities, s.Attached, s.CodexRoute, s.CodexRouteNote)
		for _, t := range s.Background {
			b.WriteString(t.ID + ",")
		}
		for _, w := range s.Workflows {
			fmt.Fprintf(&b, "workflow:%s/%s/%q/%d/%d/%d\n", w.TaskID, w.Status, w.Phase, w.AgentsDone, w.AgentsStarted, w.AgentsRunning)
		}
		if s.Loop != nil {
			fmt.Fprintf(&b, "loop%d", s.Loop.NextAt.Unix())
		}
		if g := s.Goal; g != nil {
			fmt.Fprintf(&b, "goal:%s/%q/%q\n", g.Status, g.Objective, g.Reason)
		}
		if p := s.Plan; p != nil {
			fmt.Fprintf(&b, "plan:%d/%d/%q\n", p.Done, p.Total, p.Current)
		}
		for _, a := range s.Subagents {
			fmt.Fprintf(&b, "%s:%v:%d,", a.ID, a.Running, a.UpdatedAt.Unix()/10)
		}
		for _, p := range s.Ports {
			fmt.Fprintf(&b, "port%d/%d/%s,", p.Port, p.PID, p.Title)
		}
	}
	return b.String()
}

// listKeyFixture is n sessions; every other one is busy (background tasks, subagents, ports, an activity, a pane).
func listKeyFixture(n int) []proto.Session {
	at := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	list := make([]proto.Session, n)
	for i := range list {
		s := proto.Session{SessionID: fmt.Sprintf("s%02d-0f3c9a1e-7b2d-4c5e-9a8b", i), Status: proto.StatusIdle,
			Title: fmt.Sprintf("修 listKey 的分配 %d", i), PaneID: fmt.Sprintf("%%%d", i), SpaceName: "persona",
			SlotID: "w1:t1", SlotName: "server", SlotIndex: i % 4, LastText: "已建 issue 并认领，接着看 sessions.go 的 publish",
			UpdatedAt: at.Add(time.Duration(i) * time.Minute), SourceKind: "claude"}
		if i%2 == 1 {
			pane := i % 3
			act := at.Add(time.Duration(i) * time.Second)
			s.Status = proto.StatusWorking
			s.PaneIndex = &pane
			s.Activity, s.ActivityAt = "在跑测试", &act
			s.Background = []proto.BackgroundTask{{ID: "b85polnoj"}, {ID: "b85polnok"}}
			s.Workflows = []proto.Workflow{{TaskID: "w1", Status: proto.WorkflowRunning, Phase: "Review", AgentsStarted: 3, AgentsDone: 1, AgentsRunning: 2}}
			s.Subagents = []proto.Subagent{{ID: "agent-a1", Running: true, UpdatedAt: at}, {ID: "agent-a2", UpdatedAt: at}}
			s.Ports = []proto.Port{{Port: 5173, PID: 4242, Title: "Vite App"}}
		}
		list[i] = s
	}
	return list
}

// listKeyVariants returns a session and copies of it that each differ from it in one field the key covers (or in
// one that churns without meaning, which must not count).
func listKeyVariants() (proto.Session, map[string]proto.Session) {
	at := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	act := at.Add(2 * time.Second)
	pane := 1
	base := proto.Session{SessionID: "s1", Status: proto.StatusWorking, Title: "t", PaneID: "%1", SpaceName: "sp",
		ParentSpaceID: "p", SlotID: "w1:t1", SlotName: "server", SlotIndex: 2, PaneIndex: &pane, LastText: "先看代码",
		UpdatedAt: at, Queued: []string{"再跑一次"}, Suggestion: "继续", SpawnFrom: "todo_1@mac/parent",
		Activity: "在读 tasks.go", ActivityAt: &act, AttentionReason: "等你", FailureReason: "失败", ExternalURL: "https://example.com/t",
		Chat: "oc_1", Topic: "om_1", Dispatched: []string{"om_2"},
		Compaction: &proto.Compaction{State: "running", StartedAt: at},
		Effort:     "high", Efforts: []string{"low", "high"},
		SourceKind: "codex", SourceThreadID: "th", SourceInstanceID: "in", ReadOnlyReason: "ro",
		Capabilities: &proto.SessionCapabilities{}, Attached: true, CodexRoute: "app_server", CodexRouteNote: "n",
		Background: []proto.BackgroundTask{{ID: "b1"}}, Loop: &proto.LoopInfo{NextAt: at},
		Goal:      &proto.GoalInfo{Status: "active", Objective: "o", Reason: "r"},
		Plan:      &proto.PlanInfo{Done: 3, Total: 5, Current: "Reproducing the crash", At: at},
		Subagents: []proto.Subagent{{ID: "a1", Running: true, UpdatedAt: at}},
		Ports:     []proto.Port{{Port: 3000, PID: 7, Title: "dev"}}}
	v := map[string]func(*proto.Session){
		"parent_thread_id":  func(s *proto.Session) { s.ParentThreadID = "parent" },
		"parent_session_id": func(s *proto.Session) { s.ParentSessionID = "parent-public" },
		"root_session_id":   func(s *proto.Session) { s.RootSessionID = "root-public" },
		"is_subagent":       func(s *proto.Session) { s.IsSubagent = true },
		"repo_root":         func(s *proto.Session) { s.RepoRoot = "/work/project" },
		"space_id":          func(s *proto.Session) { s.SpaceID = "project-id" },
		"space_kind":        func(s *proto.Session) { s.SpaceKind = "codex_app" },

		"session_id":    func(s *proto.Session) { s.SessionID = "s2" },
		"status":        func(s *proto.Session) { s.Status = proto.StatusDone },
		"title":         func(s *proto.Session) { s.Title = "t2" },
		"pane_id":       func(s *proto.Session) { s.PaneID = "%2" },
		"space":         func(s *proto.Session) { s.SpaceName = "sp2" },
		"parent_space":  func(s *proto.Session) { s.ParentSpaceID = "" },
		"slot_name":     func(s *proto.Session) { s.SlotName = "web" },
		"last_text":     func(s *proto.Session) { s.LastText = "跑测试" },
		"updated_10s":   func(s *proto.Session) { s.UpdatedAt = at.Add(11 * time.Second) },
		"updated_churn": func(s *proto.Session) { s.UpdatedAt = at.Add(4 * time.Second) },
		"queued":        func(s *proto.Session) { s.Queued = append(s.Queued, "x") },
		"queued_none":   func(s *proto.Session) { s.Queued = nil },
		"queued_empty":  func(s *proto.Session) { s.Queued = []string{""} },
		"suggestion":    func(s *proto.Session) { s.Suggestion = "" },
		"spawn_from":    func(s *proto.Session) { s.SpawnFrom = "todo_2@mac/parent" },
		"activity":      func(s *proto.Session) { s.Activity = "在跑测试" },
		"activity_none": func(s *proto.Session) { s.Activity = "" },
		"activity_at":   func(s *proto.Session) { later := act.Add(3 * time.Second); s.ActivityAt = &later },
		"activity_nil":  func(s *proto.Session) { s.ActivityAt = nil },
		"attention":     func(s *proto.Session) { s.AttentionReason = "等你\"确认\"" },
		"failure":       func(s *proto.Session) { s.FailureReason = "" },
		"external_url":  func(s *proto.Session) { s.ExternalURL = "https://example.com/u" },
		"chat":          func(s *proto.Session) { s.Chat = "oc_2" },
		"topic":         func(s *proto.Session) { s.Topic = "om_9" },
		"dispatched":    func(s *proto.Session) { s.Dispatched = []string{"om_2", "om_3"} },
		"botmux_none":   func(s *proto.Session) { s.Chat, s.Dispatched = "", nil },
		"slot_id":       func(s *proto.Session) { s.SlotID = "w1:t2" },
		"slot_index":    func(s *proto.Session) { s.SlotIndex = 0 },
		"pane_index":    func(s *proto.Session) { zero := 0; s.PaneIndex = &zero },
		"pane_none":     func(s *proto.Session) { s.PaneIndex = nil },
		"pane_same":     func(s *proto.Session) { one := 1; s.PaneIndex = &one },
		"compaction":    func(s *proto.Session) { s.Compaction = &proto.Compaction{State: "done", StartedAt: at} },
		"compact_at":    func(s *proto.Session) { s.Compaction = &proto.Compaction{State: "running", StartedAt: act} },
		"compact_none":  func(s *proto.Session) { s.Compaction = nil },
		"effort":        func(s *proto.Session) { s.Effort = "low" },
		"efforts":       func(s *proto.Session) { s.Efforts = []string{"low", "medium", "high"} },
		"efforts_none":  func(s *proto.Session) { s.Efforts = nil },
		"source_kind":   func(s *proto.Session) { s.SourceKind = "claude" },
		"source_thread": func(s *proto.Session) { s.SourceThreadID = "th2" },
		"source_inst":   func(s *proto.Session) { s.SourceInstanceID = "in2" },
		"read_only":     func(s *proto.Session) { s.ReadOnlyReason = "" },
		"caps":          func(s *proto.Session) { s.Capabilities = &proto.SessionCapabilities{Send: true} },
		"caps_nil":      func(s *proto.Session) { s.Capabilities = nil },
		"attached":      func(s *proto.Session) { s.Attached = false },
		"codex_route":   func(s *proto.Session) { s.CodexRoute = "terminal" },
		"route_note":    func(s *proto.Session) { s.CodexRouteNote = "" },
		"background":    func(s *proto.Session) { s.Background = append(s.Background, proto.BackgroundTask{ID: "b2"}) },
		"bg_none":       func(s *proto.Session) { s.Background = nil },
		"loop":          func(s *proto.Session) { s.Loop = &proto.LoopInfo{NextAt: act} },
		"loop_none":     func(s *proto.Session) { s.Loop = nil },
		"goal_status":   func(s *proto.Session) { s.Goal = &proto.GoalInfo{Status: "complete", Objective: "o", Reason: "r"} },
		"goal_reason":   func(s *proto.Session) { s.Goal = &proto.GoalInfo{Status: "active", Objective: "o", Reason: "r\n2"} },
		"goal_none":     func(s *proto.Session) { s.Goal = nil },
		"plan_done": func(s *proto.Session) {
			s.Plan = &proto.PlanInfo{Done: 4, Total: 5, Current: "Reproducing the crash", At: at}
		},
		"plan_total": func(s *proto.Session) {
			s.Plan = &proto.PlanInfo{Done: 3, Total: 6, Current: "Reproducing the crash", At: at}
		},
		"plan_current": func(s *proto.Session) { s.Plan = &proto.PlanInfo{Done: 3, Total: 5, Current: "Fixing it", At: at} },
		"plan_at_churn": func(s *proto.Session) {
			s.Plan = &proto.PlanInfo{Done: 3, Total: 5, Current: "Reproducing the crash", At: act}
		},
		"plan_none":    func(s *proto.Session) { s.Plan = nil },
		"subagent_run": func(s *proto.Session) { s.Subagents = []proto.Subagent{{ID: "a1", UpdatedAt: at}} },
		"subagent_at": func(s *proto.Session) {
			s.Subagents = []proto.Subagent{{ID: "a1", Running: true, UpdatedAt: act.Add(9 * time.Second)}}
		},
		"subagent_new": func(s *proto.Session) { s.Subagents = append(s.Subagents, proto.Subagent{ID: "a2"}) },
		"port":         func(s *proto.Session) { s.Ports = []proto.Port{{Port: 3001, PID: 7, Title: "dev"}} },
		"port_pid":     func(s *proto.Session) { s.Ports = []proto.Port{{Port: 3000, PID: 8, Title: "dev"}} },
		"port_title":   func(s *proto.Session) { s.Ports = []proto.Port{{Port: 3000, PID: 7, Title: "Vite"}} },
		"ports_none":   func(s *proto.Session) { s.Ports = nil },
	}
	out := make(map[string]proto.Session, len(v))
	for name, f := range v {
		s := base
		s.Queued = append([]string(nil), base.Queued...)
		s.Background = append([]proto.BackgroundTask(nil), base.Background...)
		s.Subagents = append([]proto.Subagent(nil), base.Subagents...)
		f(&s)
		out[name] = s
	}
	return base, out
}

// The key changes exactly when the fmt one did: for every one-field change (and every no-op churn), alone and in a
// list of other sessions, both agree on changed / unchanged. It is also the same text, which keeps that easy to see.
func TestListKeyAgreesWithFmt(t *testing.T) {
	base, variants := listKeyVariants()
	others := listKeyFixture(6)
	in := func(s proto.Session) []proto.Session {
		return append(append(append([]proto.Session(nil), others[:3]...), s), others[3:]...)
	}
	for _, list := range [][]proto.Session{{base}, in(base), others, nil, {{}}} {
		if got, want := listKey(list), listKeyFmt(list); got != want {
			t.Fatalf("listKey differs from the fmt key:\n got %q\nwant %q", got, want)
		}
	}
	for name, v := range variants {
		for _, pair := range [][2][]proto.Session{{{base}, {v}}, {in(base), in(v)}} {
			was := listKeyFmt(pair[0]) != listKeyFmt(pair[1])
			now := listKey(pair[0]) != listKey(pair[1])
			if was != now {
				t.Errorf("%s: fmt key changed=%v, listKey changed=%v", name, was, now)
			}
		}
	}
	// The fixture covers both: changes the key must see and churn it must not.
	if listKeyFmt([]proto.Session{base}) == listKeyFmt([]proto.Session{variants["status"]}) ||
		listKeyFmt([]proto.Session{base}) != listKeyFmt([]proto.Session{variants["updated_churn"]}) {
		t.Fatal("fixture does not exercise both outcomes")
	}
}

func TestListKeyWorkflowProgress(t *testing.T) {
	base := proto.Workflow{TaskID: "w1", Status: proto.WorkflowRunning, Phase: "Review", AgentsStarted: 3, AgentsDone: 1, AgentsRunning: 2}
	key := func(runs ...proto.Workflow) string {
		return listKey([]proto.Session{{SessionID: "s1", Workflows: runs}})
	}
	want := key(base)
	for name, change := range map[string]func(*proto.Workflow){
		"task_id":        func(w *proto.Workflow) { w.TaskID = "w2" },
		"status":         func(w *proto.Workflow) { w.Status = proto.WorkflowCompleted },
		"phase":          func(w *proto.Workflow) { w.Phase = "Verify" },
		"agents_done":    func(w *proto.Workflow) { w.AgentsDone++ },
		"agents_started": func(w *proto.Workflow) { w.AgentsStarted++ },
		"agents_running": func(w *proto.Workflow) { w.AgentsRunning-- },
	} {
		t.Run(name, func(t *testing.T) {
			changed := base
			change(&changed)
			if key(changed) == want {
				t.Fatalf("workflow %s change does not change the list key", name)
			}
		})
	}
	if key() == want || key(base, base) == want {
		t.Fatal("adding or removing a workflow does not change the list key")
	}
	base.UpdatedAt = time.Now()
	if key(base) != want {
		t.Fatal("workflow timestamp alone changes the list key")
	}
}

// 40 sessions, half of them busy: what publish keys every second (grown to the last key's length, as publish does).
func BenchmarkListKey(b *testing.B) {
	list := listKeyFixture(40)
	size := len(listKey(list))
	b.ReportAllocs()
	for b.Loop() {
		_ = listKeySized(list, size)
	}
}
