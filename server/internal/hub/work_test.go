package hub

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/assistant"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// workNode is a fake node: it records session.send and answers session.items with reply as the turn's last answer.
type workNode struct {
	mu    sync.Mutex
	sent  []string // "session: text"
	ask   string
	reply string
}

func (n *workNode) sends() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.sent...)
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
	m.peer = hubSide
	h.machines = append(h.machines, m)
	h.byID["box"] = m
	return m, n
}

func setWorkSessions(m *Machine, list ...proto.Session) {
	for i := range list {
		list[i].MachineID = m.cfg.ID
	}
	m.mu.Lock()
	m.sessions = list
	m.mu.Unlock()
}

// workModel answers every model call with reply and keeps what it was asked.
func workModel(t *testing.T, reply string) (*ai.Client, func() string) {
	t.Helper()
	var mu sync.Mutex
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		asked = string(b)
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"content": []any{map[string]any{"type": "text", "text": reply}}})
	}))
	t.Cleanup(srv.Close)
	key := filepath.Join(t.TempDir(), "key")
	_ = os.WriteFile(key, []byte("k"), 0o600)
	return ai.New(ai.Config{BaseURL: srv.URL, APIKeyFile: key, Model: "m"}), func() string {
		mu.Lock()
		defer mu.Unlock()
		return asked
	}
}

func workHub(t *testing.T) (*Hub, *Machine, *workNode) {
	t.Helper()
	h := newTestHub(t)
	m, n := workMachine(t, h)
	return h, m, n
}

var turnEnd = time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)

func stopped(id, last string) proto.Session {
	return proto.Session{SessionID: id, Title: "修登录", Cwd: "/code/app", Status: proto.StatusDone, UpdatedAt: turnEnd, LastText: last}
}

func entryOf(t *testing.T, h *Hub, sid string) db.SessionWork {
	t.Helper()
	e, ok := h.workEntry([2]string{"box", sid})
	if !ok {
		t.Fatalf("no work entry for %s", sid)
	}
	return e
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

func TestTransientErrors(t *testing.T) {
	for text, want := range map[string]bool{
		"API Error: 503 no eligible upstream account":                    true,
		"API Error: 529 {\"type\":\"overloaded_error\"}":                 true,
		"API Error: Connection error.":                                   true,
		"API Error: Request timed out.":                                  true,
		"API Error: 429 rate limit exceeded":                             true,
		"Failed to authenticate. API Error: 401 invalid x-api-key":       false,
		"OAuth token has expired. Please obtain a new token":             false,
		"Invalid API key · Please run /login":                            false,
		"There's an issue with the selected model (x). It may not exist": false,
	} {
		if got := transientError(text); got != want {
			t.Errorf("transientError(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestTriageRules(t *testing.T) {
	ask := proto.Session{Status: proto.StatusDone, PendingInteractions: 1}
	if _, _, skip := triageRules(ask, time.UTC); !skip {
		t.Fatal("a session asking on its own card is not triaged")
	}
	w, by, _ := triageRules(proto.Session{Status: proto.StatusDone, TurnError: "API Error: 503 upstream"}, time.UTC)
	if w == nil || w.On != proto.WaitTransient || by != "rule" {
		t.Fatalf("transient: %+v %s", w, by)
	}
	w, _, _ = triageRules(proto.Session{Status: proto.StatusDone, TurnError: "OAuth token has expired"}, time.UTC)
	if w == nil || w.On != proto.WaitUser || len(w.Questions) != 1 {
		t.Fatalf("sign-in: %+v", w)
	}
	w, _, _ = triageRules(proto.Session{Status: proto.StatusDone, Background: []proto.BackgroundTask{{}}}, time.UTC)
	if w == nil || w.On != proto.WaitExternal || w.Wait != "" {
		t.Fatalf("background: %+v", w)
	}
	w, _, _ = triageRules(proto.Session{Status: proto.StatusDone, Loop: &proto.LoopInfo{Kind: "wakeup"}}, time.UTC)
	if w == nil || w.On != proto.WaitExternal {
		t.Fatalf("loop: %+v", w)
	}
	if w, _, skip := triageRules(proto.Session{Status: proto.StatusDone, LastText: "改好了"}, time.UTC); w != nil || skip {
		t.Fatalf("plain answer goes to the model: %+v", w)
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

// A 503 gets 继续 after the backoff, once; three runs of it and the user is asked.
func TestTransientErrorIsContinued(t *testing.T) {
	h, m, n := workHub(t)
	const errText = "API Error: 503 no eligible upstream account"
	setWorkSessions(m, stopped("s1", errText))
	h.triageTurn(m, workSession(t, h, m, "s1"))
	e := entryOf(t, h, "s1")
	if e.WakeAt == nil || e.Work.On != proto.WaitTransient || len(n.sends()) != 0 {
		t.Fatalf("entry %+v, sent %v", e, n.sends())
	}
	if d := time.Until(*e.WakeAt); d < 25*time.Second || d > 35*time.Second {
		t.Fatalf("first retry in %v", d)
	}
	if s := workSession(t, h, m, "s1"); s.State != proto.StateIdle || s.Work == nil || s.Work.WakeAt == nil {
		t.Fatalf("a session the hub retries is no 完成未看: %s %+v", s.State, s.Work)
	}
	ctx := context.Background()
	h.workTick(ctx, time.Now())
	if len(n.sends()) != 0 {
		t.Fatal("sent before the backoff")
	}
	h.workTick(ctx, time.Now().Add(31*time.Second))
	h.workTick(ctx, time.Now().Add(32*time.Second))
	if got := n.sends(); len(got) != 1 || got[0] != "s1: 继续" {
		t.Fatalf("sent %v", got)
	}

	// It errs again after each 继续 (a new turn each time): the count carries over, and after the cap it is a card.
	for i := 1; ; i++ {
		h.workStarted("box", "s1", time.Now())
		s := stopped("s1", errText)
		s.UpdatedAt = turnEnd.Add(time.Duration(i) * time.Minute)
		setWorkSessions(m, s)
		h.triageTurn(m, workSession(t, h, m, "s1"))
		if e := entryOf(t, h, "s1"); e.WakeAt == nil {
			break
		}
		h.workTick(ctx, time.Now().Add(time.Hour))
		if i > 5 {
			t.Fatal("never stopped retrying")
		}
	}
	if got := n.sends(); len(got) != 3 {
		t.Fatalf("继续 sent %d times: %v", len(got), got)
	}
	e = entryOf(t, h, "s1")
	if e.Work.On != proto.WaitUser || e.InteractionID == "" {
		t.Fatalf("after the cap: %+v", e)
	}
	if cards := pendingWorkCards(h); len(cards) != 1 || cards[0].Questions[0].Options[0].Label != answerRetry {
		t.Fatalf("cards %+v", cards)
	}
}

// Whatever the user did meanwhile wins: the hub sends nothing once the session moved on.
func TestWorkGuardYieldsToTheUser(t *testing.T) {
	for name, change := range map[string]func(*proto.Session){
		"working":       func(s *proto.Session) { s.Status = proto.StatusWorking },
		"new turn":      func(s *proto.Session) { s.UpdatedAt = s.UpdatedAt.Add(time.Minute) },
		"queued input":  func(s *proto.Session) { s.Queued = []string{"先别动"} },
		"seen in herdr": func(s *proto.Session) { s.Status = proto.StatusIdle },
	} {
		t.Run(name, func(t *testing.T) {
			h, m, n := workHub(t)
			setWorkSessions(m, stopped("s1", "API Error: 503 upstream"))
			h.triageTurn(m, workSession(t, h, m, "s1"))
			s := stopped("s1", "API Error: 503 upstream")
			change(&s)
			setWorkSessions(m, s)
			h.workTick(context.Background(), time.Now().Add(time.Minute))
			if got := n.sends(); len(got) != 0 {
				t.Fatalf("sent %v", got)
			}
			if e := entryOf(t, h, "s1"); e.WakeAt != nil && name != "working" {
				t.Fatalf("wake left: %+v", e)
			}
		})
	}
}

// 「部署完我再跑线上测试」: the hub wakes it after the wait it named, and later ones double.
func TestExternalWaitWakes(t *testing.T) {
	h, m, n := workHub(t)
	model, asked := workModel(t, `{"phase":"blocked","on":"external","line":"等部署跑完再跑线上测试",
		"wait":{"what":"部署跑完","then":"跑线上测试","check_minutes":5}}`)
	h.triageAI = model
	n.reply = "已经触发部署，部署完我再跑线上测试。"
	setWorkSessions(m, stopped("s1", n.reply))
	h.triageTurn(m, workSession(t, h, m, "s1"))
	if !strings.Contains(asked(), "部署完我再跑线上测试") {
		t.Fatalf("the model was not shown the reply: %s", asked())
	}
	e := entryOf(t, h, "s1")
	if e.WakeAt == nil || e.Work.On != proto.WaitExternal || e.Work.Every != 5 {
		t.Fatalf("entry %+v", e)
	}
	if d := time.Until(*e.WakeAt); d < 4*time.Minute || d > 6*time.Minute {
		t.Fatalf("wake in %v", d)
	}
	h.workTick(context.Background(), time.Now().Add(6*time.Minute))
	got := n.sends()
	if len(got) != 1 || !strings.Contains(got[0], "部署跑完") || !strings.Contains(got[0], "先停下来问用户") {
		t.Fatalf("sent %v", got)
	}
	if e := entryOf(t, h, "s1"); e.Wakes != 1 || e.WakeAt != nil {
		t.Fatalf("after the wake: %+v", e)
	}

	if wakeAfter(5, 0) != 5*time.Minute || wakeAfter(5, 2) != 20*time.Minute || wakeAfter(5, 5) != time.Hour ||
		wakeAfter(0, 0) != 10*time.Minute || wakeAfter(120, 3) != 2*time.Hour {
		t.Fatal("wakeAfter")
	}
}

// A wait that ends in pushing code is never the hub's to end: the user is asked.
func TestRiskyNextStepIsACard(t *testing.T) {
	for _, reply := range []string{
		`{"phase":"blocked","on":"external","line":"CI 过了就推","wait":{"what":"CI 通过","then":"push 到 beta","check_minutes":5}}`,
		`{"phase":"blocked","on":"external","line":"等审核","wait":{"what":"审核","then":"部署到线上","check_minutes":30}}`,
		`{"phase":"blocked","on":"external","line":"等","wait":{"what":"CI","then":"跑一下","check_minutes":5,"risky":true}}`,
	} {
		h, m, n := workHub(t)
		h.triageAI, _ = workModel(t, reply)
		n.reply = "等着"
		setWorkSessions(m, stopped("s1", "等着"))
		h.triageTurn(m, workSession(t, h, m, "s1"))
		e := entryOf(t, h, "s1")
		if e.WakeAt != nil || e.Work.On != proto.WaitUser || e.InteractionID == "" {
			t.Fatalf("%s: entry %+v", reply, e)
		}
		h.workTick(context.Background(), time.Now().Add(time.Hour))
		if got := n.sends(); len(got) != 0 {
			t.Fatalf("%s: sent %v", reply, got)
		}
	}

	h, m, n := workHub(t)
	h.triageAI, _ = workModel(t, `{"phase":"blocked","on":"external","line":"CI 过了就推","wait":{"what":"CI 通过","then":"push 到 beta","check_minutes":5}}`)
	setWorkSessions(m, stopped("s1", "CI 过了我就推"))
	h.triageTurn(m, workSession(t, h, m, "s1"))
	card := pendingWorkCards(h)[0]
	if err := h.resolve(card.ID, proto.Resolution{Answers: map[string]string{card.Questions[0].Question: answerHold}}); err != nil {
		t.Fatal(err)
	}
	if got := n.sends(); len(got) != 0 {
		t.Fatalf("先别做 sent %v", got)
	}

	h, m, n = workHub(t)
	h.triageAI, _ = workModel(t, `{"phase":"blocked","on":"external","line":"CI 过了就推","wait":{"what":"CI 通过","then":"push 到 beta","check_minutes":5}}`)
	setWorkSessions(m, stopped("s1", "CI 过了我就推"))
	h.triageTurn(m, workSession(t, h, m, "s1"))
	card = pendingWorkCards(h)[0]
	if err := h.resolve(card.ID, proto.Resolution{Answers: map[string]string{card.Questions[0].Question: answerGo}}); err != nil {
		t.Fatal(err)
	}
	if got := n.sends(); len(got) != 1 || !strings.Contains(got[0], answerGo) {
		t.Fatalf("等好了就去做 sent %v", got)
	}
}

// The decision a turn ends on becomes a card; the answer goes back to the agent. Going on without it withdraws it.
func TestDecisionCard(t *testing.T) {
	h, m, n := workHub(t)
	h.triageAI, _ = workModel(t, `{"phase":"blocked","on":"user","line":"等你选方案",
		"questions":[{"question":"用哪个方案？","header":"方案","options":[{"label":"用 D4","description":"推荐"},{"label":"用 D2"}]},
		             {"question":"要不要推到 beta？","options":[{"label":"推"},{"label":"先不推"}]}]}`)
	setWorkSessions(m, stopped("s1", "D2 / D4 选哪个？要不要推到 beta？"))
	h.triageTurn(m, workSession(t, h, m, "s1"))
	cards := pendingWorkCards(h)
	if len(cards) != 1 || len(cards[0].Questions) != 2 || cards[0].Questions[0].Options[0].Label != "用 D4" {
		t.Fatalf("cards %+v", cards)
	}
	s := workSession(t, h, m, "s1")
	if s.State != proto.StateNeedsYou || s.Work.InteractionID != cards[0].ID || s.Work.Line != "等你选方案" {
		t.Fatalf("session %s %+v", s.State, s.Work)
	}
	if err := h.resolve(cards[0].ID, proto.Resolution{Answers: map[string]string{"用哪个方案？": "用 D4", "要不要推到 beta？": "先不推"}}); err != nil {
		t.Fatal(err)
	}
	got := n.sends()
	if len(got) != 1 || !strings.Contains(got[0], "用哪个方案？ → 用 D4") || !strings.Contains(got[0], "先不推") {
		t.Fatalf("sent %v", got)
	}
	if s := workSession(t, h, m, "s1"); s.Work.InteractionID != "" || len(pendingWorkCards(h)) != 0 {
		t.Fatalf("after the answer: %+v", s.Work)
	}

	// Another turn asks; the user answers in the terminal instead: the card goes.
	s2 := stopped("s2", "选哪个？")
	setWorkSessions(m, stopped("s1", "x"), s2)
	h.triageTurn(m, workSession(t, h, m, "s2"))
	if len(pendingWorkCards(h)) != 1 {
		t.Fatal("no card for s2")
	}
	h.workStarted("box", "s2", time.Now())
	if len(pendingWorkCards(h)) != 0 {
		t.Fatal("card not withdrawn when the agent went on")
	}
}

func TestDoneIsNoUnread(t *testing.T) {
	h, m, _ := workHub(t)
	h.triageAI, _ = workModel(t, `{"phase":"done","line":"已合并 PR #12，测试全过","artifacts":[{"kind":"pr","ref":"https://github.com/example/app/pull/12"},{"kind":"weird","ref":"x"}]}`)
	setWorkSessions(m, stopped("s1", "合好了"))
	if s := workSession(t, h, m, "s1"); s.State != proto.StateUnread {
		t.Fatalf("before triage: %s", s.State)
	}
	h.triageTurn(m, workSession(t, h, m, "s1"))
	s := workSession(t, h, m, "s1")
	if s.State != proto.StateIdle || s.Work.Phase != proto.WorkDone || len(s.Work.Artifacts) != 1 {
		t.Fatalf("done: %s %+v", s.State, s.Work)
	}
	if got := workLogLine(s); !strings.HasPrefix(got, "完成：已合并 PR #12") || !strings.Contains(got, "/pull/12") {
		t.Fatalf("log line %q", got)
	}
	// A new turn is not covered by the old triage.
	s1 := stopped("s1", "又改了一点")
	s1.UpdatedAt = turnEnd.Add(time.Hour)
	setWorkSessions(m, s1)
	if s := workSession(t, h, m, "s1"); s.Work != nil || s.State != proto.StateUnread {
		t.Fatalf("new turn: %s %+v", s.State, s.Work)
	}
}

// An agent waiting for another is woken when that one stops.
func TestAgentWaitWakesWhenTheOtherStops(t *testing.T) {
	h, m, n := workHub(t)
	other := proto.Session{SessionID: "abcdef123456", Title: "合 PR", Status: proto.StatusWorking, UpdatedAt: turnEnd}
	h.triageAI, _ = workModel(t, `{"phase":"blocked","on":"agent","line":"等合 PR 那个做完再 rebase",
		"wait":{"what":"合 PR 做完","then":"rebase","check_minutes":30,"agent":"abcdef12"}}`)
	setWorkSessions(m, stopped("s1", "等隔壁合完我再 rebase"), other)
	h.triageTurn(m, workSession(t, h, m, "s1"))
	e := entryOf(t, h, "s1")
	if e.Work.On != proto.WaitAgent || e.Work.Agent == nil || e.Work.Agent.SessionID != other.SessionID {
		t.Fatalf("entry %+v", e.Work)
	}
	h.workTick(context.Background(), time.Now())
	if len(n.sends()) != 0 {
		t.Fatal("woken while the other works")
	}
	other.Status, other.LastText = proto.StatusDone, "合好了"
	setWorkSessions(m, stopped("s1", "等隔壁合完我再 rebase"), other)
	h.workTick(context.Background(), time.Now())
	if got := n.sends(); len(got) != 1 || !strings.Contains(got[0], "合好了") {
		t.Fatalf("sent %v", got)
	}
}

func TestTriageWorkSanitizes(t *testing.T) {
	others := map[string]proto.WorkAgent{"abcd1234": {MachineID: "box", SessionID: "abcd1234-x"}}
	now := time.Now()
	if w := triageWork(triageOut{Phase: "maybe"}, others, now); w.Phase != "" {
		t.Fatalf("made-up phase: %+v", w)
	}
	if w := triageWork(triageOut{Phase: "blocked", On: "whoever", Line: "x"}, others, now); w.On != proto.WaitUser {
		t.Fatalf("unknown on: %+v", w)
	}
	var out triageOut
	_ = json.Unmarshal([]byte(`{"phase":"blocked","on":"agent","wait":{"what":"它","agent":"abcd1234"}}`), &out)
	if w := triageWork(out, others, now); w.On != proto.WaitAgent || w.Agent == nil {
		t.Fatalf("known agent: %+v", w)
	}
	_ = json.Unmarshal([]byte(`{"phase":"blocked","on":"agent","wait":{"what":"它","agent":"zzz"}}`), &out)
	if w := triageWork(out, others, now); w.On != proto.WaitExternal || w.Agent != nil {
		t.Fatalf("unknown agent: %+v", w)
	}
	out = triageOut{}
	_ = json.Unmarshal([]byte(`{"phase":"blocked","on":"user","line":"点链接","link":{"url":"javascript:alert(1)"}}`), &out)
	if w := triageWork(out, others, now); w.Link != nil {
		t.Fatalf("bad link kept: %+v", w.Link)
	}
	out = triageOut{}
	_ = json.Unmarshal([]byte(`{"phase":"blocked","on":"user","line":"点链接","link":{"url":"https://example.com/device","minutes":10}}`), &out)
	if w := triageWork(out, others, now); w.Link == nil || w.Link.ExpiresAt == nil || w.Link.ExpiresAt.Sub(now) < 9*time.Minute {
		t.Fatalf("link: %+v", w.Link)
	}
	var qs []proto.Question
	for i := 0; i < 6; i++ {
		qs = append(qs, proto.Question{Question: strings.Repeat("问", i+1), Options: []proto.QuestionOption{{Label: "a"}, {Label: "b"}, {Label: "c"}, {Label: "d"}, {Label: "e"}}})
	}
	qs[0].Options = qs[0].Options[:1]
	got := cardQuestions(qs)
	if len(got) != 4 || len(got[1].Options) != 4 || len(got[0].Options) != 2 || got[0].Options[1].Label != answerSkip {
		t.Fatalf("cardQuestions %+v", got)
	}
}

func TestSummarizeWork(t *testing.T) {
	at := turnEnd
	ev := func(kind, sid string, after float64, d map[string]any) db.WorkEvent {
		if d == nil {
			d = map[string]any{}
		}
		if after > 0 {
			d["after"] = after
		}
		return db.WorkEvent{At: at.Add(time.Duration(after) * time.Second), MachineID: "box", SessionID: sid, Kind: kind, Detail: d}
	}
	s := summarizeWork(24, []db.WorkEvent{
		ev("triaged", "s1", 0, map[string]any{"phase": "done", "by": "model"}),
		ev("triaged", "s2", 0, map[string]any{"phase": "blocked", "on": "user", "by": "model"}),
		ev("triaged", "s3", 0, map[string]any{"phase": "", "by": "model"}),
		ev("failed", "s4", 0, nil),
		ev("continued", "s5", 0, nil),
		ev("seen", "s1", 60, map[string]any{"phase": "done"}),
		ev("seen", "s1", 120, map[string]any{"phase": "done"}), // the same turn: no second wait
		ev("seen", "s2", 30, map[string]any{"phase": "blocked", "on": "user"}),
		ev("resumed", "s5", 40, map[string]any{"phase": "blocked", "on": "transient", "by": "hub"}),
		ev("sample", "", 0, map[string]any{"unread": 4.0, "needs_you": 1.0}),
		ev("sample", "", 0, map[string]any{"unread": 2.0}),
	})
	if s.Triaged["done"] != 1 || s.Triaged["blocked/user"] != 1 || s.Triaged["unsure"] != 1 || s.Triaged["failed"] != 1 {
		t.Fatalf("triaged %v", s.Triaged)
	}
	if s.Actions["continued"] != 1 || s.Resumed["hub"] != 1 {
		t.Fatalf("actions %v resumed %v", s.Actions, s.Resumed)
	}
	if s.Unread.Samples != 2 || s.Unread.Avg != 3 || s.Unread.Max != 4 || s.Unread.Last != 2 {
		t.Fatalf("unread %+v", s.Unread)
	}
	if w := s.Waits["done"]; w.N != 1 || w.Median != 60 {
		t.Fatalf("waits %+v", s.Waits)
	}
	if _, ok := s.Waits["blocked/transient"]; ok {
		t.Fatal("a wait the hub ended is no wait for the user")
	}
}

func TestTriageOff(t *testing.T) {
	h, m, _ := workHub(t)
	off := false
	h.cfg.Agents.Triage.Enabled = &off
	setWorkSessions(m, stopped("s1", "API Error: 503 upstream"))
	h.workTurnEnded(m, "s1")
	s := workSession(t, h, m, "s1")
	if s.Work != nil || s.State != proto.StateUnread {
		t.Fatalf("off: %s %+v", s.State, s.Work)
	}
	if _, ok := h.workEntry([2]string{"box", "s1"}); ok {
		t.Fatal("triaged while off")
	}
}

// The hub coming up triages the turns that ended while it was away.
func TestBackfillTriagesUnread(t *testing.T) {
	h, m, n := workHub(t)
	s := stopped("s1", "API Error: 503 upstream")
	s.UpdatedAt = time.Now().Add(-time.Hour)
	old := stopped("s2", "API Error: 503 upstream")
	old.UpdatedAt = time.Now().Add(-3 * 24 * time.Hour)
	setWorkSessions(m, s, old)
	h.workTick(context.Background(), time.Now())
	for end := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, ok := h.workEntry([2]string{"box", "s1"}); ok {
			break
		}
		if time.Now().After(end) {
			t.Fatal("recent unread turn not triaged")
		}
	}
	if _, ok := h.workEntry([2]string{"box", "s2"}); ok {
		t.Fatal("a turn from days ago was triaged")
	}
	if len(n.sends()) != 0 {
		t.Fatal("sent at once")
	}
}

func TestGoldenAgentTriage(t *testing.T) {
	h, m, n := workHub(t)
	h.cfg.Assistant.Owner = "示例用户"
	model, calls := captureModel(t, `{"phase":"done","line":"做完了"}`)
	h.triageAI = model
	n.ask, n.reply = "把登录页的崩溃修好，提 PR", "修好了，PR：https://github.com/example/app/pull/7，CI 在跑，过了要我合并吗？"
	other := proto.Session{SessionID: "abcdef123456", Title: "跑基准", Cwd: "/code/bench", Status: proto.StatusWorking, Activity: "在跑测试"}
	setWorkSessions(m, stopped("s1", n.reply), other)
	if _, err := h.triageTurnModel(context.Background(), m, workSession(t, h, m, "s1")); err != nil {
		t.Fatal(err)
	}
	c := onlyCall(t, calls())
	c.User = regexp.MustCompile(`现在：[^\n]+`).ReplaceAllString(c.User, "现在：YYYY-MM-DD HH:MM Day")
	goldenCall(t, "agent_triage", c)

	golden(t, "agent_wake.timer", renderWake(agentWake{Attempt: 2, Wait: "部署跑完", Then: "跑线上测试"}))
	golden(t, "agent_wake.agent", renderWake(agentWake{Attempt: 1, Wait: "合 PR 那个做完", Then: "rebase", Agent: "合 PR", AgentLine: "合好了"}))
	golden(t, "agent_answer", renderAnswer([]agentAnswerItem{{"用哪个方案？", "用 D4"}, {"要不要推到 beta？", "先不推"}}))
}

// The snapshot tells the assistant what stopped agents wait for, and what the finished ones concluded.
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
	wake := time.Now().Add(5 * time.Minute).UTC()
	for sid, w := range map[string]proto.WorkState{
		"aaaaaaaa-1": {Phase: proto.WorkDone, Line: "已合并 PR #12"},
		"bbbbbbbb-2": {Phase: proto.WorkBlocked, On: proto.WaitExternal, Line: "等部署跑完再跑线上测试", Wait: "部署跑完", WakeAt: &wake},
		"cccccccc-3": {Phase: proto.WorkBlocked, On: proto.WaitUser, Line: "等你选 D2 / D4"},
	} {
		w.TurnAt = at
		h.putWork(db.SessionWork{MachineID: "server", SessionID: sid, TurnAt: at, Work: w})
	}
	res, err := h.asst().Context(context.Background(), assistant.ContextParams{SessionID: "side-session"})
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "assistant_snapshot.work", normalSnapshot(t, res.Text))
}
