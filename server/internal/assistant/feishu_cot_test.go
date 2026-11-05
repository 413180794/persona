package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/feishubot"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// The words a turn says before a tool call go into the process; the words it ends with (its answer) do not.
func TestCotEventsHoldTheAnswerBack(t *testing.T) {
	var p cotProgress
	start := func(first string) []feishubot.CotEvent { return feishubot.CotStart("c1", "cot1", first) }
	items := []proto.Item{
		{ID: "a", Role: "assistant", Kind: proto.KindText, Text: "我先查一下"},
		{ID: "b", Role: "assistant", Kind: proto.KindToolUse, ToolUseID: "tu1", ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"ls"}`)},
		{ID: "c", Role: "user", Kind: proto.KindToolResult, ToolUseID: "tu1", Text: "a.txt"},
		{ID: "d", Role: "assistant", Kind: proto.KindText, Text: "最终结论"},
	}
	ev, para := cotEvents("", &p, items, "", start)
	types := []string{}
	all := ""
	for _, e := range ev {
		types = append(types, e.Type)
		all += e.Content
	}
	want := "RUN_STARTED REASONING_START REASONING_MESSAGE_START REASONING_MESSAGE_CONTENT REASONING_MESSAGE_END TOOL_CALL_START TOOL_CALL_END TOOL_CALL_RESULT"
	if strings.Join(types, " ") != want {
		t.Fatalf("events %v", types)
	}
	if para != "p-a" || !strings.Contains(all, "我先查一下") || strings.Contains(all, "最终结论") || !strings.Contains(all, `"parentMessageId":"p-a"`) || !strings.Contains(all, "运行 · ls") {
		t.Fatalf("para %q, events %s", para, all)
	}
	// More comes: the held words go in once a call follows them, and the run is not started again.
	more := []proto.Item{{ID: "e", Role: "assistant", Kind: proto.KindToolUse, ToolUseID: "tu2", ToolName: "Read", ToolInput: json.RawMessage(`{"file_path":"/x/y.go"}`)}}
	ev, para = cotEvents("", &p, more, para, start)
	if para != "p-d" || ev[0].Type != "REASONING_MESSAGE_START" || !strings.Contains(ev[1].Content, "最终结论") {
		t.Fatalf("second batch %v (para %q)", ev, para)
	}
	// A result whose call it did not see is left out.
	if ev, _ := cotEvents("", &p, []proto.Item{{Kind: proto.KindToolResult, ToolUseID: "other"}}, para, start); len(ev) != 0 {
		t.Fatalf("stray result %v", ev)
	}
}

// cotHub is the Feishu fake with agent messages: refused when refuse is set.
func cotHub(t *testing.T, refuse bool) (*Service, *[]map[string]any, *[]string) {
	f, posts := feishuHub("ou_a", time.Unix(100, 0))
	var calls []string
	f.answerApp(AppFeishu, FeishuCotCreate, func(json.RawMessage) (any, error) {
		calls = append(calls, "create")
		if refuse {
			return nil, errors.New("feishu cot create: code 99991672: no permission")
		}
		return map[string]any{"cot_id": "cot1", "message_id": "om_cot"}, nil
	})
	f.answerApp(AppFeishu, FeishuCotAppend, func(p json.RawMessage) (any, error) {
		var m struct {
			Events []feishubot.CotEvent `json:"events"`
		}
		_ = json.Unmarshal(p, &m)
		for _, e := range m.Events {
			calls = append(calls, e.Type)
		}
		return map[string]any{}, nil
	})
	f.answerApp(AppFeishu, FeishuCotComplete, func(json.RawMessage) (any, error) {
		calls = append(calls, "complete")
		return map[string]any{}, nil
	})
	f.answerApp(AppFeishu, FeishuReact, func(json.RawMessage) (any, error) { return map[string]any{}, nil })
	f.answerApp(AppFeishu, FeishuUnreact, func(json.RawMessage) (any, error) { return map[string]any{}, nil })
	return newTestService(t, f), posts, &calls
}

func cotRun(s *Service) (*channel.Run, *chanLive) {
	r := &channel.Run{ID: "fb_m1", Channel: channel.Feishu, MachineID: "server", SessionID: "c1", ReplyTo: "om_m1", Label: "Persona", Started: time.Now()}
	prog := &chanLive{mu: newChanMutex(), checking: newChanMutex(), done: make(chan struct{}), items: []proto.Item{
		{ID: "a", Role: "assistant", Kind: proto.KindThinking, Text: "想一想"},
		{ID: "b", Role: "assistant", Kind: proto.KindToolUse, ToolUseID: "tu1", ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"ls"}`)},
		{ID: "c", Role: "user", Kind: proto.KindToolResult, ToolUseID: "tu1"},
	}, steps: []string{"运行 `ls`"}}
	s.chans.mu.Lock()
	s.loadChannelStateLocked(context.Background())
	s.chans.st.Runs = append(s.chans.st.Runs, r)
	s.chans.live = map[string]*chanLive{r.ID: prog}
	s.chans.mu.Unlock()
	return r, prog
}

// withCot gives r an agent message, as a run made before new turns went to the progress card has.
func withCot(s *Service, r *channel.Run) {
	s.setRunExt(r, "cot", "cot1")
	s.setRunExt(r, "cot_msg", "om_cot")
}

// A new turn shows in a progress card, not an agent message: the answer goes into the card with the process folded.
func TestFeishuNewTurnIsAProgressCard(t *testing.T) {
	s, posts, calls := cotHub(t, false)
	r, _ := cotRun(s)
	sink := feishuSink{s}
	sink.progress(context.Background(), r, time.Second, []string{"运行 `ls`"}, "")
	if len(*calls) != 0 || len(*posts) != 1 || r.Ext["card"] == "" || r.Ext["cot"] != "" {
		t.Fatalf("progress: posts %v, calls %v, ext %v", *posts, *calls, r.Ext)
	}
	s.finishChannelRun(r, &channel.Result{Answer: "结论在这里"})
	last := (*posts)[len(*posts)-1]
	card, _ := last["card"].(string)
	if len(*calls) != 0 || last["update"] != true || !strings.Contains(card, "collapsible_panel") || !strings.Contains(card, "已调用 1 次工具") || !strings.Contains(card, "结论在这里") {
		t.Fatalf("answer card %v (calls %v)", last, *calls)
	}
}

// A run that already had an agent message (from before) still adds to it and ends it; the answer is a card below.
func TestFeishuExistingAgentMessageStillFinishes(t *testing.T) {
	s, posts, calls := cotHub(t, false)
	r, _ := cotRun(s)
	withCot(s, r)
	sink := feishuSink{s}
	sink.progress(context.Background(), r, time.Second, nil, "")
	if len(*posts) != 0 || len(*calls) == 0 || (*calls)[0] != "RUN_STARTED" {
		t.Fatalf("progress: posts %v, calls %v", *posts, *calls)
	}
	s.finishChannelRun(r, &channel.Result{Answer: "结论在这里"})
	got := strings.Join(*calls, " ")
	if !strings.HasSuffix(got, "REASONING_END RUN_FINISHED") || strings.Contains(got, "create") || strings.Contains(got, "complete") {
		t.Fatalf("calls %s", got)
	}
	if len(*posts) != 1 {
		t.Fatalf("posts %v", *posts)
	}
	card, _ := (*posts)[0]["card"].(string)
	if !strings.Contains(card, "结论在这里") || strings.Contains(card, "collapsible_panel") || (*posts)[0]["reply_to"] != "om_m1" {
		t.Fatalf("answer card %v", (*posts)[0])
	}
}

// A turn with nothing but its answer gets no agent message.
func TestFeishuPlainAnswerHasNoAgentMessage(t *testing.T) {
	s, posts, calls := cotHub(t, false)
	r, prog := cotRun(s)
	prog.items, prog.steps = nil, nil
	s.finishChannelRun(r, &channel.Result{Answer: "好的"})
	if len(*calls) != 0 || len(*posts) != 1 || strings.Contains((*posts)[0]["card"].(string), "collapsible_panel") {
		t.Fatalf("calls %v, posts %v", *calls, *posts)
	}
}

// An append that did not reach the hub is made again from the same items: once, not twice.
func TestCotAppendTriedAgainSendsItOnce(t *testing.T) {
	s, _, calls := cotHub(t, false)
	r, _ := cotRun(s)
	withCot(s, r)
	sink := feishuSink{s}
	s.hub.(*fakeHub).answerApp(AppFeishu, FeishuCotAppend, func(json.RawMessage) (any, error) { return nil, rpc.ErrClosed })
	sink.progress(context.Background(), r, time.Second, nil, "")
	var got []string
	s.hub.(*fakeHub).answerApp(AppFeishu, FeishuCotAppend, func(p json.RawMessage) (any, error) {
		var m struct {
			Events []feishubot.CotEvent `json:"events"`
		}
		_ = json.Unmarshal(p, &m)
		for _, e := range m.Events {
			got = append(got, e.Type)
		}
		return map[string]any{}, nil
	})
	sink.progress(context.Background(), r, time.Second, nil, "")
	if strings.Count(strings.Join(got, " "), "RUN_STARTED") != 1 || strings.Count(strings.Join(got, " "), "TOOL_CALL_RESULT") != 1 || strings.Count(strings.Join(*calls, " "), "create") != 0 {
		t.Fatalf("again: %v (calls %v)", got, *calls)
	}
}

// A held run's next turn drops the agent message it had and shows in a progress card.
func TestHeldRunsNextTurnIsAProgressCard(t *testing.T) {
	s, posts, calls := cotHub(t, false)
	r, prog := cotRun(s)
	withCot(s, r)
	feishuSink{s}.progress(context.Background(), r, time.Second, nil, "")
	s.finishChannelRun(r, &channel.Result{Answer: "先答一部分", Held: true})
	for _, k := range []string{"cot", "cot_msg", "cot_para", "cot_done"} {
		if r.Ext[k] != "" {
			t.Fatalf("%s kept: %v", k, r.Ext)
		}
	}
	*calls, *posts = nil, nil
	prog.items = []proto.Item{{ID: "x", Role: "assistant", Kind: proto.KindToolUse, ToolUseID: "tu9", ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"pwd"}`)}}
	feishuSink{s}.progress(context.Background(), r, time.Second, []string{"运行 `pwd`"}, "")
	if len(*calls) != 0 || len(*posts) != 1 {
		t.Fatalf("next turn: calls %v, posts %v", *calls, *posts)
	}
}

// A turn that starts with a tool call hangs it under a paragraph that says so (botmux's shape: a whole paragraph
// before the call), not 「处理中…」.
func TestCotFirstToolCallHasItsParagraph(t *testing.T) {
	s, _, _ := cotHub(t, false)
	r, prog := cotRun(s)
	withCot(s, r)
	prog.items = prog.items[1:] // straight into the tool call
	var events []feishubot.CotEvent
	s.hub.(*fakeHub).answerApp(AppFeishu, FeishuCotAppend, func(p json.RawMessage) (any, error) {
		var m struct {
			Events []feishubot.CotEvent `json:"events"`
		}
		_ = json.Unmarshal(p, &m)
		events = append(events, m.Events...)
		return map[string]any{}, nil
	})
	feishuSink{s}.progress(context.Background(), r, time.Second, nil, "")
	s.finishChannelRun(r, &channel.Result{Answer: "结论"})
	var seq []string
	all := ""
	for _, e := range events {
		seq = append(seq, e.Type)
		all += e.Content
	}
	want := "RUN_STARTED REASONING_START REASONING_MESSAGE_START REASONING_MESSAGE_CONTENT REASONING_MESSAGE_END TOOL_CALL_START TOOL_CALL_END TOOL_CALL_RESULT REASONING_END RUN_FINISHED"
	if strings.Join(seq, " ") != want || strings.Contains(all, "处理中") || !strings.Contains(all, "调用工具") {
		t.Fatalf("events %v\n%s", seq, all)
	}
}

// The card comes a second after the message, before the conversation is seen working (queued, starting), and says
// so; then its elapsed time moves every few seconds without new steps. A held run gets none until it works again.
func TestFeishuCardFromTheStartAndAlwaysMoving(t *testing.T) {
	s, posts, _ := cotHub(t, false)
	r, prog := cotRun(s)
	prog.items, prog.steps = nil, nil
	now := time.Unix(1000, 0)
	r.Started = now
	s.chans.now = func() time.Time { return now }
	s.channelProgressTick(r.ID)
	if len(*posts) != 0 {
		t.Fatalf("at once: %v", *posts)
	}
	now = now.Add(feishuProgressAfter)
	s.channelProgressTick(r.ID)
	if len(*posts) != 1 || !strings.Contains((*posts)[0]["card"].(string), "准备中") {
		t.Fatalf("first card: %v", *posts)
	}
	now = now.Add(feishuProgressIdle - time.Second)
	s.channelProgressTick(r.ID)
	if len(*posts) != 1 {
		t.Fatalf("too soon: %v", *posts)
	}
	s.chans.mu.Lock()
	r.Seen = true // the conversation works, but on another turn: this message waits behind it
	s.chans.mu.Unlock()
	now = now.Add(time.Second)
	s.channelProgressTick(r.ID)
	if len(*posts) != 2 || (*posts)[1]["update"] != true || !strings.Contains((*posts)[1]["card"].(string), "排队中… · 4 秒") {
		t.Fatalf("queued: %v", *posts)
	}
	// Its own message shows in the conversation: from there on it works, with only its own steps.
	s.chans.mu.Lock()
	r.Probe = "我的问题"
	noteRunItems("", r, prog, []proto.Item{
		{Seq: 1, ID: "x", Role: "assistant", Kind: proto.KindToolUse, ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"earlier turn"}`)},
		{Seq: 2, ID: "m", Role: "user", Kind: proto.KindText, Text: "我的问题"},
		{Seq: 3, ID: "y", Role: "assistant", Kind: proto.KindToolUse, ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"mine"}`)},
	}, now)
	s.chans.mu.Unlock()
	now = now.Add(feishuProgressIdle)
	s.channelProgressTick(r.ID)
	if card := (*posts)[len(*posts)-1]["card"].(string); !strings.Contains(card, "处理中 · 7 秒") || !strings.Contains(card, "mine") || strings.Contains(card, "earlier turn") {
		t.Fatalf("working: %v", *posts)
	}
	// A held run waiting on its background work (not working): no new card.
	*posts = nil
	s.chans.mu.Lock()
	r.Held = true
	delete(r.Ext, "card")
	s.chans.mu.Unlock()
	now = now.Add(time.Minute)
	s.channelProgressTick(r.ID)
	if len(*posts) != 0 {
		t.Fatalf("held run: %v", *posts)
	}
}

// A turn that stopped without words (an API error) tells the chat why, not just that there is no answer.
func TestFeishuEmptyTurnSaysWhyItStopped(t *testing.T) {
	s, posts, _ := cotHub(t, false)
	r, _ := cotRun(s)
	s.finishChannelRun(r, &channel.Result{Stopped: "API 错误：overloaded"})
	if len(*posts) != 1 {
		t.Fatalf("posts %v", *posts)
	}
	card, _ := (*posts)[0]["card"].(string)
	if !strings.Contains(card, "这一轮没有文字回答就停了") || !strings.Contains(card, "overloaded") {
		t.Fatalf("answer card %s", card)
	}
}

// The progress card is not sent again when it is the same; after Feishu's rate limit updates wait (5 s, then
// longer) and the next due one sends the latest; a withdrawn card is forgotten and the next tick posts a new one.
func TestFeishuProgressCardThrottled(t *testing.T) {
	s, posts, _ := cotHub(t, false)
	r, _ := cotRun(s)
	now := time.Unix(1000, 0)
	s.chans.now = func() time.Time { return now }
	sink := feishuSink{s}
	ctx := context.Background()
	steps := []string{"运行 `ls`"}
	sink.progress(ctx, r, 4*time.Second, steps, "")
	sink.progress(ctx, r, 4*time.Second, steps, "")
	if len(*posts) != 1 {
		t.Fatalf("same card sent again: %v", *posts)
	}
	f := s.hub.(*fakeHub)
	var reply error
	f.answerApp(AppFeishu, FeishuUpdateCard, func(p json.RawMessage) (any, error) {
		var m map[string]any
		_ = json.Unmarshal(p, &m)
		m["update"] = true
		*posts = append(*posts, m)
		return map[string]any{}, reply
	})
	reply = &feishubot.APIError{Op: "update card", Code: 230020, Msg: "This operation triggers the frequency limit."}
	sink.progress(ctx, r, 5*time.Second, steps, "")
	if len(*posts) != 2 {
		t.Fatalf("limited update not tried: %v", *posts)
	}
	reply = nil
	now = now.Add(4 * time.Second)
	sink.progress(ctx, r, 9*time.Second, steps, "")
	if len(*posts) != 2 {
		t.Fatalf("sent while backing off: %v", *posts)
	}
	now = now.Add(time.Second)
	sink.progress(ctx, r, 10*time.Second, steps, "")
	if len(*posts) != 3 || !strings.Contains((*posts)[2]["card"].(string), "10 秒") {
		t.Fatalf("latest after the backoff: %v", *posts)
	}
	reply = &feishubot.APIError{Op: "update card", Code: 230011, Msg: "The message is recalled."}
	sink.progress(ctx, r, 11*time.Second, steps, "")
	if r.Ext["card"] != "" {
		t.Fatalf("withdrawn card kept: %v", r.Ext)
	}
	sink.progress(ctx, r, 11*time.Second, steps, "")
	if len(*posts) != 5 || (*posts)[4]["update"] == true || r.Ext["card"] != "om_card" {
		t.Fatalf("no new card: %v", *posts)
	}
}
