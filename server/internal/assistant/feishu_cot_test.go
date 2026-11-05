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

// A turn shows as one agent message (created as it starts, ended as it ends) and the answer as one card below it,
// without the process in it and without buttons.
func TestFeishuTurnIsAnAgentMessage(t *testing.T) {
	s, posts, calls := cotHub(t, false)
	r, _ := cotRun(s)
	sink := feishuSink{s}
	if d := sink.progressAfter(r); d != 0 {
		t.Fatalf("the agent message waits %v", d)
	}
	sink.progress(context.Background(), r, time.Second, nil, "")
	if len(*posts) != 0 || len(*calls) == 0 || (*calls)[0] != "create" {
		t.Fatalf("progress: posts %v, calls %v", *posts, *calls)
	}
	s.finishChannelRun(r, &channel.Result{Answer: "结论在这里"})
	got := strings.Join(*calls, " ")
	if !strings.HasSuffix(got, "REASONING_END RUN_FINISHED") || strings.Count(got, "create") != 1 || strings.Contains(got, "complete") {
		t.Fatalf("calls %s", got)
	}
	if len(*posts) != 1 {
		t.Fatalf("posts %v", *posts)
	}
	card, _ := (*posts)[0]["card"].(string)
	if !strings.Contains(card, "结论在这里") || strings.Contains(card, "collapsible_panel") || strings.Contains(card, `"button"`) || (*posts)[0]["reply_to"] != "om_m1" {
		t.Fatalf("answer card %v", (*posts)[0])
	}
}

// Feishu refusing agent messages: the progress card comes after a few seconds as before, and the answer card carries
// the process in a folded panel.
func TestFeishuWithoutAgentMessagesFoldsTheProcessIntoTheCard(t *testing.T) {
	s, posts, calls := cotHub(t, true)
	r, _ := cotRun(s)
	sink := feishuSink{s}
	sink.progress(context.Background(), r, time.Second, []string{"运行 `ls`"}, "")
	if r.Ext["cot_off"] == "" || len(*posts) != 1 {
		t.Fatalf("fallback: ext %v, posts %v, calls %v", r.Ext, *posts, *calls)
	}
	if d := sink.progressAfter(r); d != channel.ProgressAfter {
		t.Fatalf("the card waits %v", d)
	}
	s.finishChannelRun(r, &channel.Result{Answer: "结论在这里"})
	last := (*posts)[len(*posts)-1]
	card, _ := last["card"].(string)
	if last["update"] != true || !strings.Contains(card, "collapsible_panel") || !strings.Contains(card, "已调用 1 次工具") || !strings.Contains(card, "结论在这里") {
		t.Fatalf("answer card %v", last)
	}
	if strings.Count(strings.Join(*calls, " "), "create") != 1 {
		t.Fatalf("asked again: %v", *calls)
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
	if strings.Count(strings.Join(got, " "), "RUN_STARTED") != 1 || strings.Count(strings.Join(got, " "), "TOOL_CALL_RESULT") != 1 || strings.Count(strings.Join(*calls, " "), "create") != 1 {
		t.Fatalf("again: %v (calls %v)", got, *calls)
	}
}

// A held run's next turn starts an agent message of its own, opened afresh.
func TestHeldRunStartsItsNextAgentMessageAfresh(t *testing.T) {
	s, _, calls := cotHub(t, false)
	r, prog := cotRun(s)
	feishuSink{s}.progress(context.Background(), r, time.Second, nil, "")
	s.finishChannelRun(r, &channel.Result{Answer: "先答一部分", Held: true})
	for _, k := range []string{"cot", "cot_msg", "cot_para", "cot_done"} {
		if r.Ext[k] != "" {
			t.Fatalf("%s kept: %v", k, r.Ext)
		}
	}
	*calls = nil
	prog.items = []proto.Item{{ID: "x", Role: "assistant", Kind: proto.KindToolUse, ToolUseID: "tu9", ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"pwd"}`)}}
	feishuSink{s}.progress(context.Background(), r, time.Second, nil, "")
	if got := strings.Join(*calls, " "); !strings.HasPrefix(got, "create RUN_STARTED REASONING_START") {
		t.Fatalf("next turn: %s", got)
	}
}

// While a turn has only words (maybe all of its answer), nothing goes out: no progress card above the agent message
// that comes with the first tool call, and the answer card after it.
func TestFeishuNoProgressCardBeforeTheAgentMessage(t *testing.T) {
	s, posts, calls := cotHub(t, false)
	r, prog := cotRun(s)
	prog.items = []proto.Item{{ID: "w", Role: "assistant", Kind: proto.KindText, Text: "我先看看"}}
	sink := feishuSink{s}
	if d := sink.progressAfter(r); d != 0 {
		t.Fatalf("waits %v", d)
	}
	sink.progress(context.Background(), r, 10*time.Second, nil, "我先看看")
	if len(*posts) != 0 || len(*calls) != 0 {
		t.Fatalf("words only: posts %v, calls %v", *posts, *calls)
	}
	s.chans.mu.Lock()
	prog.items = append(prog.items, proto.Item{ID: "b", Role: "assistant", Kind: proto.KindToolUse, ToolUseID: "tu1", ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"ls"}`)})
	s.chans.mu.Unlock()
	sink.progress(context.Background(), r, 12*time.Second, nil, "")
	if len(*posts) != 0 || len(*calls) == 0 || (*calls)[0] != "create" {
		t.Fatalf("first call: posts %v, calls %v", *posts, *calls)
	}
	s.finishChannelRun(r, &channel.Result{Answer: "结论"})
	if len(*posts) != 1 || (*posts)[0]["update"] == true {
		t.Fatalf("the answer is not a card of its own below the agent message: %v", *posts)
	}
}

// A turn that starts with a tool call hangs it under a paragraph that says so (botmux's shape: a whole paragraph
// before the call), not 「处理中…」.
func TestCotFirstToolCallHasItsParagraph(t *testing.T) {
	s, _, _ := cotHub(t, false)
	r, prog := cotRun(s)
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

// Once Feishu refused an agent message, the next turns get the progress card after a few seconds, words-only ones
// too.
func TestFeishuRefusedAgentMessagesGoToTheCardFromTheStart(t *testing.T) {
	s, posts, _ := cotHub(t, true)
	r, _ := cotRun(s)
	sink := feishuSink{s}
	sink.progress(context.Background(), r, time.Second, nil, "")
	s.finishChannelRun(r, &channel.Result{Answer: "结论"})
	r2 := &channel.Run{ID: "fb_m2", Channel: channel.Feishu, MachineID: "server", SessionID: "c1", ReplyTo: "om_m2", Started: time.Now()}
	prog := &chanLive{mu: newChanMutex(), checking: newChanMutex(), done: make(chan struct{}),
		items: []proto.Item{{ID: "w", Role: "assistant", Kind: proto.KindText, Text: "想想"}}}
	s.chans.mu.Lock()
	s.chans.st.Runs = append(s.chans.st.Runs, r2)
	s.chans.live[r2.ID] = prog
	s.chans.mu.Unlock()
	if d := sink.progressAfter(r2); d != channel.ProgressAfter {
		t.Fatalf("waits %v", d)
	}
	n := len(*posts)
	sink.progress(context.Background(), r2, 6*time.Second, nil, "想想")
	if len(*posts) != n+1 {
		t.Fatalf("no progress card: %v", *posts)
	}
}
