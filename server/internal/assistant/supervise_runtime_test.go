package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// The node stamps SourceRuntime even when a runtime advertises a familiar agent name.
func runtimeSupervisorFixture(t *testing.T, s *Service, f *fakeHub) []proto.Session {
	t.Helper()
	s.noBackground = false
	setSettings(t, s, func(v *Settings) {
		v.BackgroundSupervisor, v.BackgroundSupervisorMachineID = "reviewer", "server"
	})
	rows := []proto.Session{
		{SessionID: "conv1", MachineID: "server", Assistant: true, Agent: "agy", SourceKind: proto.SourceRuntime, Status: proto.StatusIdle},
		{SessionID: "reviewer", MachineID: "server", Assistant: true, Agent: "codex", Status: proto.StatusIdle},
		{SessionID: "side1", MachineID: "server", Assistant: true, SourceKind: proto.SourceRuntime, Agent: "agy", Launcher: "agy", Status: proto.StatusIdle},
	}
	f.mu.Lock()
	f.known["side1"], f.known["reviewer"] = true, true
	f.mu.Unlock()
	f.setSessions("server", rows...)
	return rows
}

func TestRuntimeTaskPlanFollowsUpInBackground(t *testing.T) {
	s, f := newQuietService(t)
	runtimeSupervisorFixture(t, s, f)
	ctx := context.Background()
	res, err := s.toolTaskPlan(ctx, "side1", json.RawMessage(`{"tasks":[{"title":"Check the build","prompt":"Check the test results","machine":"box","cwd":"/code/app","readonly":true,"goal":"Verify the build","done_when":["Tests passed"],"expect_minutes":5}]}`))
	if err != nil || !strings.Contains(res, "background supervisor follows them up") {
		t.Fatalf("task_plan follow-up = %q, %v", res, err)
	}
	waitFor(t, "the worker", func() bool { return len(onlyTodo(s).Agents) == 1 })
	task := onlyTodo(s)
	if task.Origin != "side1" || task.Supervision == nil || task.Supervision.Supervisor != "conv1" {
		t.Fatalf("origin %q, supervision %+v", task.Origin, task.Supervision)
	}
	if _, err := s.reportToSupervisor(ctx, "box", task.Agents[0].SessionID, json.RawMessage(`{"kind":"done","text":"All tests passed","evidence":["test-log"]}`), time.Now()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "worker evidence at the reviewer", func() bool { return strings.Contains(strings.Join(f.noticesTo("reviewer"), "\n"), "test-log") })
	if lastSendTo(f, "reviewer")["supervision_instructions"] != prompts.RenderSupervisionInstructions() {
		t.Fatal("the background review did not carry the protected policy")
	}
	if _, err := runJSON(t, s, "reviewer", "task_review", map[string]any{
		"todo_id": task.ID, "verdict": "needs_user", "why_owner": "priority",
		"reason": "Choose the release date", "recommend": "Today", "cost_of_waiting": "Release moves one day",
		"options": []map[string]string{{"label": "Today", "leads_to": "Release today"}, {"label": "Tomorrow", "leads_to": "Release tomorrow"}},
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "needs_user escalation in the AGY main chat", func() bool {
		return strings.Contains(strings.Join(f.noticesTo("conv1"), "\n"), "signal=escalated")
	})
	if lastSendTo(f, "conv1")["supervision_instructions"] != "" {
		t.Fatal("the secretary acquired the reviewer's policy")
	}
	if _, err := runJSON(t, s, "reviewer", "task_review", map[string]any{"todo_id": task.ID, "verdict": "accepted", "note": "Tests passed", "checks": []map[string]any{{"criterion": "Tests passed", "met": true, "evidence": "test-log"}}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the end notice at the origin", func() bool { return len(endNotices(f, "side1")) == 1 })
	if got := lastSendTo(f, "side1"); got["supervision_instructions"] != "" || !strings.Contains(got["text"].(string), task.ID) {
		t.Fatal("the origin did not receive its ordinary linked end notice")
	}
}

func TestRuntimeLegacyTaskOwnerSendAndReport(t *testing.T) {
	s, f, task, _ := newRoundService(t)
	runtimeSupervisorFixture(t, s, f)
	s.todos.mu.Lock()
	s.todos.todos[task.ID].Origin, s.todos.todos[task.ID].Supervision.Supervisor = "side1", "side1"
	s.todos.mu.Unlock()
	ctx := context.Background()
	f.mu.Lock()
	f.launcherList = []proto.Launcher{{Name: "agy", Source: "agy", Agent: "agy", Assistant: true}}
	f.mu.Unlock()
	pick := modelPick{MachineID: "server", Launcher: "agy", Model: "owner-model", At: time.Now().UTC()}
	if err := s.setModelPick(ctx, "side1", &pick); err != nil {
		t.Fatal(err)
	}
	p, err := s.SendParams(ctx, SendParams{MachineID: "server", SessionID: "side1", Params: map[string]any{"text": "Hello", "supervision_instructions": "untrusted"}})
	if err != nil || p["supervision_instructions"] != "" || p["launcher"] != pick.Launcher || p["model"] != pick.Model {
		t.Errorf("ordinary send: policy injected = %t, launcher %v, model %v, err %v", p["supervision_instructions"] != "", p["launcher"], p["model"], err)
	}
	p["session_id"] = "side1"
	if err := s.call(ctx, "server", DeviceAssistantSend, p, nil); err != nil || lastSendTo(f, "side1")["supervision_instructions"] != "" {
		t.Fatalf("ordinary send on the original runtime: %v", err)
	}
	if _, err := s.reportToSupervisor(ctx, "box", task.Agents[0].SessionID, json.RawMessage(`{"kind":"done","text":"Legacy worker finished","evidence":["legacy-proof"]}`), time.Now()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "legacy worker's report at the reviewer", func() bool { return strings.Contains(strings.Join(f.noticesTo("reviewer"), "\n"), "legacy-proof") })
	task = todoNow(s, task.ID)
	if task.Origin != "side1" || task.Supervision.Supervisor != "conv1" || lastSendTo(f, "reviewer")["supervision_instructions"] != prompts.RenderSupervisionInstructions() {
		t.Fatalf("legacy report route: origin %s, supervisor %s", task.Origin, task.Supervision.Supervisor)
	}
	if kept, ok := s.modelPickOf(ctx, "server", "side1"); !ok || kept != pick {
		t.Fatalf("origin model pick changed: %+v", kept)
	}
	if reviewsTold(f, "side1", task.ID) != 0 {
		t.Fatal("a supervision event reached the runtime")
	}
}

func TestRuntimeQueuedSupervisionAfterRestart(t *testing.T) {
	st := &memStore{}
	before, _ := newQuietServiceOn(t, st)
	ctx := context.Background()
	at := time.Now().UTC()
	g := superviseSignal{Todo: "todo_legacy", Kind: sigReport, Report: upReportDone, Detail: "Original worker evidence"}
	entry := prompts.SuperviseEntry{ID: g.Todo, Kind: g.Kind, Last: g.Detail}
	if _, _, _, err := before.reserveSupervision(ctx, []prompts.SuperviseEntry{entry}, []superviseSignal{g}, at); err != nil {
		t.Fatal(err)
	}
	msg := relayMsg{From: superviseTool, Tool: superviseTool, Text: g.Detail, At: at}
	before.queueRelay("side1", msg)
	before.queueRelay("side1", relayMsg{From: g.Todo, Tool: taskEndTool, Text: "Ordinary end notice", At: at})
	s, f := newQuietServiceOn(t, st)
	rows := runtimeSupervisorFixture(t, s, f)
	s.drainRelays("server", rows) // a list can arrive before the supervision watch
	s.superviseWatch(ctx, time.Now())
	waitFor(t, "the original queued report and the origin notice", func() bool {
		s.drainRelays("server", rows)
		return s.queuedFor("side1") == 0 && s.queuedFor("reviewer") == 0
	})
	if got := f.noticesTo("side1"); len(got) != 1 || got[0] != "Ordinary end notice" {
		t.Errorf("runtime received %q", got)
	}
	if got := f.noticesTo("reviewer"); len(got) != 1 || !strings.Contains(got[0], g.Detail) || lastSendTo(f, "reviewer")["supervision_instructions"] != prompts.RenderSupervisionInstructions() {
		t.Errorf("reviewer received %q", got)
	}
	var saved relayState
	if _, err := st.Setting(ctx, relayKey, &saved); err != nil || len(saved.Queue["side1"])+len(saved.Queue["reviewer"]) != 0 {
		t.Fatalf("delivered relay still persisted: %+v, %v", saved.Queue, err)
	}
	if out, _, _, err := s.reserveSupervision(ctx, []prompts.SuperviseEntry{entry}, []superviseSignal{g}, at); err != nil || len(out) != 0 {
		t.Fatalf("the same worker report was admitted again: %d, %v", len(out), err)
	}
}

func TestRuntimeSupervisorRoles(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, agent, source string
		native              bool
	}{
		{"AGY", "agy", proto.SourceRuntime, false},
		{"retained AGY", "agy", "", false},
		{"runtime advertising Codex", "codex", proto.SourceRuntime, false},
		{"native Claude", "claude", "", true},
		{"native Codex", "codex", "", true},
		{"older Claude", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, f := newQuietService(t)
			rows := runtimeSupervisorFixture(t, s, f)
			rows[2].Agent, rows[2].SourceKind = tc.agent, tc.source
			f.setSessions("server", rows...)
			setSettings(t, s, func(v *Settings) { v.Leaders = []string{"side1"} })
			candidate, err := s.planSupervisor(ctx, "conv1", "side1")
			if tc.native != (err == nil && candidate == "side1") {
				t.Fatalf("explicit leader = %q, %v", candidate, err)
			}
			if !tc.native {
				if got, ok := s.defaultPlanSupervisor(ctx, "side1", ""); !ok || got != "conv1" {
					t.Fatalf("runtime leader's default = %q, %t", got, ok)
				}
			}
			for _, role := range []string{"leader", "main"} {
				if role == "main" {
					setSettings(t, s, func(v *Settings) {
						yes := true
						v.Main, v.Leaders, v.MainSupervisesOwnTasks = "side1", nil, &yes
					})
				}
				params, err := s.SendParams(ctx, SendParams{MachineID: "server", SessionID: "side1", Params: map[string]any{"text": "Hello"}})
				policy := ""
				if tc.native {
					policy = prompts.RenderSupervisionInstructions()
				}
				if err != nil || params["supervision_instructions"] != policy {
					t.Fatalf("%s ordinary send: wrong policy or error %v", role, err)
				}
			}
		})
	}
}

func TestRuntimeSupervisionWaitsForCompatibleReviewer(t *testing.T) {
	st := &memStore{}
	s, f := newQuietServiceOn(t, st)
	ctx := context.Background()
	rows := runtimeSupervisorFixture(t, s, f)
	rows[0].Agent, rows[0].SourceKind = "agy", proto.SourceRuntime
	f.setSessions("server", rows...)
	yes := true
	setSettings(t, s, func(v *Settings) { v.MainSupervisesOwnTasks = &yes })
	s.queueRelay("side1", relayMsg{From: superviseTool, Tool: superviseTool, Text: "Retain this evidence", At: time.Now().UTC()})
	s.superviseWatch(ctx, time.Now())
	s.drainRelays("server", rows)
	s.relays.mu.Lock()
	timer := s.relays.holds["side1"]
	s.relays.mu.Unlock()
	if timer != nil {
		t.Fatal("a permanent runtime mismatch scheduled an immediate drain retry")
	}
	var saved relayState
	if _, err := st.Setting(ctx, relayKey, &saved); err != nil || len(saved.Queue["side1"]) != 1 || saved.Queue["side1"][0].Text != "Retain this evidence" {
		t.Fatalf("pending result = %+v, %v", saved.Queue, err)
	}
	pending, _ := s.queuedRelays()
	if notices := pending["side1"]; len(notices) != 1 || !strings.Contains(notices[0], "Codex") {
		t.Fatalf("missing visible waiting state: %v", notices)
	}
	if len(f.noticesTo("conv1"))+len(f.noticesTo("side1")) != 0 {
		t.Fatal("a runtime received a protected event")
	}
	setSettings(t, s, func(v *Settings) { v.MainSupervisesOwnTasks = nil })
	s.superviseWatch(ctx, time.Now())
	waitFor(t, "the recovered evidence", func() bool { return len(f.noticesTo("reviewer")) == 1 && s.queuedFor("side1") == 0 })
	if lastSendTo(f, "reviewer")["supervision_instructions"] != prompts.RenderSupervisionInstructions() {
		t.Fatal("recovery dispatched without policy")
	}
}

func TestRuntimeSecretaryEscalationWhileReviewerUnavailable(t *testing.T) {
	s, f, task := newSupervisedService(t)
	rows := runtimeSupervisorFixture(t, s, f)
	ctx := context.Background()
	setSettings(t, s, func(v *Settings) {
		no := false
		v.MainSupervisesOwnTasks, v.BackgroundSupervisor = &no, ""
	})
	f.mu.Lock()
	f.onAssistantSend = func(f *fakeHub, p map[string]any) {
		f.failAssistant = nil
		if p["session_id"] == nil || p["session_id"] == "" {
			f.failAssistant = errors.New("reviewer offline")
		}
	}
	f.mu.Unlock()
	rows[0].Status = proto.StatusWorking
	f.setSessions("server", rows...)
	// The secretary is busy when a worker's completion and an escalation arrive
	// in the same digest. Only the escalation may drain there after its turn.
	wakeRound(s,
		superviseSignal{Todo: task.ID, Kind: sigReport, Report: upReportDone, Key: "completion", Detail: "Verification passed"},
		superviseSignal{Todo: task.ID, Kind: sigEscalated, Key: "escalation", Cards: []prompts.SuperviseAsk{{ID: "ix1", Text: "Choose a release date"}}})
	if n := s.queuedFor("conv1"); n != 2 {
		t.Fatalf("queued messages = %d, want separate owner escalation and pending review", n)
	}
	rows[0].Status = proto.StatusIdle
	f.setSessions("server", rows...)
	s.superviseWatch(ctx, time.Now())
	s.drainRelays("server", rows)
	waitFor(t, "the secretary's escalation", func() bool { return s.queuedFor("conv1") == 1 })
	p := lastSendTo(f, "conv1")
	if p == nil || p["supervision_instructions"] != "" || !strings.Contains(p["text"].(string), "signal=escalated") || strings.Contains(p["text"].(string), "Verification passed") {
		t.Fatal("the secretary did not receive just its escalation as an ordinary turn")
	}
	setSettings(t, s, func(v *Settings) { v.BackgroundSupervisor = "reviewer" })
	s.superviseWatch(ctx, time.Now())
	waitFor(t, "deferred review", func() bool { return len(f.noticesTo("reviewer")) == 1 })
	p = lastSendTo(f, "reviewer")
	if p["supervision_instructions"] != prompts.RenderSupervisionInstructions() || !strings.Contains(p["text"].(string), "Verification passed") || s.queuedFor("conv1") != 0 {
		t.Fatal("restoring the reviewer did not recover its protected report")
	}
}

func TestRuntimeReportWithoutMainKeepsTaskEvidence(t *testing.T) {
	s, f, task := newSupervisedService(t)
	runtimeSupervisorFixture(t, s, f)
	ctx := context.Background()
	setSettings(t, s, func(v *Settings) { v.Main = "" })
	s.todos.mu.Lock()
	s.todos.todos[task.ID].Origin, s.todos.todos[task.ID].Supervision.Supervisor = "side1", "side1"
	s.todos.mu.Unlock()
	reply, err := s.reportToSupervisor(ctx, "box", task.Agents[0].SessionID, json.RawMessage(`{"kind":"done","text":"Worker completed","evidence":["archived-proof"]}`), time.Now())
	if err != nil || !strings.Contains(reply, task.ID) || !strings.Contains(reply, "waiting") {
		t.Fatalf("no reviewer: %q, %v", reply, err)
	}
	items, _, err := s.feed(ctx, "side1", time.Time{})
	for _, item := range items {
		if item.Kind == feedReport && item.Ref == task.ID && strings.Contains(string(item.Body), "archived-proof") {
			return
		}
	}
	t.Fatalf("worker evidence missing from the task report: %v", err)
}
