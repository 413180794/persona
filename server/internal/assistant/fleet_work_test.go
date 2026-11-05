package assistant

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// The work line of an agent waiting on the user reads in the language asked for, Chinese when none was said.
func TestInteractionLineLanguages(t *testing.T) {
	plan := proto.Interaction{Kind: proto.IxPlan}
	perm := proto.Interaction{Kind: proto.IxPermission, Summary: "Bash: go test"}
	for _, c := range []struct {
		l    i18n.Lang
		ix   proto.Interaction
		want string
	}{
		{"", plan, "计划等你审批"},
		{i18n.ZhHans, perm, "等你授权：Bash: go test"},
		{i18n.En, plan, "Plan waiting for approval"},
		{i18n.En, perm, "Waiting for approval: Bash: go test"},
		{i18n.Ja, proto.Interaction{}, "ターミナルが対応待ち"},
	} {
		if got := interactionLine(c.l, c.ix); got != c.want {
			t.Errorf("%q %s: %q, want %q", c.l, c.ix.Kind, got, c.want)
		}
	}
}

func TestWorkState(t *testing.T) {
	done := &proto.WorkState{Phase: proto.WorkDone}
	wake := time.Now()
	ext := &proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitExternal, Wait: "部署", WakeAt: &wake}
	unwoken := &proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitExternal, Wait: "部署"}
	background := &proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitExternal, Line: "后台 1 个任务在跑"}
	user := &proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitUser}
	for _, c := range []struct {
		state string
		w     *proto.WorkState
		want  string
	}{
		{proto.StateUnread, done, proto.StateIdle},
		{proto.StateUnread, ext, proto.StateIdle},
		{proto.StateUnread, user, proto.StateUnread},
		{proto.StateUnread, unwoken, proto.StateUnread},
		{proto.StateUnread, background, proto.StateIdle},
		{proto.StateUnread, nil, proto.StateUnread},
		{proto.StateNeedsYou, done, proto.StateNeedsYou},
		{proto.StateWorking, done, proto.StateWorking},
	} {
		if got := workState(c.state, c.w); got != c.want {
			t.Errorf("workState(%s, %+v) = %s, want %s", c.state, c.w, got, c.want)
		}
	}
}

func listed(t *testing.T, s *Service, machine, sid string) proto.Session {
	t.Helper()
	sess, ok := s.decoratedSession(machine, sid)
	if !ok {
		t.Fatalf("no session %s", sid)
	}
	return sess
}

func workReport(on bool, ws map[string]proto.WorkState) *WorkReport {
	r := &WorkReport{On: on, Sessions: []assistantwire.SessionWork{}}
	for sid, w := range ws {
		r.Sessions = append(r.Sessions, assistantwire.SessionWork{MachineID: "mac", SessionID: sid, Work: w})
	}
	return r
}

// The lists show what the Agents business reported for a turn while the session is still stopped on it; what it
// shows while it works or asks on its own card; the triage off, nothing; a report it could not read changes nothing.
// A report that changes a machine's sessions has the hub told their list.
func TestWorkOnTheListsIsTheReport(t *testing.T) {
	ft := newFleetTestNoStore(t)
	s := ft.s
	working := proto.Session{SessionID: "s3", Status: proto.StatusWorking, UpdatedAt: turnEnd}
	ft.d.setSessions("mac", []proto.Session{stopped("s1", "合好了"), stopped("s2", "选哪个？"), working})
	done := proto.WorkState{Phase: proto.WorkDone, Line: "已合并 PR #12", TurnAt: turnEnd,
		Artifacts: []proto.WorkArtifact{{Kind: "pr", Ref: "https://github.com/example/app/pull/12"}}}
	ask := proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitUser, Line: "等你选方案", TurnAt: turnEnd, InteractionID: "work_1"}
	if sess := listed(t, s, "mac", "s1"); sess.Work != nil || sess.State != proto.StateUnread {
		t.Fatalf("before any report: %s %+v", sess.State, sess.Work)
	}
	before := len(ft.toldOf(assistantwire.EventFleetSessions))
	s.fleetTakeSnapshot(Snapshot{Work: workReport(true, map[string]proto.WorkState{"s1": done, "s2": ask, "s3": done})})
	if len(ft.toldOf(assistantwire.EventFleetSessions)) != before+1 {
		t.Fatal("the hub was not told the list the report changed")
	}
	sess := listed(t, s, "mac", "s1")
	if sess.State != proto.StateIdle || sess.Work == nil || sess.Work.Phase != proto.WorkDone {
		t.Fatalf("done: %s %+v", sess.State, sess.Work)
	}
	if got := workLogLine(sess); !strings.HasPrefix(got, "完成：已合并 PR #12") || !strings.Contains(got, "/pull/12") {
		t.Fatalf("log line %q", got)
	}
	if sess := listed(t, s, "mac", "s3"); sess.Work == nil || sess.Work.Phase != proto.WorkWorking {
		t.Fatalf("working: %+v", sess.Work)
	}
	if sess := listed(t, s, "mac", "s2"); sess.Work == nil || sess.Work.InteractionID != "" || sess.Work.Line != "等你选方案" {
		t.Fatalf("no card: %+v", sess.Work)
	}
	putPending(s, &pending{ix: proto.Interaction{ID: "work_1", MachineID: "mac", SessionID: "s2", Kind: proto.IxQuestion,
		ToolName: assistantwire.WorkCardTool}})
	putPending(s, &pending{ix: proto.Interaction{ID: "own", MachineID: "mac", SessionID: "s1", Kind: proto.IxQuestion,
		Questions: []proto.Question{{Question: "删掉这个分支吗？"}}}})
	if sess := listed(t, s, "mac", "s2"); sess.State != proto.StateNeedsYou || sess.Work.InteractionID != "work_1" {
		t.Fatalf("the card: %s %+v", sess.State, sess.Work)
	}
	if sess := listed(t, s, "mac", "s1"); sess.Work.Phase != proto.WorkBlocked || sess.Work.Line != "删掉这个分支吗？" {
		t.Fatalf("the agent's own question first: %+v", sess.Work)
	}
	s.finishInteraction("own")

	s1 := stopped("s1", "又改了一点")
	s1.UpdatedAt = turnEnd.Add(time.Hour)
	ft.d.setSessions("mac", []proto.Session{s1, stopped("s2", "选哪个？"), working})
	if sess := listed(t, s, "mac", "s1"); sess.Work != nil || sess.State != proto.StateUnread {
		t.Fatalf("new turn: %s %+v", sess.State, sess.Work)
	}
	s.fleetTakeSnapshot(Snapshot{}) // a report it could not read keeps the last
	if sess := listed(t, s, "mac", "s2"); sess.Work == nil {
		t.Fatal("the last report went with one it could not read")
	}
	soon, late := time.Now().Add(time.Minute).UTC(), time.Now().Add(-10*time.Minute).UTC()
	wait := proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitTransient, Line: "模型服务暂时出错，自动重试", Wait: "503", TurnAt: turnEnd}
	soonW, lateW := wait, wait
	soonW.WakeAt, lateW.WakeAt = &soon, &late
	ft.d.setSessions("mac", []proto.Session{stopped("s4", "API Error: 503"), stopped("s5", "API Error: 503")})
	s.fleetTakeSnapshot(Snapshot{Work: workReport(true, map[string]proto.WorkState{"s4": soonW, "s5": lateW})})
	if sess := listed(t, s, "mac", "s4"); sess.State != proto.StateIdle || sess.Work.WakeAt == nil {
		t.Fatalf("a wake due soon: %s %+v", sess.State, sess.Work)
	}
	if sess := listed(t, s, "mac", "s5"); sess.State != proto.StateUnread || sess.Work == nil || sess.Work.WakeAt != nil {
		t.Fatalf("a wake long overdue: %s %+v", sess.State, sess.Work)
	}
	s.fleetTakeSnapshot(Snapshot{Work: &WorkReport{On: false}})
	if sess := listed(t, s, "mac", "s4"); sess.Work != nil {
		t.Fatalf("off: %+v", sess.Work)
	}
}

// The assistant's own cards (its snapshot's interactions) are shown like any card; one that left the snapshot is
// withdrawn and nothing is said to its Action.
func TestWithdrawnCardSaysNothing(t *testing.T) {
	ft := newFleetTestNoStore(t)
	s := ft.s
	card := proto.Interaction{ID: "work_z", MachineID: "mac", SessionID: "s1", Kind: proto.IxQuestion, ToolName: assistantwire.WorkCardTool,
		Questions: []proto.Question{{Question: "A？", Options: []proto.QuestionOption{{Label: "A"}, {Label: "B"}}}},
		ExpiresAt: time.Now().Add(time.Hour)}
	s.askOnPhone([]proto.Interaction{card})
	ft.waitPending(1)
	s.askOnPhone(nil)
	ft.waitPending(0)
	time.Sleep(100 * time.Millisecond)
	if s.answerKept(card.ID) {
		t.Fatal("a dismiss was kept for the Action")
	}
}

// The snapshot tells the assistant what stopped agents wait for, and what the finished ones concluded: the session
// lists as the manager decorates them with the Agents business's report (moved from the hub's golden test).
func TestGoldenAssistantSnapshotWork(t *testing.T) {
	ft := newFleetTestNoStore(t)
	s := ft.s
	ft.f.devices = []Device{{ID: "server", Name: "服务器", Online: true, Reachable: true}, {ID: "laptop", Name: "笔记本"}, {ID: "desk", Name: "台式机"}}
	ft.d.add("server", "服务器")
	setSettings(t, s, func(v *Settings) { v.Timezone = "Asia/Shanghai" })
	at := time.Now().Add(-10 * time.Minute).UTC()
	ft.d.setSessions("server", []proto.Session{
		{SessionID: "aaaaaaaa-1", Cwd: "/code/demo", Title: "修登录", Status: proto.StatusDone, UpdatedAt: at},
		{SessionID: "bbbbbbbb-2", Cwd: "/code/demo", Title: "等部署", Status: proto.StatusDone, UpdatedAt: at},
		{SessionID: "cccccccc-3", Cwd: "/code/demo", Title: "选方案", Status: proto.StatusDone, UpdatedAt: at},
		{SessionID: "dddddddd-4", Cwd: "/code/demo", Title: "没分诊", Status: proto.StatusDone, UpdatedAt: at},
	})
	wake := time.Now().Add(5 * time.Minute).UTC()
	var report []assistantwire.SessionWork
	for sid, w := range map[string]proto.WorkState{
		"aaaaaaaa-1": {Phase: proto.WorkDone, Line: "已合并 PR #12"},
		"bbbbbbbb-2": {Phase: proto.WorkBlocked, On: proto.WaitExternal, Line: "等部署跑完再跑线上测试", Wait: "部署跑完", WakeAt: &wake},
		"cccccccc-3": {Phase: proto.WorkBlocked, On: proto.WaitUser, Line: "等你选 D2 / D4"},
	} {
		w.TurnAt = at
		report = append(report, assistantwire.SessionWork{MachineID: "server", SessionID: sid, Work: w})
	}
	s.fleetTakeSnapshot(Snapshot{Work: &WorkReport{On: true, Sessions: report}})
	ft.f.answerApp(AppFeishu, FeishuStatusGet, func(json.RawMessage) (any, error) {
		return FeishuStatus{PublicURL: "http://hub.example.com:18660"}, nil
	})
	res := s.Context(context.Background(), ContextParams{SessionID: "side-session"})
	head := regexp.MustCompile(`^- 现在：\d{4}-\d\d-\d\d 周[日一二三四五六] \d\d:\d\d（([^）]+)）\n`)
	m := head.FindStringSubmatch(res.Text)
	if m == nil {
		t.Fatalf("snapshot header: %q", res.Text)
	}
	golden(t, "assistant_snapshot.work", "- 现在：YYYY-MM-DD 周X HH:MM（"+m[1]+"）\n"+res.Text[len(m[0]):])
}
