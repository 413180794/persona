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

// newTextAskService is an assistant with a connected text-only plugin channel "examplechat", ixs pending on the hub
// and already asked in the channel (so a reply answers the latest one). It records the notes sent and the resolutions
// handed to interactions.resolve.
func newTextAskService(t *testing.T, ixs ...proto.Interaction) (*Service, *fakeHub, *[]map[string]any, *[]map[string]any) {
	t.Helper()
	f := newFakeHub().withMachine()
	f.settings = &Settings{MachineID: "server", Main: "m1"}
	var notes, resolves []map[string]any
	f.answerApp(AppChannels, ChannelsList, func(json.RawMessage) (any, error) {
		return map[string]any{"channels": []channel.View{{ID: "examplechat", Name: "Example Chat", Kind: channel.KindIM, Source: channel.SourcePlugin,
			Installed: true, State: channel.StateConnected, Capabilities: channel.Capabilities{Asks: channel.AsksText}}}}, nil
	})
	f.answerApp(AppChannels, ChannelsSend, func(p json.RawMessage) (any, error) {
		var m map[string]any
		_ = json.Unmarshal(p, &m)
		notes = append(notes, m)
		return map[string]any{}, nil
	})
	f.answerApp(AppInteractions, InteractionsResolve, func(p json.RawMessage) (any, error) {
		var m map[string]any
		_ = json.Unmarshal(p, &m)
		resolves = append(resolves, m)
		return map[string]any{"ok": true}, nil
	})
	f.mu.Lock()
	f.hubCards = append(f.hubCards, ixs...)
	f.sessions["server"] = []proto.Session{{SessionID: "m1", MachineID: "server"}}
	f.mu.Unlock()
	s := newTestService(t, f)
	ctx := context.Background()
	now := time.Now()
	s.chans.mu.Lock()
	s.loadChannelStateLocked(ctx)
	for _, ix := range ixs {
		s.chans.st.Asks = append(s.chans.st.Asks, channel.NewAsk("examplechat", ix, now))
	}
	s.saveChannelStateLocked()
	s.chans.mu.Unlock()
	return s, f, &notes, &resolves
}

// sendOnceText is the text of the session.send_once call the channel made ("" when it made none).
func sendOnceText(f *fakeHub) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	text := ""
	for _, c := range f.calls {
		if c.Method != DeviceSessionSendOnce {
			continue
		}
		var p map[string]any
		_ = json.Unmarshal(c.Params, &p)
		text, _ = p["text"].(string)
	}
	return text
}

// A command the bot does not handle ("/new 帮我…") leaves the text for the conversation without its name. With an ask
// waiting, that text answers it: the name must not be read as part of the answer, and the message is answered, not
// swallowed or sent on.
func TestChannelReceiveCommandRestAnswersAsk(t *testing.T) {
	for _, c := range []struct {
		name string
		ix   proto.Interaction
		msg  string
		good func(map[string]any) bool
		note string
	}{
		{
			"plan", proto.Interaction{ID: "ix_plan", Kind: proto.IxPlan}, "/new 帮我查下周天气",
			func(r map[string]any) bool { return r["approve"] == false && r["feedback"] == "帮我查下周天气" },
			"已打回，带上了你的话。",
		},
		{
			"question", proto.Interaction{ID: "ix_q", Kind: proto.IxQuestion, Questions: []proto.Question{{Question: "去哪个城市？"}}},
			"/new 北京",
			func(r map[string]any) bool {
				a, _ := r["answers"].(map[string]any)
				return a["去哪个城市？"] == "北京"
			},
			"已回答。",
		},
		{
			// Extra spaces around the rest are trimmed away with the command name.
			"question spaces", proto.Interaction{ID: "ix_q2", Kind: proto.IxQuestion, Questions: []proto.Question{{Question: "去哪个城市？"}}},
			"/new   北京  ",
			func(r map[string]any) bool {
				a, _ := r["answers"].(map[string]any)
				return a["去哪个城市？"] == "北京"
			},
			"已回答。",
		},
		{
			"permission", proto.Interaction{ID: "ix_p", Kind: proto.IxPermission}, "/new 允许",
			func(r map[string]any) bool { return r["allow"] == true },
			"已允许。",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, f, notes, resolves := newTextAskService(t, c.ix)
			s.channelReceive("examplechat", channel.Message{ID: "m_" + c.name, Text: c.msg}, "")
			if len(*resolves) != 1 {
				t.Fatalf("%d resolutions, want 1: %v", len(*resolves), *resolves)
			}
			r, _ := (*resolves)[0]["resolution"].(map[string]any)
			if !c.good(r) {
				t.Fatalf("resolution %v still carries the command name", r)
			}
			if len(*notes) != 1 || !strings.Contains((*notes)[0]["text"].(string), c.note) {
				t.Fatalf("confirmation note %v, want %q", *notes, c.note)
			}
			if text := sendOnceText(f); text != "" {
				t.Fatalf("the answer was sent on to the conversation: %q", text)
			}
		})
	}
}

// A message without a command, and one with a command the channel bot does not know (the conversation's own), both
// go on exactly as they were typed.
func TestChannelReceiveNonCommandTextGoesOnUnchanged(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct{ name, msg string }{
		{"plain", "帮我写周报"},
		{"unknown command", "/compact 你好"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, f, _, _ := newTextAskService(t) // nothing pending: the message is an ordinary one
			s.channelReceive("examplechat", channel.Message{ID: "m_" + c.name, Text: c.msg}, "")
			text := sendOnceText(f)
			if !strings.Contains(text, c.msg) {
				t.Fatalf("forwarded %q, want it to contain %q", text, c.msg)
			}
		})
	}
	// And channelCommand hands an unknown command back byte for byte.
	s, _, _, _ := newTextAskService(t)
	if handled, rest := s.channelCommand(ctx, "examplechat", channel.Message{ID: "m9", Text: "/compact 你好"}, "/compact 你好"); handled || rest != "/compact 你好" {
		t.Fatalf("unknown command: handled=%v rest=%q", handled, rest)
	}
}
