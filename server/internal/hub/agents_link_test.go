package hub

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/assistant"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// The Agents business is the manager's (internal/assistant, agents_*.go); these are the hub's ends: the lists show its
// report, the turns reach it, its cards are shown and answered, it fills in the starts.

// A test without a manager does not wait for one either.
func init() { managerStartWait, managerRestartWait = 0, 0 }

// workNode is a fake node: it records session.send and session.start and answers session.items with reply as the
// turn's last answer.
type workNode struct {
	mu     sync.Mutex
	sent   []string // "session: text"
	starts []map[string]any
	ask    string
	reply  string
}

func (n *workNode) sends() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.sent...)
}

func (n *workNode) lastStart(t *testing.T) map[string]any {
	t.Helper()
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.starts) == 0 {
		t.Fatal("nothing started")
	}
	return n.starts[len(n.starts)-1]
}

func workMachine(t *testing.T, h *Hub) (*Machine, *workNode) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h2nR, h2nW := io.Pipe()
	n2hR, n2hW := io.Pipe()
	hubSide, nodeSide := rpc.New("h", h2nW), rpc.New("n", n2hW)
	n := &workNode{}
	nodeSide.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		var p map[string]any
		_ = json.Unmarshal(params, &p)
		n.mu.Lock()
		defer n.mu.Unlock()
		switch method {
		case "session.send":
			n.sent = append(n.sent, p["session_id"].(string)+": "+p["text"].(string))
		case "session.start":
			n.starts = append(n.starts, p)
			return map[string]any{"session": proto.Session{SessionID: "new-1", Cwd: "/code"}}, nil
		case "session.items":
			items := []proto.Item{}
			if n.ask != "" {
				items = append(items, proto.Item{Kind: proto.KindText, Role: "user", Text: n.ask})
			}
			if n.reply != "" {
				items = append(items, proto.Item{Kind: proto.KindText, Role: "assistant", Text: n.reply})
			}
			return map[string]any{"items": items}, nil
		}
		return map[string]any{}, nil
	}
	go func() { _ = hubSide.Serve(ctx, n2hR) }()
	go func() { _ = nodeSide.Serve(ctx, h2nR) }()
	t.Cleanup(func() { cancel(); _ = h2nW.Close(); _ = n2hW.Close() })
	m := newMachine(h, MachineConfig{ID: "box", Name: "示例电脑", Kind: "local"})
	m.peer, m.online = hubSide, true
	h.machines = append(h.machines, m)
	h.byID["box"] = m
	return m, n
}

// workModel answers every model call with reply.
func workModel(t *testing.T, reply string) *ai.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{"content": []any{map[string]any{"type": "text", "text": reply}}})
	}))
	t.Cleanup(srv.Close)
	key := filepath.Join(t.TempDir(), "key")
	_ = os.WriteFile(key, []byte("k"), 0o600)
	return ai.New(ai.Config{BaseURL: srv.URL, APIKeyFile: key, Model: "m"})
}

var turnEnd = time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)

func stopped(id, last string) proto.Session {
	return proto.Session{SessionID: id, Title: "修登录", Cwd: "/code/app", Status: proto.StatusDone, UpdatedAt: turnEnd, LastText: last}
}

func setWorkSessions(m *Machine, list ...proto.Session) {
	for i := range list {
		list[i].MachineID = m.cfg.ID
	}
	m.mu.Lock()
	m.sessions = list
	m.mu.Unlock()
}

func workSession(t *testing.T, h *Hub, m *Machine, sid string) proto.Session {
	t.Helper()
	s, ok := h.decoratedSession(m, sid)
	if !ok {
		t.Fatalf("no session %s", sid)
	}
	return s
}

func pendingWorkCards(h *Hub) []proto.Interaction {
	var out []proto.Interaction
	for _, ix := range h.pendingInteractions() {
		if ix.ToolName == workCardTool {
			out = append(out, ix)
		}
	}
	return out
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

// The lists show what the manager reported for a turn while the session is still stopped on it; what it shows while
// it works or asks on its own card is the hub's; the triage off, nothing; a report it could not read changes nothing.
func TestWorkOnTheListsIsTheReport(t *testing.T) {
	h := newTestHub(t)
	m, _ := workMachine(t, h)
	working := proto.Session{SessionID: "s3", Status: proto.StatusWorking, UpdatedAt: turnEnd}
	setWorkSessions(m, stopped("s1", "合好了"), stopped("s2", "选哪个？"), working)
	done := proto.WorkState{Phase: proto.WorkDone, Line: "已合并 PR #12", TurnAt: turnEnd,
		Artifacts: []proto.WorkArtifact{{Kind: "pr", Ref: "https://github.com/example/app/pull/12"}}}
	ask := proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitUser, Line: "等你选方案", TurnAt: turnEnd, InteractionID: "work_1"}
	if s := workSession(t, h, m, "s1"); s.Work != nil || s.State != proto.StateUnread {
		t.Fatalf("before any report: %s %+v", s.State, s.Work)
	}
	changed := h.takeWorkReport(&assistantwire.WorkReport{On: true, Sessions: []assistantwire.SessionWork{
		{MachineID: "box", SessionID: "s1", Work: done}, {MachineID: "box", SessionID: "s2", Work: ask},
		{MachineID: "box", SessionID: "s3", Work: done}}})
	if len(changed) != 1 || changed[0] != "box" {
		t.Fatalf("changed machines %v", changed)
	}
	s := workSession(t, h, m, "s1")
	if s.State != proto.StateIdle || s.Work == nil || s.Work.Phase != proto.WorkDone {
		t.Fatalf("done: %s %+v", s.State, s.Work)
	}
	if got := workLogLine(s); !strings.HasPrefix(got, "完成：已合并 PR #12") || !strings.Contains(got, "/pull/12") {
		t.Fatalf("log line %q", got)
	}
	if s := workSession(t, h, m, "s3"); s.Work == nil || s.Work.Phase != proto.WorkWorking {
		t.Fatalf("working: %+v", s.Work)
	}
	// The card is not shown (yet): the list does not name it.
	if s := workSession(t, h, m, "s2"); s.Work == nil || s.Work.InteractionID != "" || s.Work.Line != "等你选方案" {
		t.Fatalf("no card: %+v", s.Work)
	}
	h.mu.Lock()
	h.interactions["work_1"] = &pending{ix: proto.Interaction{ID: "work_1", MachineID: "box", SessionID: "s2", Kind: proto.IxQuestion,
		ToolName: workCardTool}}
	h.interactions["own"] = &pending{ix: proto.Interaction{ID: "own", MachineID: "box", SessionID: "s1", Kind: proto.IxQuestion,
		Questions: []proto.Question{{Question: "删掉这个分支吗？"}}}}
	h.mu.Unlock()
	if s := workSession(t, h, m, "s2"); s.State != proto.StateNeedsYou || s.Work.InteractionID != "work_1" {
		t.Fatalf("the card: %s %+v", s.State, s.Work)
	}
	if s := workSession(t, h, m, "s1"); s.Work.Phase != proto.WorkBlocked || s.Work.Line != "删掉这个分支吗？" {
		t.Fatalf("the agent's own question first: %+v", s.Work)
	}
	h.mu.Lock()
	delete(h.interactions, "own")
	h.mu.Unlock()

	// A new turn is not covered by the old report; one it could not read keeps the last; off, nothing.
	s1 := stopped("s1", "又改了一点")
	s1.UpdatedAt = turnEnd.Add(time.Hour)
	setWorkSessions(m, s1, stopped("s2", "选哪个？"), working)
	if s := workSession(t, h, m, "s1"); s.Work != nil || s.State != proto.StateUnread {
		t.Fatalf("new turn: %s %+v", s.State, s.Work)
	}
	if got := h.takeWorkReport(nil); got != nil {
		t.Fatalf("a report it could not read changed %v", got)
	}
	if s := workSession(t, h, m, "s2"); s.Work == nil {
		t.Fatal("the last report went with one it could not read")
	}
	// A wake long overdue (nobody woke it: the manager is away) is no wait that ends by itself any more.
	soon, late := time.Now().Add(time.Minute).UTC(), time.Now().Add(-10*time.Minute).UTC()
	wait := proto.WorkState{Phase: proto.WorkBlocked, On: proto.WaitTransient, Line: "模型服务暂时出错，自动重试", Wait: "503", TurnAt: turnEnd}
	soonW, lateW := wait, wait
	soonW.WakeAt, lateW.WakeAt = &soon, &late
	setWorkSessions(m, stopped("s4", "API Error: 503"), stopped("s5", "API Error: 503"))
	h.takeWorkReport(&assistantwire.WorkReport{On: true, Sessions: []assistantwire.SessionWork{
		{MachineID: "box", SessionID: "s4", Work: soonW}, {MachineID: "box", SessionID: "s5", Work: lateW}}})
	if s := workSession(t, h, m, "s4"); s.State != proto.StateIdle || s.Work.WakeAt == nil {
		t.Fatalf("a wake due soon: %s %+v", s.State, s.Work)
	}
	if s := workSession(t, h, m, "s5"); s.State != proto.StateUnread || s.Work == nil || s.Work.WakeAt != nil {
		t.Fatalf("a wake long overdue: %s %+v", s.State, s.Work)
	}

	h.takeWorkReport(&assistantwire.WorkReport{On: false})
	if s := workSession(t, h, m, "s4"); s.Work != nil {
		t.Fatalf("off: %+v", s.Work)
	}
}

// End to end with the manager: a turn ends on the hub, the manager triages it (on the hub's model), puts a card up
// for the decision, the user answers it on the hub, and the answer goes to the agent.
func TestAgentWorkOverTheBus(t *testing.T) {
	h := newTestHub(t)
	h.cfg.Agents.Triage.SettleSeconds = 1
	m, n := workMachine(t, h)
	h.triageAI = workModel(t, `{"phase":"blocked","on":"user","line":"等你选方案",
		"questions":[{"question":"用哪个方案？","header":"方案","options":[{"label":"用 D4","description":"推荐"},{"label":"用 D2"}]}]}`)
	n.reply = "D2 / D4 选哪个？"
	connectAssistant(t, h)
	mux := h.routes()
	if code, out := call(t, mux, "PUT", "/api/v1/agents/triage", h.cfg.Token, `{"enabled":true,"decide":true,"auto_continue":true,"wake":true}`); code != 200 {
		t.Fatalf("switches: %d %v", code, out)
	}
	if code, out := call(t, mux, "GET", "/api/v1/agents/triage", h.cfg.Token, ""); code != 200 || out["decide"] != true {
		t.Fatalf("GET switches: %d %v", code, out)
	}
	at := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
	working := proto.Session{SessionID: "s1", Title: "选方案", Cwd: "/code/app", Status: proto.StatusWorking, UpdatedAt: at}
	m.setSessions([]proto.Session{working})
	done := working
	done.Status, done.LastText = proto.StatusDone, n.reply
	m.setSessions([]proto.Session{done}) // the turn ends: session.turn_ended goes to the manager
	var card proto.Interaction
	eventually(t, 20*time.Second, "the manager's card", func() bool {
		cards := pendingWorkCards(h)
		if len(cards) == 1 {
			card = cards[0]
		}
		return len(cards) == 1
	})
	if card.SessionTitle == "" || card.Questions[0].Options[0].Label != "用 D4" {
		t.Fatalf("card %+v", card)
	}
	eventually(t, 10*time.Second, "the work on the list", func() bool {
		s := workSession(t, h, m, "s1")
		return s.State == proto.StateNeedsYou && s.Work != nil && s.Work.InteractionID == card.ID
	})
	if err := h.resolve(card.ID, proto.Resolution{Answers: map[string]string{"用哪个方案？": "用 D4"}}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 10*time.Second, "the answer at the agent", func() bool {
		got := n.sends()
		return len(got) == 1 && strings.Contains(got[0], "用哪个方案？ → 用 D4")
	})
	eventually(t, 10*time.Second, "the card off the list", func() bool {
		s := workSession(t, h, m, "s1")
		return len(pendingWorkCards(h)) == 0 && s.Work != nil && s.Work.InteractionID == ""
	})
	// Off: the lists show no work.
	if code, _ := call(t, mux, "PUT", "/api/v1/agents/triage", h.cfg.Token, `{"enabled":false}`); code != 200 {
		t.Fatal("switch off")
	}
	eventually(t, 10*time.Second, "no work while off", func() bool { return workSession(t, h, m, "s1").Work == nil })
}

// An answer to one of the manager's cards given while it is not connected reaches it once it is back and reported its
// snapshot; one it could not take then is tried again; a card it still reports after its answer went, with none
// waiting, is shown again.
func TestManagerCardAnswerWaitsForIt(t *testing.T) {
	h := newTestHub(t)
	h.bus.idents = &memIdents{}
	saved := answerRetryAfter
	answerRetryAfter = 0
	t.Cleanup(func() { answerRetryAfter = saved })
	card := proto.Interaction{ID: "work_x", MachineID: "box", SessionID: "s1", Kind: proto.IxQuestion, ToolName: workCardTool,
		Questions: []proto.Question{{Question: "A 还是 B？", Options: []proto.QuestionOption{{Label: "A"}, {Label: "B"}}}},
		ExpiresAt: time.Now().Add(time.Hour)}
	h.askOnPhone([]proto.Interaction{card})
	eventually(t, 5*time.Second, "the card shown", func() bool { return len(pendingWorkCards(h)) == 1 })
	if err := h.resolve(card.ID, proto.Resolution{Answers: map[string]string{"A 还是 B？": "A"}}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "the answer kept", func() bool { return h.answerKept(card.ID) })
	mux := http.NewServeMux()
	h.busRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	tok, err := h.createBusToken(context.Background(), assistantwire.AssistantID, "助手")
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan assistantwire.Action, 4)
	var mu sync.Mutex
	actions := 0
	r := dialRawBusWith(t, srv, tok, func(_ context.Context, method string, params json.RawMessage) (any, error) {
		switch method {
		case assistantwire.MethodTools:
			return map[string]any{"tools": []any{}}, nil
		case assistantwire.MethodAction:
			mu.Lock()
			actions++
			first := actions == 1
			mu.Unlock()
			if first {
				return nil, &rpc.Error{Code: "busy", Message: "not now"} // not final: kept
			}
			var a assistantwire.Action
			_ = json.Unmarshal(params, &a)
			got <- a
			return assistantwire.ActionResult{OK: true}, nil
		}
		return map[string]any{}, nil
	})
	if err := r.call(assistantwire.MethodHello, map[string]any{"id": assistantwire.AssistantID}, nil); err != nil {
		t.Fatal(err)
	}
	snapshot := func(seq uint64) {
		t.Helper()
		if err := r.call(assistantwire.MethodSnapshot, assistantwire.Snapshot{Epoch: "e", Seq: seq,
			Interactions: []proto.Interaction{card}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	snapshot(1) // the first delivery: refused for now
	eventually(t, 5*time.Second, "the first delivery", func() bool { mu.Lock(); defer mu.Unlock(); return actions == 1 })
	eventually(t, 5*time.Second, "kept after a refusal", func() bool { return h.answerKept(card.ID) })
	if len(pendingWorkCards(h)) != 0 {
		t.Fatal("shown again while its answer waits")
	}
	snapshot(2)
	select {
	case a := <-got:
		var res proto.Resolution
		_ = json.Unmarshal(a.Answer, &res)
		if a.InteractionID != card.ID || res.Answers["A 还是 B？"] != "A" {
			t.Fatalf("delivered %+v %+v", a, res)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the kept answer was not delivered")
	}
	// It still reports the card a while after: shown again.
	savedAgain := askAgainAfter
	askAgainAfter = 0
	t.Cleanup(func() { askAgainAfter = savedAgain })
	eventually(t, 5*time.Second, "nothing kept", func() bool { return !h.answerKept(card.ID) })
	snapshot(3)
	eventually(t, 5*time.Second, "the card shown again", func() bool { return len(pendingWorkCards(h)) == 1 })

	// An answer for what it says is gone is not kept.
	if h.keepAnswer(assistantwire.Action{InteractionID: "work_y"}, &rpc.Error{Code: assistantwire.CodeGone}, time.Now().Add(time.Hour)) {
		t.Fatal("kept an answer for a card that is gone")
	}
}

// A card the manager withdraws says nothing to it (no late dismiss that would clear a card raised again).
func TestWithdrawnCardSaysNothing(t *testing.T) {
	h := newTestHub(t)
	card := proto.Interaction{ID: "work_z", MachineID: "box", SessionID: "s1", Kind: proto.IxQuestion, ToolName: workCardTool,
		Questions: []proto.Question{{Question: "A？", Options: []proto.QuestionOption{{Label: "A"}, {Label: "B"}}}},
		ExpiresAt: time.Now().Add(time.Hour)}
	h.askOnPhone([]proto.Interaction{card})
	eventually(t, 5*time.Second, "the card shown", func() bool { return len(pendingWorkCards(h)) == 1 })
	h.askOnPhone(nil)
	eventually(t, 5*time.Second, "the card withdrawn", func() bool { return len(pendingWorkCards(h)) == 0 })
	time.Sleep(100 * time.Millisecond)
	if h.answerKept(card.ID) {
		t.Fatal("a dismiss was kept for the manager")
	}
}

// POST /api/v1/sessions: the manager says what a new Agent runs on (模型配置 › Agents, a model picked for this start
// over it) and names its tab. A start waits a little for a manager that is not connected yet (a hub that just
// started); while there is none the hub applies 模型配置 › Agents itself and the tab gets no name.
func TestAgentStartAsksTheManager(t *testing.T) {
	h := newTestHub(t)
	h.store = &newAgentStore{memStore: newMemStore(), b: ModelBinding{Provider: "launcher.work", Model: "m-1"}}
	h.ai = workModel(t, "fix-login")
	_, n := workMachine(t, h)
	mux := h.routes()
	start := func(body string) func(string) string {
		t.Helper()
		if code, out := call(t, mux, "POST", "/api/v1/sessions", h.cfg.Token, body); code != 200 {
			t.Fatalf("start %s: %d %v", body, code, out)
		}
		p := n.lastStart(t)
		return func(k string) string { v, _ := p[k].(string); return v }
	}
	p := start(`{"machine_id":"box","cwd":"/code","prompt":"修一下登录页的报错"}`)
	if p("launcher") != "work" || p("model") != "m-1" || p("model_fingerprint") == "" || p("tab_name") != "" {
		t.Fatalf("without the manager: %q %q %q", p("launcher"), p("model"), p("tab_name"))
	}
	p = start(`{"machine_id":"box","cwd":"/code","prompt":"修一下登录页的报错","model":"picked"}`)
	if p("launcher") != "work" || p("model") != "picked" || p("model_fingerprint") == "" {
		t.Fatalf("without the manager, a model picked: %q %q", p("launcher"), p("model"))
	}

	// The hub just started: a start waits for the manager connecting.
	saved := managerStartWait
	managerStartWait = 20 * time.Second
	t.Cleanup(func() { managerStartWait = saved })
	h.agentWork.mu.Lock()
	h.agentWork.firstWait = time.Time{}
	h.agentWork.mu.Unlock()
	waited := make(chan func(string) string, 1)
	go func() { waited <- start(`{"machine_id":"box","cwd":"/code","prompt":"修一下登录页的报错"}`) }()
	time.Sleep(300 * time.Millisecond)
	connectAssistant(t, h)
	select {
	case p = <-waited:
	case <-time.After(15 * time.Second):
		t.Fatal("the start did not go on once the manager connected")
	}
	if p("launcher") != "work" || p("tab_name") != "fix-login" {
		t.Fatalf("waited for the manager: %q %q", p("launcher"), p("tab_name"))
	}

	p = start(`{"machine_id":"box","cwd":"/code","prompt":"修一下登录页的报错"}`)
	if p("launcher") != "work" || p("model") != "m-1" || p("model_fingerprint") == "" || p("tab_name") != "fix-login" {
		t.Fatalf("with the manager: %q %q %q", p("launcher"), p("model"), p("tab_name"))
	}
	p = start(`{"machine_id":"box","cwd":"/code","prompt":"x","launcher":"other","model":"picked","tab_name":"mine"}`)
	if p("launcher") != "other" || p("model") != "picked" || p("tab_name") != "mine" {
		t.Fatalf("picked for this start: %q %q %q", p("launcher"), p("model"), p("tab_name"))
	}
	p = start(`{"machine_id":"box","cwd":"/code","resume_session_id":"old-1"}`)
	if p("launcher") != "" || p("model") != "" || p("tab_name") != "" {
		t.Fatalf("a resume keeps what it runs on: %q %q %q", p("launcher"), p("model"), p("tab_name"))
	}
	// The Feishu card's start and an Agent started in the background ask for a tab name alone.
	if got := h.autoTabName(context.Background(), "修一下登录页的报错"); got != "fix-login" {
		t.Fatalf("tab name %q", got)
	}
}

// The snapshot tells the assistant what stopped agents wait for, and what the finished ones concluded (the hub's
// lists, with the manager's report on them).
func TestGoldenAssistantSnapshotWork(t *testing.T) {
	h := snapshotHub(t, false)
	srv := h.byID["server"]
	srv.online = true
	at := time.Now().Add(-10 * time.Minute).UTC()
	srv.sessions = []proto.Session{
		{SessionID: "aaaaaaaa-1", Cwd: "/code/demo", Title: "修登录", Status: proto.StatusDone, UpdatedAt: at},
		{SessionID: "bbbbbbbb-2", Cwd: "/code/demo", Title: "等部署", Status: proto.StatusDone, UpdatedAt: at},
		{SessionID: "cccccccc-3", Cwd: "/code/demo", Title: "选方案", Status: proto.StatusDone, UpdatedAt: at},
		{SessionID: "dddddddd-4", Cwd: "/code/demo", Title: "没分诊", Status: proto.StatusDone, UpdatedAt: at},
	}
	eventually(t, 10*time.Second, "the assistant's first snapshot", func() bool { // after it, nothing is reported here
		h.assistant.mu.RLock()
		defer h.assistant.mu.RUnlock()
		return h.assistant.got
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
	h.takeWorkReport(&assistantwire.WorkReport{On: true, Sessions: report})
	res, err := h.asst().Context(context.Background(), assistant.ContextParams{SessionID: "side-session"})
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "assistant_snapshot.work", normalSnapshot(t, res.Text))
}
