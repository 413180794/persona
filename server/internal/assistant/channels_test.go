package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/channel"
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
		if got := resolutionText(c.ix, c.r); got != c.want {
			t.Errorf("%+v: %q, want %q", c.r, got, c.want)
		}
	}
}

func TestBotStepAndStripContext(t *testing.T) {
	if s := botStep(proto.Item{Kind: proto.KindToolUse, ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"go test ./...\nmore"}`)}); s != "运行 `go test ./...`" {
		t.Fatalf("bash step %q", s)
	}
	if s := botStep(proto.Item{Kind: proto.KindToolUse, ToolName: "mcp__jarvis__kb_search", ToolInput: json.RawMessage(`{"query":"周报"}`)}); s != "kb_search `周报`" {
		t.Fatalf("mcp step %q", s)
	}
	if s := botStep(proto.Item{Kind: proto.KindText}); s != "" {
		t.Fatalf("text step %q", s)
	}
	in := "<jarvis-context kind=\"context\" title=\"x\" url=\"\">\n快照\n</jarvis-context>\n真正的问题"
	if got := stripContext(in); got != "真正的问题" {
		t.Fatalf("strip %q", got)
	}
}

// "/new <text>" goes on with just the text; a command the bot does not know goes on as it is (it may be the
// conversation's own).
func TestBotCommandsPassTheRestOn(t *testing.T) {
	s := newTestService(t, newFakeHub().withMachine())
	ctx := context.Background()
	if h, rest := s.botCommand(ctx, FeishuFeatures{Main: true}, "om", "/new 帮我写周报"); h || rest != "帮我写周报" {
		t.Fatalf("/new with text: handled=%v rest=%q", h, rest)
	}
	if got := s.botTargetFor("", ""); got.Kind != targetJarvis || got.SessionID != "" {
		t.Fatalf("/new points the chat at a new conversation: %+v", got)
	}
	if h, rest := s.botCommand(ctx, FeishuFeatures{}, "om", "/compact"); h || rest != "/compact" {
		t.Fatalf("an unknown command goes on as it is: handled=%v rest=%q", h, rest)
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
	b := NewBusHub(BusOptions{Logf: t.Logf})
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
