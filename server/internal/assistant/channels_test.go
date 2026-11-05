package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// The channels' pipeline end to end (the hub's transport, the bus, the assistant) is the hub's channels_test.go and
// bots_test.go; these are the assistant's own parts.

func TestResolutionText(t *testing.T) {
	yes, no := true, false
	q := proto.Interaction{Kind: proto.IxQuestion, Questions: []proto.Question{{Question: "a"}, {Question: "b"}}}
	for _, c := range []struct {
		ix   proto.Interaction
		r    proto.Resolution
		want string
	}{
		{q, proto.Resolution{Answers: map[string]string{"a": "1", "b": "2"}}, "已回答：1；2"},
		{proto.Interaction{Kind: proto.IxPlan}, proto.Resolution{Approve: &yes, Mode: "acceptEdits"}, "已批准，自动接受编辑"},
		{proto.Interaction{Kind: proto.IxPlan}, proto.Resolution{Approve: &no, Feedback: "拆小点"}, "已打回：拆小点"},
		{proto.Interaction{Kind: proto.IxPermission}, proto.Resolution{Allow: &yes, Always: true}, "已允许（本会话不再问）"},
		{proto.Interaction{Kind: proto.IxPermission}, proto.Resolution{Allow: &no}, "已拒绝"},
		{proto.Interaction{}, proto.Resolution{Dismiss: true}, "已忽略"},
	} {
		if got := resolutionText("", c.ix, c.r); got != c.want {
			t.Errorf("%+v: %q, want %q", c.r, got, c.want)
		}
	}
}

// Each tool call reads as one line: a verb and what it works on.
func TestBotStepLines(t *testing.T) {
	for _, c := range []struct{ tool, in, want string }{
		{"Bash", `{"command":"go test ./...","description":"跑服务端测试"}`, "运行 · 跑服务端测试"},
		{"Bash", `{"command":"sleep 60","run_in_background":true}`, "后台运行 `sleep 60`"},
		{"Read", `{"file_path":"/a/feishu.go","offset":850,"limit":101}`, "读 `feishu.go` 第 850–950 行"},
		{"Read", `{"file_path":"/a/feishu.go","offset":20}`, "读 `feishu.go` 第 20 行起"},
		{"Read", `{"file_path":"/a/feishu.go"}`, "读 `feishu.go`"},
		{"Edit", `{"file_path":"/a/b.go"}`, "改 `b.go`"},
		{"Edit", `{"file_path":"/a/b.go","change":"add"}`, "新建 `b.go`"},
		{"Write", `{"file_path":"/a/b.go"}`, "写入 `b.go`"},
		{"Grep", `{"pattern":"botStep","path":"internal","glob":"*.go"}`, "搜 `botStep` 在 internal（&#42;.go）"},
		{"Grep", `{"pattern":"x","glob":"*.go"}`, "搜 `x`（&#42;.go）"},
		{"Glob", `{"pattern":"**/*.md"}`, "搜 `**/*.md`"},
		{"Task", `{"description":"查代码","subagent_type":"Explore"}`, "派子任务 · 查代码（Explore）"},
		{"TodoWrite", `{"todos":[{"content":"a","status":"completed"},{"content":"跑测试","activeForm":"正在跑测试","status":"in_progress"}]}`, "更新待办 · 正在跑测试"},
		{"mcp__jarvis__phone_calendar_events", `{"start":"2026-01-01"}`, "查日历"},
		{"mcp__jarvis__phone_contacts_search", `{"query":"张三"}`, "查通讯录 `张三`"},
		{"mcp__jarvis__phone_reminders_list", `{}`, "查提醒事项"},
		{"mcp__jarvis__phone_reminders_complete", `{"id":"r1"}`, "完成提醒 `r1`"},
		{"mcp__jarvis__phone_reminders_create", `{"title":"买菜"}`, "加提醒 `买菜`"},
		{"mcp__jarvis__mail_search", `{"query":"发票"}`, "查邮件 `发票`"},
		{"mcp__jarvis__mail_send", `{"to":"me@example.com"}`, "发邮件 `me@example.com`"},
		{"mcp__jarvis__kb_search", `{"query":"周报"}`, "查知识库 `周报`"},
		{"mcp__github__github_read", `{"url":"https://example.com/x"}`, "github_read `https://example.com/x`"},
	} {
		it := proto.Item{Kind: proto.KindToolUse, ToolName: c.tool, ToolInput: json.RawMessage(c.in)}
		if got := botStep("", it); got != c.want {
			t.Errorf("%s %s: %q, want %q", c.tool, c.in, got, c.want)
		}
	}
	// The agent message's titles keep the (verb, detail) split.
	verb, detail := botStepParts(i18n.En, proto.Item{Kind: proto.KindToolUse, ToolName: "Read", ToolInput: json.RawMessage(`{"file_path":"/a/x.go","offset":1,"limit":10}`)})
	if verb != "Read" || detail != "x.go lines 1–10" {
		t.Fatalf("%q %q", verb, detail)
	}
	for _, c := range []struct {
		l              i18n.Lang
		tool, in, want string
	}{
		{i18n.En, "Task", `{"description":"look","subagent_type":"Explore"}`, "Subtask · look (Explore)"},
		{i18n.En, "Grep", `{"pattern":"x","path":"src/x_y","glob":"*.go"}`, "Search · x in src/x_y (*.go)"},
		{"", "Task", `{"description":"查代码","subagent_type":"Explore"}`, "派子任务 · 查代码（Explore）"},
		// An unknown tool goes by its name: one separator in the agent message's title.
		{"", "mcp__x__github_read_file", `{"path":"abc_def"}`, "github_read_file · abc_def"},
	} {
		verb, detail := botStepParts(c.l, proto.Item{Kind: proto.KindToolUse, ToolName: c.tool, ToolInput: json.RawMessage(c.in)})
		if got := verb + " · " + detail; got != c.want { // as cotEvents joins them
			t.Errorf("%s: %q, want %q", c.tool, got, c.want)
		}
	}
}

func TestBotStepAndStripContext(t *testing.T) {
	if s := botStep("", proto.Item{Kind: proto.KindToolUse, ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"go test ./...\nmore"}`)}); s != "运行 `go test ./...`" {
		t.Fatalf("bash step %q", s)
	}
	if s := botStep("", proto.Item{Kind: proto.KindText}); s != "" {
		t.Fatalf("text step %q", s)
	}
	in := "<jarvis-context kind=\"context\" title=\"x\" url=\"\">\n快照\n</jarvis-context>\n真正的问题"
	if got := stripContext(in); got != "真正的问题" {
		t.Fatalf("strip %q", got)
	}
	for in, want := range map[string]string{
		"<persona-context kind=\"context\" title=\"x\" url=\"\">\n快照\n</persona-context>\n真正的问题":          "真正的问题",
		"<persona-context>\n快照\n</persona-context>\n<jarvis-context kind=\"doc\">文档</jarvis-context>\n问题": "问题",
		"问题\n<persona-context kind=\"context\">\n被截断":                                                     "问题",
	} {
		if got := stripContext(in); got != want {
			t.Fatalf("strip %q = %q, want %q", in, got, want)
		}
	}
}

// "/new <text>" goes on with just the text; a command the bot does not know goes on as it is (it may be the
// conversation's own).
func TestBotCommandsPassTheRestOn(t *testing.T) {
	s := newTestService(t, newFakeHub().withMachine())
	ctx := context.Background()
	if h, rest := s.botCommand(ctx, FeishuFeatures{Main: true}, "om", "/new 帮我写周报", "", ""); h || rest != "帮我写周报" {
		t.Fatalf("/new with text: handled=%v rest=%q", h, rest)
	}
	if got := s.botTargetFor("", ""); got.Kind != targetJarvis || got.SessionID != "" {
		t.Fatalf("/new points the chat at a new conversation: %+v", got)
	}
	if h, rest := s.botCommand(ctx, FeishuFeatures{}, "om", "/compact", "", ""); h || rest != "/compact" {
		t.Fatalf("an unknown command goes on as it is: handled=%v rest=%q", h, rest)
	}
}

// Commands run before the normal message's feature gate, so /stop and /status must check botAllowed against the
// target they resolve: a feature that is off can neither be interrupted nor shown in a status (#3903).
func TestBotCommandFeatureGate(t *testing.T) {
	f := newFakeHub().withMachine()
	f.setSessions("box", proto.Session{SessionID: "agent01", MachineID: "box", Agent: "claude"})
	var notes []string
	var posts []map[string]any
	f.answerApp(AppFeishu, FeishuText, func(p json.RawMessage) (any, error) {
		var m struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(p, &m)
		notes = append(notes, m.Text)
		return map[string]any{}, nil
	})
	f.answerApp(AppFeishu, FeishuPost, func(p json.RawMessage) (any, error) {
		var m map[string]any
		_ = json.Unmarshal(p, &m)
		posts = append(posts, m)
		return map[string]any{"message_id": "om_card"}, nil
	})
	s := newTestService(t, f)
	ctx := context.Background()
	s.setBotCurrent(botTarget{Kind: targetAgent, MachineID: "box", SessionID: "agent01"})
	s.rememberBotThread("om_agent", botTarget{Kind: targetAgent, MachineID: "box", SessionID: "agent01"}, "agent")

	// Agents feature off: /stop as a bare command and in the agent's thread interrupts nothing, and the note says why.
	for _, c := range []struct{ name, root, parent string }{
		{"agents off, current target", "", ""},
		{"agents off, agent thread", "", "om_agent"},
	} {
		before := len(f.interrupted())
		notes = nil
		if h, _ := s.botCommand(ctx, FeishuFeatures{}, "om", "/stop", c.root, c.parent); !h {
			t.Fatalf("%s: /stop was not handled", c.name)
		}
		if got := f.interrupted()[before:]; len(got) != 0 {
			t.Fatalf("%s: interrupted %v, want none", c.name, got)
		}
		want := i18n.T("", "assistant-feishu.agents_off")
		if len(notes) != 1 || notes[0] != want {
			t.Fatalf("%s: notes %v, want %q", c.name, notes, want)
		}
	}
	// Main off with Agents on and a real main conversation: /stop must reach the gate, not the main session.
	f.setSessions("server", proto.Session{SessionID: "main001", MachineID: "server", Assistant: true})
	setSettings(t, s, func(v *Settings) { v.MachineID = "server"; v.Main = "main001" })
	s.setBotCurrent(botTarget{Kind: targetMain})
	before := len(f.interrupted())
	notes = nil
	mainOff := i18n.T("", "assistant-feishu.main_off_agents_on")
	if h, _ := s.botCommand(ctx, FeishuFeatures{Agents: true}, "om", "/stop", "", ""); !h {
		t.Fatal("main off: /stop not handled")
	}
	if got := f.interrupted()[before:]; len(got) != 0 || len(notes) != 1 || notes[0] != mainOff {
		t.Fatalf("main off: interrupted %v notes %v, want none + %q", got, notes, mainOff)
	}
	// /status must not reveal a target the features do not allow, and must not post a card.
	notes, posts = nil, nil
	s.setBotCurrent(botTarget{Kind: targetAgent, MachineID: "box", SessionID: "agent01"})
	if h, _ := s.botCommand(ctx, FeishuFeatures{}, "om", "/status", "", "om_agent"); !h {
		t.Fatal("agents off /status: not handled")
	}
	if len(notes) != 1 || notes[0] != i18n.T("", "assistant-feishu.agents_off") || len(posts) != 0 {
		t.Fatalf("agents off /status: notes %v posts %d, want one agents_off note and no card", notes, len(posts))
	}
	// With Agents on, /stop interrupts as before.
	s.setBotCurrent(botTarget{Kind: targetAgent, MachineID: "box", SessionID: "agent01"})
	before = len(f.interrupted())
	if h, _ := s.botCommand(ctx, FeishuFeatures{Agents: true}, "om", "/stop", "", ""); !h {
		t.Fatal("agents on: /stop not handled")
	}
	if got := f.interrupted()[before:]; len(got) != 1 || got[0] != "box/agent01" {
		t.Fatalf("agents on: interrupted %v, want [box/agent01]", got)
	}
}

// "/stop" in an agent's thread (or replying to one of its cards) stops that agent, not the chat's current target;
// without a bound thread it stops the current target, as before (#3844).
func TestBotStopFollowsTheRepliedThread(t *testing.T) {
	f := newFakeHub().withMachine()
	f.setSessions("box", proto.Session{SessionID: "cur0001", MachineID: "box", Agent: "claude"},
		proto.Session{SessionID: "agent01", MachineID: "box", Agent: "claude"})
	s := newTestService(t, f)
	ctx := context.Background()
	s.setBotCurrent(botTarget{Kind: targetAgent, MachineID: "box", SessionID: "cur0001"})
	s.rememberBotThread("om_agent", botTarget{Kind: targetAgent, MachineID: "box", SessionID: "agent01"}, "agent")
	for _, c := range []struct {
		name         string
		root, parent string
		want         string
	}{
		{"no reply stops the current target", "", "", "box/cur0001"},
		{"the card it replies to binds the agent", "", "om_agent", "box/agent01"},
		{"the thread root binds the agent too", "om_agent", "om_reply", "box/agent01"},
		{"an unbound thread falls back to current", "om_other", "om_reply", "box/cur0001"},
	} {
		before := len(f.interrupted())
		if h, _ := s.botCommand(ctx, FeishuFeatures{Agents: true}, "om_cmd", "/stop", c.root, c.parent); !h {
			t.Fatalf("%s: /stop was not handled", c.name)
		}
		got := f.interrupted()[before:]
		if len(got) != 1 || got[0] != c.want {
			t.Fatalf("%s: interrupted %v, want [%s]", c.name, got, c.want)
		}
	}
}

// The snapshot says which channels each conversation's messages came from (newest first) and where a channel's turn is
// followed or was just answered: the hub draws the first and leaves the finished push out for the second.
func TestChannelsReportMarksAndTurns(t *testing.T) {
	s := newTestService(t, newFakeHub().withMachine())
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	s.chans.now = func() time.Time { return now }
	s.noteChannelConversation("wechat", "server", "c1")
	time.Sleep(2 * time.Millisecond)
	s.noteChannelConversation(channel.Feishu, "server", "c1")
	s.chans.mu.Lock()
	s.loadChannelStateLocked(context.Background())
	s.chans.st.Runs = append(s.chans.st.Runs, &channel.Run{ID: "r1", Channel: "wechat", MachineID: "server", SessionID: "c2"})
	s.chans.answered = map[[2]string]time.Time{{"server", "c3"}: now.Add(-time.Minute), {"server", "old"}: now.Add(-2 * time.Hour)}
	s.chans.mu.Unlock()
	r := s.channelsReport()
	if r == nil || len(r.Marks) != 1 || r.Marks[0].SessionID != "c1" || len(r.Marks[0].Channels) != 2 || r.Marks[0].Channels[0] != channel.Feishu {
		t.Fatalf("marks %+v", r)
	}
	if len(r.Turns) != 2 || r.Turns[0].SessionID != "c2" || !r.Turns[0].Answered.IsZero() || r.Turns[1].SessionID != "c3" || r.Turns[1].Answered.IsZero() {
		t.Fatalf("turns %+v", r.Turns)
	}
}

// The turns an older hub kept in the Feishu bot's state come over to the channels' runs once.
func TestFeishuLegacyRunsMove(t *testing.T) {
	st := &memStore{}
	ctx := context.Background()
	_ = st.PutSetting(ctx, botStateKey, map[string]any{"current": map[string]any{"kind": "main"}, "seen": []string{"om_1"},
		"runs": []map[string]any{{"id": "fb_om_2", "message_id": "om_2", "target": map[string]any{"kind": "jarvis", "machine_id": "server", "session_id": "c1"},
			"base": 7, "card": "om_card", "reaction": "rx"}}})
	s := newServiceOn(t, newFakeHub().withMachine(), st)
	runs := s.channelRuns(channel.Feishu)
	if len(runs) != 1 || runs[0].SessionID != "c1" || runs[0].Base != 7 || runs[0].Ext["card"] != "om_card" || runs[0].MsgIDs[0] != "feishu-om_2" {
		t.Fatalf("runs %+v", runs)
	}
	var left botState
	_, _ = st.Setting(ctx, botStateKey, &left)
	if len(left.Runs) != 0 || len(left.Seen) != 1 {
		t.Fatalf("the chat's state keeps %+v", left)
	}
	again := newServiceOn(t, newFakeHub().withMachine(), st)
	if n := len(again.channelRuns(channel.Feishu)); n != 1 {
		t.Fatalf("%d runs after a second start", n)
	}
}

// A channel with nowhere to send to yet (the hub's cannot_send) is told apart from a failed send; an answer is cut to
// what the channel's messages hold.
func TestChannelSendSplitsAndKnowsCannotSend(t *testing.T) {
	f := newFakeHub().withMachine()
	f.answerApp(AppChannels, ChannelsList, func(json.RawMessage) (any, error) {
		return map[string]any{"channels": []channel.View{{ID: "examplechat", Name: "Example Chat", Kind: channel.KindIM, Source: channel.SourcePlugin,
			Installed: true, State: channel.StateConnected, Capabilities: channel.Capabilities{MaxText: 200}}}}, nil
	})
	refused := false
	var sent []map[string]any
	f.answerApp(AppChannels, ChannelsSend, func(p json.RawMessage) (any, error) {
		if refused {
			return nil, &rpc.Error{Code: CodeCannotSend, Message: "nowhere yet"}
		}
		var m map[string]any
		_ = json.Unmarshal(p, &m)
		sent = append(sent, m)
		return map[string]any{}, nil
	})
	s := newTestService(t, f)
	ctx := context.Background()
	long := ""
	for len([]rune(long)) < 500 {
		long += "会议安排在上午十点。"
	}
	if err := s.channelSend(ctx, "examplechat", "ref", long, channel.OutAnswer, "m1"); err != nil {
		t.Fatal(err)
	}
	if len(sent) < 3 || sent[0]["ref"] != "ref#0" || sent[0]["reply_to"] != "m1" || len([]rune(sent[0]["text"].(string))) > 200 {
		t.Fatalf("sent %v", sent)
	}
	refused = true
	if err := s.channelSend(ctx, "examplechat", "ref2", "短", channel.OutAsk, ""); !errors.Is(err, errCannotSend) {
		t.Fatalf("cannot_send: %v", err)
	}
}

// feishuHub is a fake hub with the Feishu bot paired with owner (when given), recording what is posted.
func feishuHub(owner string, pairedAt time.Time) (*fakeHub, *[]map[string]any) {
	f := newFakeHub().withMachine()
	var posts []map[string]any
	f.answerApp(AppFeishu, FeishuStatusGet, func(json.RawMessage) (any, error) {
		return FeishuStatus{Ready: owner != "", Enabled: true, Owner: owner, PairedAt: pairedAt, Features: FeishuFeatures{Main: true, Cards: true}}, nil
	})
	f.answerApp(AppFeishu, FeishuPost, func(p json.RawMessage) (any, error) {
		var m map[string]any
		_ = json.Unmarshal(p, &m)
		posts = append(posts, m)
		return map[string]any{"message_id": "om_card"}, nil
	})
	f.answerApp(AppFeishu, FeishuUpdateCard, func(p json.RawMessage) (any, error) {
		var m map[string]any
		_ = json.Unmarshal(p, &m)
		m["update"] = true
		posts = append(posts, m)
		return map[string]any{}, nil
	})
	return f, &posts
}

// Paired with someone else, paired again or unpaired while the assistant was away (the hub's feishu.owner missed):
// the next look at the bot starts the chat afresh — the main conversation, no threads, nothing waiting. A state from
// before pairings were recorded keeps what it has.
func TestFeishuChatStartsAfreshWithANewPairing(t *testing.T) {
	st := &memStore{}
	ctx := context.Background()
	old := "ou_a@100"
	_ = st.PutSetting(ctx, botStateKey, botState{Current: botTarget{Kind: targetAgent, MachineID: "server", SessionID: "a1"},
		Threads: map[string]*botThread{"om_t": {Target: botTarget{Kind: targetAgent, MachineID: "server", SessionID: "a1"}}}, Pairing: &old})
	_ = st.PutSetting(ctx, channelsStateKey, channelsState{Runs: []*channel.Run{{ID: "fb_1", Channel: channel.Feishu, MachineID: "server", SessionID: "a1"}}})
	f, _ := feishuHub("ou_b", time.Unix(200, 0))
	s := newServiceOn(t, f, st)
	if _, err := s.feishuStatus(ctx); err != nil {
		t.Fatal(err)
	}
	if cur := s.botTargetFor("", ""); cur.Kind != targetMain {
		t.Fatalf("current %+v", cur)
	}
	if got := s.botTargetFor("om_t", ""); got.Kind != targetMain || len(s.channelRuns(channel.Feishu)) != 0 {
		t.Fatalf("threads %+v, runs %v", got, s.channelRuns(channel.Feishu))
	}
	// A state from before: the pairing now is recorded, nothing reset.
	st2 := &memStore{}
	_ = st2.PutSetting(ctx, botStateKey, botState{Current: botTarget{Kind: targetJarvis, MachineID: "server", SessionID: "c1"}})
	f2, _ := feishuHub("ou_b", time.Unix(200, 0))
	s2 := newServiceOn(t, f2, st2)
	_, _ = s2.feishuStatus(ctx)
	if cur := s2.botTargetFor("", ""); cur.SessionID != "c1" {
		t.Fatalf("an older state was reset: %+v", cur)
	}
	var saved botState
	_, _ = st2.Setting(ctx, botStateKey, &saved)
	if saved.Pairing == nil || *saved.Pairing != "ou_b@200" {
		t.Fatalf("pairing %v", saved.Pairing)
	}
}

// A card that ends before (or while) it is sent to Feishu does not stay there with live buttons.
func TestFeishuCardOfAnEndedQuestionIsNotLeftOpen(t *testing.T) {
	f, posts := feishuHub("ou_a", time.Unix(100, 0))
	f.settings = &Settings{MachineID: "server", Main: "m1"}
	s := newTestService(t, f)
	ix := proto.Interaction{ID: "ix_q", MachineID: "server", SessionID: "m1", Kind: proto.IxQuestion, Questions: []proto.Question{{Question: "好吗？"}}}
	s.InteractionEnded(context.Background(), InteractionEnded{Interaction: ix})
	s.feishuInteraction(ix)
	if len(*posts) != 0 {
		t.Fatalf("an ended question was asked in Feishu: %v", *posts)
	}
	ix.ID = "ix_q2"
	s.feishuInteraction(ix)
	if len(*posts) != 1 {
		t.Fatalf("posts %v", *posts)
	}
	s.InteractionEnded(context.Background(), InteractionEnded{Interaction: ix})
	waitUntil(t, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(*posts) == 2 && (*posts)[1]["update"] == true
	})
}

// A run keeps its answer until the hub takes it: the hub not answering where the channel is, or the send failing, is
// tried again with the same ref.
func TestChannelAnswerWaitsForTheHub(t *testing.T) {
	f := newFakeHub().withMachine()
	down := true
	f.answerApp(AppChannels, ChannelsList, func(json.RawMessage) (any, error) {
		if down {
			return nil, errors.New("the hub is restarting")
		}
		return map[string]any{"channels": []channel.View{{ID: "examplechat", Name: "Example Chat", Kind: channel.KindIM, Source: channel.SourcePlugin,
			Installed: true, State: channel.StateConnected}}}, nil
	})
	var sent []map[string]any
	f.answerApp(AppChannels, ChannelsSend, func(p json.RawMessage) (any, error) {
		var m map[string]any
		_ = json.Unmarshal(p, &m)
		sent = append(sent, m)
		return map[string]any{}, nil
	})
	s := newTestService(t, f)
	r := &channel.Run{ID: "examplechat_m1", Channel: "examplechat", MachineID: "server", SessionID: "c1", ReplyTo: "m1", Base: 3, Started: time.Now()}
	prog := &chanLive{mu: newChanMutex(), checking: newChanMutex(), done: make(chan struct{})}
	s.chans.mu.Lock()
	s.loadChannelStateLocked(context.Background())
	s.chans.st.Runs = append(s.chans.st.Runs, r)
	s.chans.live = map[string]*chanLive{r.ID: prog}
	s.chans.mu.Unlock()
	s.finishChannelRun(r, &channel.Result{Answer: "答案"})
	if n := len(s.channelRuns("examplechat")); n != 1 || len(sent) != 0 {
		t.Fatalf("with the hub away: %d runs, sent %v", n, sent)
	}
	f.mu.Lock()
	down = false
	f.mu.Unlock()
	s.forgetChannelList()
	s.finishChannelRun(r, &channel.Result{Answer: "答案"})
	if n := len(s.channelRuns("examplechat")); n != 0 || len(sent) != 1 || sent[0]["ref"] != "examplechat_m1:3" || sent[0]["text"] != "答案" {
		t.Fatalf("back: %d runs, sent %v", n, sent)
	}
}

// The text a followed conversation writes comes many times a second: one per conversation waits in line, with the
// newest text, and the important events are never crowded out.
func TestBusDeltasAreTakenTogether(t *testing.T) {
	b := NewBusHub(BusOptions{Logf: testLogf(t)})
	delta := func(sid, text string) json.RawMessage {
		raw, _ := json.Marshal(SessionDelta{MachineID: "server", SessionID: sid, Text: text})
		return raw
	}
	for i := 0; i < 5000; i++ {
		b.queueEvent(assistantwire.EventSessionDelta, delta("c1", fmt.Sprint("第", i, "个字")))
	}
	b.queueEvent(assistantwire.EventSessionDelta, delta("c2", "另一段"))
	b.queueEvent(assistantwire.EventTurnEnded, json.RawMessage(`{"machine_id":"server","session_id":"c1"}`))
	if n := len(b.events); n != 3 {
		t.Fatalf("%d events waiting, want 3", n)
	}
	first := <-b.events
	var p SessionDelta
	_ = json.Unmarshal(b.takeDelta(first.params), &p)
	if p.SessionID != "c1" || p.Text != "第4999个字" {
		t.Fatalf("delta %+v", p)
	}
}

// waitUntil waits up to 5 seconds for ok.
func waitUntil(t *testing.T, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal("timed out")
}

// pagedHub answers session.items a page at a time after the seq asked (more_after while more follow) and session.item
// whole: the transcript a run's answer is read from over the bus.
type pagedHub struct {
	*fakeHub
	items []proto.Item
	page  int
	full  map[string]string
	after []int // the after_seq of each session.items asked
}

func (h *pagedHub) DeviceCall(ctx context.Context, p assistantwire.DeviceCall) (json.RawMessage, error) {
	var a struct {
		SessionID string `json:"session_id"`
		After     int    `json:"after_seq"`
		ItemID    string `json:"item_id"`
		Lazy      bool   `json:"lazy_images"`
	}
	_ = json.Unmarshal(p.Params, &a)
	switch {
	case p.Method == DeviceSessionItems && a.SessionID == "s1" && a.Lazy:
		h.after = append(h.after, a.After)
		var page []proto.Item
		for _, it := range h.items {
			if it.Seq > a.After && len(page) < h.page {
				page = append(page, it)
			}
		}
		to := a.After
		if len(page) > 0 {
			to = page[len(page)-1].Seq
		}
		return json.Marshal(map[string]any{"items": page, "more_after": to < h.items[len(h.items)-1].Seq, "to_seq": to})
	case p.Method == "session.item" && a.SessionID == "s1":
		return json.Marshal(proto.Item{ID: a.ItemID, Role: "assistant", Kind: proto.KindText, Text: h.full[a.ItemID]})
	}
	return h.fakeHub.DeviceCall(ctx, p)
}

// The assistant reads a run's answer over the bus (channel.ReadTurn): every page after the run's base, its message
// found, the answer's cut item read whole.
func TestReadTurnOverTheBus(t *testing.T) {
	f := newFakeHub().withMachine()
	h := &pagedHub{fakeHub: f, page: 2, full: map[string]string{"a4": "the whole answer"}, items: []proto.Item{
		{ID: "u1", Seq: 1, Role: "user", Kind: proto.KindText, Text: "earlier"},
		{ID: "u2", Seq: 2, Role: "user", Kind: proto.KindText, Text: "ours", ClientMsgID: "wechat-1"},
		{ID: "t3", Seq: 3, Role: "assistant", Kind: proto.KindToolUse},
		{ID: "a4", Seq: 4, Role: "assistant", Kind: proto.KindText, Text: "the whole…", FullSize: 16},
	}}
	s := withSettings(t, f, New(Options{Hub: h, Store: func() Store { return &memStore{} }, Logf: testLogf(t)}))
	rd, err := s.readTurn(context.Background(), &channel.Run{MachineID: "server", SessionID: "s1", MsgIDs: []string{"wechat-1"}})
	if err != nil || !rd.Found || rd.Answer != "the whole answer" || rd.Last != 4 {
		t.Fatalf("%+v %v", rd, err)
	}
	if !slices.Equal(h.after, []int{0, 2}) {
		t.Fatalf("pages asked after %v", h.after)
	}
}

// A failed read of the channels' settings, their runs or the Feishu chat's state used to leave an empty copy in
// memory that the next change wrote over the stored one (every other channel's settings, the runs waiting, the chat's
// target, threads and pairing), and the first message after it wrote into a nil map: a panic that took the assistant
// down. Now nothing is written until a read succeeds, and a message is handed back to the hub to try again.
func TestChannelsReadFailureNeverOverwrites(t *testing.T) {
	ctx := context.Background()
	store := &flakyScheduleStore{data: map[string][]byte{}}
	_ = store.PutSetting(ctx, channelsKey, map[string]*channelPrefs{"telegram": {Features: map[string]bool{"asks": false}}})
	_ = store.PutSetting(ctx, channelsStateKey, channelsState{Runs: []*channel.Run{{ID: "wechat_x_m0", Channel: "wechat_x"}},
		Seen: map[string][]string{"wechat_x": {"m0"}}})
	pairing := "ou_example"
	_ = store.PutSetting(ctx, botStateKey, botState{Current: botTarget{Kind: targetMain}, Pairing: &pairing,
		Threads: map[string]*botThread{"om_old": {Target: botTarget{Kind: targetMain}}}})
	before := store.stored()
	store.failReads(true)
	// The runs' machine is known: otherwise their watchers drop them (the machine left the configuration) while the
	// test is still looking.
	h := newServiceOn(t, newFakeHub().withMachine(), store)

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panicked while the state could not be read: %v", r)
			}
		}()
		err := h.ChannelReceive(ctx, ChannelReceive{Channel: "wechat_x", Message: []byte(`{"id":"m1","text":"示例"}`)})
		var re *rpc.Error
		if !errors.As(err, &re) || re.Code != CodeTryAgain {
			t.Fatalf("channels.receive while unreadable: %v, want %s", err, CodeTryAgain)
		}
		if first, err := h.claimBotMessage("om_new"); first || err == nil {
			t.Fatalf("a Feishu message while unreadable: first %v, err %v", first, err)
		}
		h.rememberBotThread("om_bot", botTarget{Kind: targetMain}, "示例")
		h.setBotCurrent(botTarget{Kind: targetAgent, MachineID: "server", SessionID: "s2"})
		h.noteChannelConversation("wechat_x", "server", "s1")
		h.startChannelRun(&channel.Run{ID: "wechat_x_m2", Channel: "wechat_x", MachineID: "server", SessionID: "s1"})
	}()
	if n := store.writes(); n != 0 {
		t.Fatalf("%d writes while the state could not be read", n)
	}
	for k, v := range store.stored() {
		if before[k] != v {
			t.Fatalf("%s changed while unreadable:\n before %s\n after  %s", k, before[k], v)
		}
	}

	// Readable again: what was stored is what is read, and a message is taken once.
	store.failReads(false)
	if err := h.ChannelReceive(ctx, ChannelReceive{Channel: "wechat_x", Message: []byte(`{"id":"m0","text":"示例"}`)}); err != nil {
		t.Fatalf("a message seen before: %v", err)
	}
	if first, err := h.claimChannelMessage("wechat_x", "m3"); !first || err != nil {
		t.Fatalf("a new message: first %v, err %v", first, err)
	}
	// The run started while unreadable is kept next to the stored one, each with its own fields (the load used to
	// decode into the live run, under its watcher), and both are written back.
	runs := map[string]channel.Run{}
	for _, r := range h.channelRuns("wechat_x") {
		runs[r.ID] = r
	}
	if r := runs["wechat_x_m0"]; r.Channel != "wechat_x" || r.MachineID != "" || r.SessionID != "" {
		t.Fatalf("the stored run: %+v (runs %+v)", r, runs)
	}
	if r := runs["wechat_x_m2"]; r.Channel != "wechat_x" || r.MachineID != "server" || r.SessionID != "s1" {
		t.Fatalf("the run started while unreadable: %+v (runs %+v)", r, runs)
	}
	var saved channelsState
	_, _ = store.Setting(ctx, channelsStateKey, &saved)
	if len(saved.Runs) != 2 || !slices.Contains(saved.Seen["wechat_x"], "m0") {
		t.Fatalf("the stored state: %s", store.stored()[channelsStateKey])
	}
	var prefs map[string]*channelPrefs
	_, _ = store.Setting(ctx, channelsKey, &prefs)
	if prefs["telegram"] == nil {
		t.Fatalf("the channels' settings: %s", store.stored()[channelsKey])
	}
	if first, err := h.claimBotMessage("om_new"); !first || err != nil {
		t.Fatalf("a Feishu message once readable: first %v, err %v", first, err)
	}
	var bot botState
	_, _ = store.Setting(ctx, botStateKey, &bot)
	if bot.Pairing == nil || *bot.Pairing != pairing || bot.Threads["om_old"] == nil {
		t.Fatalf("the chat's state: %s", store.stored()[botStateKey])
	}
}

// The bot's cards and lines read in the user's language; without one they are Chinese.
func TestBotTextInLanguages(t *testing.T) {
	yes := true
	if got := resolutionText(i18n.En, proto.Interaction{}, proto.Resolution{Approve: &yes, Mode: "acceptEdits"}); got != "Approved, auto-accepting edits" {
		t.Fatal(got)
	}
	if got := resolutionText("", proto.Interaction{}, proto.Resolution{Approve: &yes}); got != "已批准" {
		t.Fatal(got)
	}
	bash := proto.Item{Kind: proto.KindToolUse, ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"go test ./..."}`)}
	if got := botStep(i18n.Ja, bash); got != "実行 `go test ./...`" {
		t.Fatal(got)
	}
	if got := botHelp(i18n.En, FeishuFeatures{Main: true}); !strings.Contains(got, "`/main` back to the main chat") || strings.Contains(got, "/agents") {
		t.Fatal(got)
	}
	if err := botAllowed(i18n.Ja, FeishuFeatures{}, botTarget{Kind: targetMain}); err == nil || !strings.Contains(err.Error(), "Larkボット") {
		t.Fatal(err)
	}
}

// A synced turn goes to Feishu whole, laid out like the bot's answers (more cards when long), where it came from and
// the open button under the last card only; a reply to any card goes back to that conversation.
func TestFeishuMirrorSendsTheWholeAnswer(t *testing.T) {
	f, posts := feishuHub("ou_a", time.Unix(100, 0))
	f.answerApp(AppFeishu, FeishuStatusGet, func(json.RawMessage) (any, error) {
		return FeishuStatus{Ready: true, Enabled: true, Owner: "ou_a", PairedAt: time.Unix(100, 0), Features: FeishuFeatures{Main: true, Sync: true}}, nil
	})
	f.settings = &Settings{MachineID: "server", Main: "m1"}
	f.setSessions("server", proto.Session{SessionID: "m1", Assistant: true})
	long := "# 晚报\n\n" + strings.Repeat("今天做完了很多事情。\n\n", 700) + "| 项 | 状态 |\n| --- | --- |\n| a | 完成 |\n\n结尾这句在最后"
	f.setItems(proto.Item{Role: "user", Kind: proto.KindText, Text: "写晚报"}, proto.Item{Role: "assistant", Kind: proto.KindText, Text: long})
	s := newTestService(t, f)
	s.feishuMirror(context.Background(), "server", "m1")
	if len(*posts) < 2 {
		t.Fatalf("one card for a long answer: %d", len(*posts))
	}
	first, _ := (*posts)[0]["card"].(string)
	last, _ := (*posts)[len(*posts)-1]["card"].(string)
	if strings.Contains(first, `"button"`) || !strings.Contains(last, `"button"`) || !strings.Contains(last, "主对话") || !strings.Contains(last, "写晚报") {
		t.Fatalf("first %s\nlast %s", first, last)
	}
	if !strings.Contains(last, "结尾这句在最后") || !strings.Contains(last, `"tag":"table"`) || strings.Contains(last, "只发了开头") {
		t.Fatalf("not the whole answer: %s", last)
	}
	if !strings.Contains(first, "晚报") || !strings.Contains(first, `"header"`) {
		t.Fatalf("no header: %s", first)
	}
	if got := s.botTargetFor("om_card", ""); got.Kind != targetJarvis || got.SessionID != "m1" {
		t.Fatalf("thread %+v", got)
	}
}

// A turn that ended without words says why, when the conversation says; otherwise the plain line as before.
func TestEmptyAnswerSaysWhyItStopped(t *testing.T) {
	if got := emptyAnswer("zh-Hans", "API 错误：overloaded"); got != "这一轮没有文字回答就停了：API 错误：overloaded" {
		t.Fatalf("with a reason: %q", got)
	}
	if got := emptyAnswer("zh-Hans", " "); got != "（这一轮没有文字回答）" {
		t.Fatalf("without: %q", got)
	}
	if got := emptyAnswer("en", "已中断"); !strings.Contains(got, "stopped without a text answer: 已中断") {
		t.Fatalf("en: %q", got)
	}
}
