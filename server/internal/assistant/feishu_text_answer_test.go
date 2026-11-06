package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/feishubot"
	"github.com/nwverse/persona/server/internal/proto"
)

// textAnswerBot pairs the Feishu bot with ixs pending and records card posts, chat notes and resolves.
func textAnswerBot(t *testing.T, resolve appAnswer, ixs ...proto.Interaction) (*Service, *fakeHub, *[]map[string]any, *[]map[string]any, *[]map[string]any) {
	t.Helper()
	f, posts := feishuHub("ou_a", time.Unix(100, 0))
	f.settings = &Settings{MachineID: "server", Main: "m1"}
	f.withQueue(ixs...)
	var notes, resolves []map[string]any
	f.answerApp(AppFeishu, FeishuText, func(p json.RawMessage) (any, error) {
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
	s := newTestService(t, f)
	_, _ = s.feishuStatus(context.Background())
	s.fbot.cards = map[string]botCard{}
	return s, f, posts, &notes, &resolves
}
func seedCard(s *Service, ix proto.Interaction) {
	s.fbot.cards[ix.ID] = botCard{MessageID: "om_" + ix.ID, Ix: ix, At: time.Unix(1000, 0)}
}
func seedThreadCard(s *Service, ix proto.Interaction, thread string, at int64) {
	s.fbot.cards[ix.ID] = botCard{MessageID: "om_" + ix.ID, Thread: thread, Ix: ix, At: time.Unix(at, 0)}
}
func qCard(id string, multi bool, opts ...string) proto.Interaction {
	qos := make([]proto.QuestionOption, len(opts))
	for i, o := range opts {
		qos[i] = proto.QuestionOption{Label: o}
	}
	return proto.Interaction{ID: id, MachineID: "server", SessionID: "m1", Kind: proto.IxQuestion,
		Questions: []proto.Question{{Question: "选哪个？", MultiSelect: multi, Options: qos}}}
}
func noteText(ns *[]map[string]any) string {
	var b strings.Builder
	for _, n := range *ns {
		b.WriteString(n["text"].(string))
	}
	return b.String()
}

// A typed option resolves like the press (same id, answers, one-use owner action); the latest posted card wins.
func TestTypedAnswerResolvesTheCard(t *testing.T) {
	for _, c := range []struct {
		text, want, id string
		multi          bool
	}{
		{"2", "上海", "ix_a", false}, {"广州", "广州", "ix_b", false}, {"1、3", "北京, 广州", "ix_c", true},
	} {
		ix := qCard(c.id, c.multi, "北京", "上海", "广州")
		s, _, _, _, rs := textAnswerBot(t, nil, ix)
		seedCard(s, ix)
		if !s.botAnswerQuestion(context.Background(), "om_r", c.text, "oa_1", "", "") || len(*rs) != 1 {
			t.Fatalf("%q not resolved: %v", c.text, *rs)
		}
		r := (*rs)[0]
		ans, _ := r["resolution"].(map[string]any)["answers"].(map[string]any)
		if r["interaction_id"] != c.id || r["action"] != "oa_1" || ans["选哪个？"] != c.want {
			t.Fatalf("%q resolve %v", c.text, r)
		}
	}
	old, latest := qCard("ix_old", false, "甲", "乙"), qCard("ix_new", false, "子", "丑")
	s, _, _, _, rs := textAnswerBot(t, nil, old, latest)
	seedCard(s, old)
	s.fbot.cards["ix_new"] = botCard{MessageID: "om_ix_new", Ix: latest, At: time.Unix(2000, 0)}
	if !s.botAnswerQuestion(context.Background(), "om_r", "2", "oa_1", "", "") || (*rs)[0]["interaction_id"] != "ix_new" {
		t.Fatalf("the latest card was not the one answered: %v", *rs)
	}
	if _, ok := s.fbot.cards["ix_old"]; !ok {
		t.Fatal("the older card was closed")
	}
	// The card says a plain reply works for one question with options, not for a free-text question.
	hint := "也可以直接在聊天里回复序号或选项名"
	if js := s.interactionCard(context.Background(), old).JSON(); !strings.Contains(js, hint) {
		t.Fatalf("no text-answer hint: %s", js)
	}
	freeQ := proto.Interaction{Kind: proto.IxQuestion, Questions: []proto.Question{{Question: "为什么？"}}}
	if strings.Contains(s.interactionCard(context.Background(), freeQ).JSON(), hint) {
		t.Fatal("a free-text question promises a typed answer")
	}
}

// Non-options/no-action/plan/form messages stay for the agent; over-elsewhere handled and swallowed; failed resolve keeps waiting.
func TestTypedAnswerGuards(t *testing.T) {
	free, plan := qCard("ix_q", false, "北京", "上海"), proto.Interaction{ID: "ix_plan", Kind: proto.IxPlan}
	form := proto.Interaction{ID: "ix_form", Kind: proto.IxQuestion, Questions: []proto.Question{{Question: "一"}, {Question: "二"}}}
	for _, c := range []struct {
		name, text, action, note string
		card                     proto.Interaction
		pending                  bool
		resolve                  appAnswer
		taken, closed            bool
		resolves                 int
	}{
		{"free text", "帮我查天气", "oa_1", "", free, true, nil, false, false, 0},
		{"no action", "2", "", "", free, true, nil, false, false, 0},
		{"plan", "2", "oa_1", "", plan, true, nil, false, false, 0},
		{"form", "1", "oa_1", "", form, true, nil, false, false, 0},
		{"ended elsewhere", "2", "oa_1", "已经在别处处理过了", free, false, nil, true, true, 0},
		{"resolve error", "2", "oa_1", "没交上去", free, true, func(json.RawMessage) (any, error) { return nil, errors.New("hub restarted") }, true, false, 1},
		{"resolve race", "2", "oa_1", "已经在别处处理过了", free, true, func(json.RawMessage) (any, error) { return map[string]any{"ok": false}, nil }, true, true, 1},
	} {
		s, _, posts, notes, rs := textAnswerBot(t, c.resolve, map[bool][]proto.Interaction{true: {free}}[c.pending]...)
		seedCard(s, c.card)
		if got := s.botAnswerQuestion(context.Background(), "om_r", c.text, c.action, "", ""); got != c.taken || len(*rs) != c.resolves {
			t.Fatalf("%s: taken %v, resolves %v", c.name, got, *rs)
		}
		_, waits := s.fbot.cards[c.card.ID]
		if waits == c.closed || (c.note != "" && !strings.Contains(noteText(notes), c.note)) || c.closed != (len(*posts) == 1) {
			t.Fatalf("%s: waits %v posts %d notes %q", c.name, waits, len(*posts), noteText(notes))
		}
	}
}

// A message inside an agent's topic only answers a card waiting in that topic; a card elsewhere keeps waiting and the
// words go to that agent. In the root chat the newest card anywhere is still answered.
func TestTypedAnswerStaysInItsTopic(t *testing.T) {
	rootCard := qCard("ix_root", false, "甲", "乙")
	topicCard := qCard("ix_topic", false, "子", "丑")
	otherCard := qCard("ix_other", false, "寅", "卯")

	// Typed in agent B's topic, where no card waits: cards in the root chat and in other topics are not touched.
	s, _, _, _, rs := textAnswerBot(t, nil, rootCard, topicCard, otherCard)
	seedCard(s, rootCard)
	seedThreadCard(s, topicCard, "om_topic_root", 2000)
	seedThreadCard(s, otherCard, "om_other_root", 3000) // newest overall, but in a different topic
	if s.botAnswerQuestion(context.Background(), "om_r", "2", "oa_1", "om_b_root", "") {
		t.Fatal("a topic with no card there must not take the words")
	}
	if len(*rs) != 0 {
		t.Fatalf("nothing may be resolved: %v", *rs)
	}
	for _, id := range []string{"ix_root", "ix_topic", "ix_other"} {
		if _, ok := s.fbot.cards[id]; !ok {
			t.Fatalf("card %s was closed", id)
		}
	}

	// A card inside that topic is the one answered, even though a card elsewhere is newer.
	s, _, _, _, rs = textAnswerBot(t, nil, topicCard, otherCard)
	seedThreadCard(s, topicCard, "om_topic_root", 1000)
	seedThreadCard(s, otherCard, "om_other_root", 2000)
	if !s.botAnswerQuestion(context.Background(), "om_r", "1", "oa_1", "om_topic_root", "om_topic_root") ||
		len(*rs) != 1 || (*rs)[0]["interaction_id"] != "ix_topic" {
		t.Fatalf("the topic's own card was not answered: %v", *rs)
	}
	if _, ok := s.fbot.cards["ix_other"]; !ok {
		t.Fatal("the other topic's card was closed")
	}

	// Replying directly to a card posted as a topic root answers it (root id is the card's own message).
	s, _, _, _, rs = textAnswerBot(t, nil, rootCard)
	seedCard(s, rootCard)
	if !s.botAnswerQuestion(context.Background(), "om_r", "2", "oa_1", "om_ix_root", "") ||
		len(*rs) != 1 || (*rs)[0]["interaction_id"] != "ix_root" {
		t.Fatalf("the reply to the card itself was not taken: %v", *rs)
	}
}

// End to end through the handler: the owner's text answers the waiting card and starts no conversation.
func TestFeishuMessageTypedAnswer(t *testing.T) {
	ix := qCard("ix_q", false, "北京", "上海", "广州")
	s, f, _, _, rs := textAnswerBot(t, nil, ix)
	seedCard(s, ix)
	var ev feishubot.MessageEvent
	ev.Message.MessageID, ev.Message.MessageType, ev.Message.Content = "om_in", "text", `{"text":"第二个"}`
	st, _ := s.feishuStatus(context.Background())
	s.feishuMessage(ev, st, "oa_1")
	if len(*rs) != 1 || (*rs)[0]["interaction_id"] != "ix_q" || sendOnceText(f) != "" {
		t.Fatalf("resolves %v, leaked into chat %q", *rs, sendOnceText(f))
	}
}
