package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/proto"
)

// endedRunHub is a run whose turn is over (not working) and whose transcript ends on a tool result: its last words
// are not written yet. clock moves the service's clock.
func endedRunHub(t *testing.T) (*Service, *pagedHub, *[]map[string]any, *channel.Run, func(time.Duration)) {
	t.Helper()
	f := newFakeHub().withMachine()
	h := &pagedHub{fakeHub: f, page: 50, full: map[string]string{}, items: []proto.Item{
		{ID: "u1", Seq: 1, Role: "user", Kind: proto.KindText, Text: "查一下", ClientMsgID: "examplechat-m9"},
		{ID: "t2", Seq: 2, Role: "assistant", Kind: proto.KindToolUse, ToolUseID: "tu1", ToolName: "Bash"},
		{ID: "r3", Seq: 3, Role: "user", Kind: proto.KindToolResult, ToolUseID: "tu1", Text: "ok"},
	}}
	var sent []map[string]any
	f.answerApp(AppChannels, ChannelsList, func(json.RawMessage) (any, error) {
		return map[string]any{"channels": []channel.View{{ID: "examplechat", Name: "Example Chat", Kind: channel.KindIM, Source: channel.SourcePlugin,
			Installed: true, State: channel.StateConnected, Capabilities: channel.Capabilities{MaxText: 2000}}}}, nil
	})
	f.answerApp(AppChannels, ChannelsSend, func(p json.RawMessage) (any, error) {
		var m map[string]any
		_ = json.Unmarshal(p, &m)
		sent = append(sent, m)
		return map[string]any{}, nil
	})
	s := withSettings(t, f, New(Options{Hub: h, Store: func() Store { return &memStore{} }, Logf: testLogf(t)}))
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	s.chans.now = func() time.Time { return now }
	f.setSessions("server", proto.Session{SessionID: "s1", MachineID: "server", Assistant: true})
	r := &channel.Run{ID: "examplechat_m9", Channel: "examplechat", MachineID: "server", SessionID: "s1", ReplyTo: "m9",
		MsgIDs: []string{"examplechat-m9"}, Started: now.Add(-time.Minute), Seen: true}
	prog := &chanLive{mu: newChanMutex(), checking: newChanMutex(), done: make(chan struct{})}
	s.chans.mu.Lock()
	s.loadChannelStateLocked(context.Background())
	s.chans.st.Runs = append(s.chans.st.Runs, r)
	s.chans.live = map[string]*chanLive{r.ID: prog}
	s.chans.mu.Unlock()
	return s, h, &sent, r, func(d time.Duration) { now = now.Add(d) }
}

func sentText(sent []map[string]any) string {
	var out []string
	for _, m := range sent {
		t, _ := m["text"].(string)
		out = append(out, t)
	}
	return strings.Join(out, "\n")
}

// A turn reported over before its last words are in the transcript waits for them (the owner's 20:52 turn: tool
// calls, then a long answer, shown as "no text answer").
func TestTurnOverBeforeItsWordsAreWrittenWaitsForThem(t *testing.T) {
	s, h, sent, r, clock := endedRunHub(t)
	s.checkChannelRun(r.ID)
	if len(*sent) != 0 || sendOnceText(h.fakeHub) != "" {
		t.Fatalf("answered before the words were in: %v", *sent)
	}
	h.items = append(h.items, proto.Item{ID: "a4", Seq: 4, Role: "assistant", Kind: proto.KindText, Text: "长长的结论"})
	clock(2 * time.Second)
	s.checkChannelRun(r.ID)
	if got := sentText(*sent); !strings.Contains(got, "长长的结论") || strings.Contains(got, "没有文字回答") {
		t.Fatalf("answer %q", got)
	}
}

// Still nothing after a few seconds: the conversation is asked once for its answer, and that answer goes out.
func TestNoAnswerAsksTheConversationAgain(t *testing.T) {
	s, h, sent, r, clock := endedRunHub(t)
	s.checkChannelRun(r.ID)
	clock(channel.AnswerSettle + time.Second)
	s.checkChannelRun(r.ID)
	ask := sendOnceText(h.fakeHub)
	if len(*sent) != 0 || !strings.Contains(ask, "最终回答") || !r.Asked() {
		t.Fatalf("not asked again: sent %v, ask %q, run %+v", *sent, ask, r)
	}
	h.items = append(h.items,
		proto.Item{ID: "u5", Seq: 5, Role: "user", Kind: proto.KindText, Text: ask, ClientMsgID: r.MsgIDs[0]},
		proto.Item{ID: "a6", Seq: 6, Role: "assistant", Kind: proto.KindText, Text: "补上的回答"})
	clock(30 * time.Second)
	s.checkChannelRun(r.ID)
	if got := sentText(*sent); got != "补上的回答" && !strings.Contains(got, "补上的回答") {
		t.Fatalf("answer %q", got)
	}
}

// A placeholder ("No response requested.") is no answer; asked again and still a placeholder, the owner is told why.
func TestPlaceholderAnswerSaysWhy(t *testing.T) {
	s, h, sent, r, clock := endedRunHub(t)
	h.items = append(h.items, proto.Item{ID: "a4", Seq: 4, Role: "assistant", Kind: proto.KindText, Text: "No response requested."})
	s.checkChannelRun(r.ID)
	clock(channel.AnswerSettle + time.Second)
	s.checkChannelRun(r.ID)
	if !r.Asked() || len(*sent) != 0 {
		t.Fatalf("placeholder answered: %v", *sent)
	}
	h.items = append(h.items,
		proto.Item{ID: "u5", Seq: 5, Role: "user", Kind: proto.KindText, Text: "请把", ClientMsgID: r.MsgIDs[0]},
		proto.Item{ID: "a6", Seq: 6, Role: "assistant", Kind: proto.KindText, Text: "No response requested."})
	clock(30 * time.Second)
	s.checkChannelRun(r.ID)
	clock(channel.AnswerSettle + time.Second)
	s.checkChannelRun(r.ID)
	got := sentText(*sent)
	if !strings.Contains(got, "这一轮没产出回答，原因：") || !strings.Contains(got, "No response requested.") {
		t.Fatalf("answer %q", got)
	}
	if strings.Count(sendOnceText(h.fakeHub), "最终回答") > 1 {
		t.Fatalf("asked twice")
	}
}

func TestPlaceholder(t *testing.T) {
	for _, s := range []string{"No response requested.", " no response requested ", "(no content)"} {
		if !channel.Placeholder(s) {
			t.Fatalf("%q", s)
		}
	}
	if channel.Placeholder("No response requested, but here is the answer") {
		t.Fatal("an answer counted as a placeholder")
	}
}

// A normal end (the answer, then the line with the turn's duration) goes out at once, not after the settle wait.
func TestAnswerBeforeTheDurationLineGoesOutAtOnce(t *testing.T) {
	s, h, sent, r, _ := endedRunHub(t)
	h.items = append(h.items,
		proto.Item{ID: "a4", Seq: 4, Role: "assistant", Kind: proto.KindText, Text: "结论"},
		proto.Item{ID: "d5", Seq: 5, Role: "system", Kind: "turn_duration", TurnDuration: &proto.TurnDuration{}})
	s.checkChannelRun(r.ID)
	if got := sentText(*sent); !strings.Contains(got, "结论") {
		t.Fatalf("waited: %q", got)
	}
}

// Read as unsettled, then working again: the next end waits afresh rather than counting the old wait.
func TestSettleWaitStartsAgainAfterWorking(t *testing.T) {
	s, h, sent, r, clock := endedRunHub(t)
	s.checkChannelRun(r.ID) // ends on a tool result: waits
	h.fakeHub.setSessions("server", proto.Session{SessionID: "s1", MachineID: "server", Assistant: true, Status: proto.StatusWorking})
	clock(time.Minute)
	s.checkChannelRun(r.ID)
	h.fakeHub.setSessions("server", proto.Session{SessionID: "s1", MachineID: "server", Assistant: true})
	clock(time.Second)
	s.checkChannelRun(r.ID)
	if len(*sent) != 0 || r.Asked() {
		t.Fatalf("the old wait counted: sent %v, asked %v", *sent, r.Asked())
	}
}
