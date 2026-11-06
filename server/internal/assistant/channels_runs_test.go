package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// queuedRunHub builds a channel run whose own message never reached the conversation: the transcript
// only holds the previous turn (its question and its closing words). status is the conversation's
// status at check time; the run was seen working and has waited past QueuedMost.
func queuedRunHub(t *testing.T, status string) (*Service, *[]map[string]any, *channel.Run) {
	t.Helper()
	f := newFakeHub().withMachine()
	h := &pagedHub{fakeHub: f, page: 4, full: map[string]string{}, items: []proto.Item{
		{ID: "u1", Seq: 1, Role: "user", Kind: proto.KindText, Text: "上一轮的问题"},
		{ID: "a2", Seq: 2, Role: "assistant", Kind: proto.KindText, Text: "上一轮的收尾话"},
	}}
	var sent []map[string]any
	f.answerApp(AppChannels, ChannelsList, func(json.RawMessage) (any, error) {
		return map[string]any{"channels": []channel.View{{ID: "examplechat", Name: "Example Chat", Kind: channel.KindIM, Source: channel.SourcePlugin,
			Installed: true, State: channel.StateConnected, Capabilities: channel.Capabilities{MaxText: 200}}}}, nil
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
	f.setSessions("server", proto.Session{SessionID: "s1", MachineID: "server", Status: status})
	r := &channel.Run{ID: "examplechat_m9", Channel: "examplechat", MachineID: "server", SessionID: "s1", ReplyTo: "m9",
		MsgIDs: []string{"wechat-9"}, Probe: "全新的排队问题", Started: now.Add(-channel.QueuedMost - time.Minute), Seen: true}
	prog := &chanLive{mu: newChanMutex(), checking: newChanMutex(), done: make(chan struct{})}
	s.chans.mu.Lock()
	s.loadChannelStateLocked(context.Background())
	s.chans.st.Runs = append(s.chans.st.Runs, r)
	s.chans.live = map[string]*chanLive{r.ID: prog}
	s.chans.mu.Unlock()
	return s, &sent, r
}

// The previous turn ended leaving its closing words, and the queued message still never showed up
// past QueuedMost: the channel gets a neutral "please resend" note, never the previous turn's text.
func TestCheckChannelRunQueuedPastMostAsksToResend(t *testing.T) {
	s, sent, r := queuedRunHub(t, "")
	s.checkChannelRun(r.ID)
	if len(*sent) != 1 {
		t.Fatalf("got %d messages sent, want 1: %v", len(*sent), *sent)
	}
	text, _ := (*sent)[0]["text"].(string)
	if strings.Contains(text, "上一轮的收尾话") {
		t.Fatalf("the previous turn's words were answered with: %q", text)
	}
	if !strings.Contains(text, i18n.T("", "assistant-channels_runs.not_delivered")) {
		t.Fatalf("the channel got %q, want the resend note", text)
	}
	if n := len(s.channelRuns("examplechat")); n != 0 {
		t.Fatalf("%d runs left after the give-up; want 0", n)
	}
}

// While the conversation is still working the run waits on: a not-found read never ends it.
func TestCheckChannelRunQueuedWhileWorkingKeepsWaiting(t *testing.T) {
	s, sent, r := queuedRunHub(t, proto.StatusWorking)
	s.checkChannelRun(r.ID)
	if len(*sent) != 0 {
		t.Fatalf("a working conversation answered the channel: %v", *sent)
	}
	if n := len(s.channelRuns("examplechat")); n != 1 {
		t.Fatalf("%d runs left; want the run still waiting", n)
	}
}
