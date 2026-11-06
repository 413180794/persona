package assistant

import (
	"context"
	"encoding/json"
	"errors"
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
	return newTextAskServiceWithResolve(t, nil, ixs...)
}

// newTextAskServiceWithResolve is newTextAskService with a custom interactions.resolve answer (nil: always ok).
func newTextAskServiceWithResolve(t *testing.T, resolve appAnswer, ixs ...proto.Interaction) (*Service, *fakeHub, *[]map[string]any, *[]map[string]any) {
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
		if resolve != nil {
			return resolve(p)
		}
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

// askInState is the channel's pending ask with id ixID as stored now (nil when it was deleted).
func askInState(s *Service, ixID string) *channel.Ask {
	s.chans.mu.Lock()
	defer s.chans.mu.Unlock()
	for _, a := range s.chans.st.Asks {
		if a.ID == ixID {
			cp := *a
			return &cp
		}
	}
	return nil
}

// noteTexts joins the note texts sent to the channel.
func noteTexts(notes *[]map[string]any) string {
	var b strings.Builder
	for _, n := range *notes {
		b.WriteString(n["text"].(string))
		b.WriteString("\n")
	}
	return b.String()
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

// A command the bot does not handle ("/new help me…") leaves the text for the conversation without its name. With an ask
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

// A resolve that fails on the wire keeps the ask and the answers already given: the last question is asked again (its
// failed answer is dropped), a re-sent reply finishes it, and the ask is deleted only once the resolve goes through.
func TestChannelAnswerKeptOnResolveError(t *testing.T) {
	ix := proto.Interaction{ID: "ix_multi", Kind: proto.IxQuestion,
		Questions: []proto.Question{{Question: "去哪个城市？"}, {Question: "哪天到？"}}}
	failing := true
	resolve := func(json.RawMessage) (any, error) {
		if failing {
			return nil, errors.New("the hub is restarting")
		}
		return map[string]any{"ok": true}, nil
	}
	s, f, notes, resolves := newTextAskServiceWithResolve(t, resolve, ix)
	recv := func(id, text string) {
		s.channelReceive("examplechat", channel.Message{ID: id, Text: text}, "")
	}

	recv("m1", "北京") // question 1 answered, question 2 is asked next
	recv("m2", "上海") // the final answer: resolve fails
	if a := askInState(s, "ix_multi"); a == nil {
		t.Fatal("the ask was deleted after a failed resolve")
	} else if a.Step != 1 || a.Answers["去哪个城市？"] != "北京" || a.Answers["哪天到？"] != "" {
		t.Fatalf("progress not kept: step=%d answers=%v", a.Step, a.Answers)
	}
	if got := noteTexts(notes); !strings.Contains(got, "刚那条没交上去") {
		t.Fatalf("the failure note does not say to re-send: %q", got)
	}
	if strings.Contains(noteTexts(notes), "还有") {
		t.Fatalf("the failed ask itself was counted as more waiting: %q", noteTexts(notes))
	}
	if text := sendOnceText(f); text != "" {
		t.Fatalf("the failed answer was forwarded as an ordinary message: %q", text)
	}

	failing = false
	recv("m3", "广州") // re-sent for the last question: it resolves and finishes
	if a := askInState(s, "ix_multi"); a != nil {
		t.Fatalf("the ask was kept after a successful resolve: %+v", a)
	}
	if len(*resolves) != 2 {
		t.Fatalf("%d resolves, want 2", len(*resolves))
	}
	answers, _ := (*resolves)[1]["resolution"].(map[string]any)["answers"].(map[string]any)
	if answers["去哪个城市？"] != "北京" || answers["哪天到？"] != "广州" {
		t.Fatalf("the re-sent resolution lost earlier answers: %v", answers)
	}
}

// A single-question ask, a plan and a permission ask have no earlier answers: a failed resolve keeps them at their
// first question / decision, and the re-sent reply goes through.
func TestChannelAnswerKeptOnResolveErrorSingleKinds(t *testing.T) {
	for _, c := range []struct {
		name       string
		ix         proto.Interaction
		first      string
		second     string
		wantSecond func(map[string]any) bool
	}{
		{
			"question", proto.Interaction{ID: "ix_one", Kind: proto.IxQuestion, Questions: []proto.Question{{Question: "去哪个城市？"}}},
			"北京", "上海", func(r map[string]any) bool { return r["answers"].(map[string]any)["去哪个城市？"] == "上海" },
		},
		{
			"plan", proto.Interaction{ID: "ix_plan", Kind: proto.IxPlan},
			"再拆小点", "1", func(r map[string]any) bool { return r["approve"] == true && r["mode"] == "bypassPermissions" },
		},
		{
			"permission", proto.Interaction{ID: "ix_perm", Kind: proto.IxPermission},
			"允许", "允许", func(r map[string]any) bool { return r["allow"] == true },
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			failing := true
			resolve := func(json.RawMessage) (any, error) {
				if failing {
					return nil, errors.New("network down")
				}
				return map[string]any{"ok": true}, nil
			}
			s, _, _, resolves := newTextAskServiceWithResolve(t, resolve, c.ix)
			s.channelReceive("examplechat", channel.Message{ID: "m1", Text: c.first}, "")
			if a := askInState(s, c.ix.ID); a == nil {
				t.Fatal("the ask was deleted after a failed resolve")
			} else if a.Step != 0 || len(a.Answers) != 0 {
				t.Fatalf("a finished ask was not back at its start: step=%d answers=%v", a.Step, a.Answers)
			}
			failing = false
			s.channelReceive("examplechat", channel.Message{ID: "m2", Text: c.second}, "")
			if a := askInState(s, c.ix.ID); a != nil {
				t.Fatal("the ask was kept after a successful resolve")
			}
			r := (*resolves)[1]["resolution"].(map[string]any)
			if !c.wantSecond(r) {
				t.Fatalf("the re-sent resolution is wrong: %v", r)
			}
		})
	}
}

// With another ask waiting in the same channel, a failed resolve names the one still answered by the reply, and the
// "more waiting" line counts only the other ask.
func TestChannelAnswerFailureCountsOnlyOtherAsks(t *testing.T) {
	one := proto.Interaction{ID: "ix_one", Kind: proto.IxPermission}
	two := proto.Interaction{ID: "ix_two", Kind: proto.IxPermission} // asked last: the reply answers this one
	failing := true
	s, _, notes, _ := newTextAskServiceWithResolve(t, func(json.RawMessage) (any, error) {
		if failing {
			return nil, errors.New("network down")
		}
		return map[string]any{"ok": true}, nil
	}, one, two)
	s.channelReceive("examplechat", channel.Message{ID: "m1", Text: "允许"}, "")
	got := noteTexts(notes)
	if !strings.Contains(got, "刚那条没交上去") || !strings.Contains(got, "还有 1 个在等你回复") {
		t.Fatalf("notes should name the failure and one other ask: %q", got)
	}
	if a := askInState(s, "ix_one"); a == nil || askInState(s, "ix_two") == nil {
		t.Fatal("both asks must stay after the failure")
	}
}

// A crash after ReadAnswer saved the final step but before the resolve leaves the question ask at its final step on
// disk: after a restart the reply steps the last question back and finishes it, keeping the earlier answers.
func TestChannelAnswerRecoversFromAFinishedStepOnDisk(t *testing.T) {
	ix := proto.Interaction{ID: "ix_crash", Kind: proto.IxQuestion,
		Questions: []proto.Question{{Question: "去哪个城市？"}, {Question: "哪天到？"}}}
	s, _, _, resolves := newTextAskService(t, ix)
	s.chans.mu.Lock()
	s.loadChannelStateLocked(context.Background())
	a := func() *channel.Ask {
		for _, x := range s.chans.st.Asks {
			if x.ID == "ix_crash" {
				return x
			}
		}
		return nil
	}()
	a.Step = 2
	a.Answers = map[string]string{"去哪个城市？": "北京", "哪天到？": "上海"} // the lost answer, never resolved
	s.saveChannelStateLocked()
	s.chans.mu.Unlock()

	s.channelReceive("examplechat", channel.Message{ID: "m1", Text: "广州"}, "")
	if got := askInState(s, "ix_crash"); got != nil {
		t.Fatalf("the ask was kept: %+v", got)
	}
	if len(*resolves) != 1 {
		t.Fatalf("%d resolves, want 1", len(*resolves))
	}
	answers, _ := (*resolves)[0]["resolution"].(map[string]any)["answers"].(map[string]any)
	if answers["去哪个城市？"] != "北京" || answers["哪天到？"] != "广州" {
		t.Fatalf("the recovered resolution is wrong: %v", answers)
	}
}

// A successful resolve deletes the ask as before, and the owner's reply is confirmed rather than kept waiting.
func TestChannelAnswerDeletedOnResolveOK(t *testing.T) {
	ix := proto.Interaction{ID: "ix_ok", Kind: proto.IxPermission}
	s, f, notes, resolves := newTextAskService(t, ix)
	s.channelReceive("examplechat", channel.Message{ID: "m1", Text: "允许"}, "")
	if a := askInState(s, "ix_ok"); a != nil {
		t.Fatalf("the ask was kept after a successful resolve: %+v", a)
	}
	if len(*resolves) != 1 || (*resolves)[0]["resolution"].(map[string]any)["allow"] != true {
		t.Fatalf("resolution %v", *resolves)
	}
	if !strings.Contains(noteTexts(notes), "已允许") || sendOnceText(f) != "" {
		t.Fatalf("notes %v forwarded %q", *notes, sendOnceText(f))
	}
}

// -race: while the final resolve marshals the answers unlocked, a concurrent heal must not touch that map.
func TestChannelAnswerResolveInFlightNoMapRace(t *testing.T) {
	ix := proto.Interaction{ID: "ix_multi", Kind: proto.IxQuestion,
		Questions: []proto.Question{{Question: "去哪个城市？"}, {Question: "哪天到？"}}}
	proceed := make(chan struct{})
	calls := 0
	resolve := func(json.RawMessage) (any, error) {
		if calls++; calls == 1 {
			<-proceed
			return nil, errors.New("network down")
		}
		return map[string]any{"ok": true}, nil
	}
	s, _, _, resolves := newTextAskServiceWithResolve(t, resolve, ix)
	s.channelReceive("examplechat", channel.Message{ID: "m1", Text: "北京"}, "") // Q1 answered; Q2 asked
	// Widen the marshal window (marshal finishes before the gated handler) so the heal overlaps it.
	s.chans.mu.Lock()
	for i := range 2000 {
		s.chans.st.Asks[0].Answers["pad"+strings.Repeat("x", i)] = strings.Repeat("y", 2048)
	}
	s.chans.mu.Unlock()
	// Synchronized only via the channel mutex: as soon as the final step is taken, heal it like a second message.
	healed := make(chan struct{})
	go func() {
		for {
			s.chans.mu.Lock()
			ready := false
			for _, x := range s.chans.st.Asks {
				if x.ID == "ix_multi" && x.Step == len(x.Questions) {
					rewindLastAnswer(x)
					ready = true
				}
			}
			s.chans.mu.Unlock()
			if ready {
				close(healed)
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	done := make(chan struct{})
	go func() { s.channelReceive("examplechat", channel.Message{ID: "m2", Text: "上海"}, ""); close(done) }()
	<-healed // the racing heal landed while the first resolve was held mid-flight on proceed
	close(proceed)
	<-done
	// Both rewinds can land; two fresh replies (the final answer dropped) finish the ask: Q1 then Q2.
	s.channelReceive("examplechat", channel.Message{ID: "m3", Text: "广州"}, "")
	s.channelReceive("examplechat", channel.Message{ID: "m4", Text: "天津"}, "")
	if a := askInState(s, "ix_multi"); a != nil {
		t.Fatalf("the ask was not finished by the replacement answers: %+v", a)
	}
	answers := (*resolves)[len(*resolves)-1]["resolution"].(map[string]any)["answers"].(map[string]any)
	if answers["去哪个城市？"] != "广州" || answers["哪天到？"] != "天津" {
		t.Fatalf("replacement resolution is wrong: %v", answers)
	}
}
