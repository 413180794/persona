package assistant

import (
	"context"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/proto"
)

// endAskElsewhere ends ix on the hub (answered in the app): it leaves the pending list, and interaction.ended comes
// when ended is true (else it was missed, and only the pending list says so).
func endAskElsewhere(s *Service, f *fakeHub, ix proto.Interaction, ended bool) {
	f.mu.Lock()
	f.hubCards = nil
	f.mu.Unlock()
	if ended {
		s.InteractionEnded(context.Background(), InteractionEnded{Interaction: ix})
	}
}

// A card answered in the app, then a late "Allow" / number in a text channel: the owner is told it was handled
// elsewhere, and the words do not go into the conversation as a new message (#5093).
func TestChannelReplyToAnAskAnsweredElsewhereIsNotForwarded(t *testing.T) {
	for _, c := range []struct {
		name  string
		ix    proto.Interaction
		reply string
		ended bool
	}{
		{"permission", proto.Interaction{ID: "ix_p", Kind: proto.IxPermission}, "允许", true},
		{"question number", proto.Interaction{ID: "ix_q", Kind: proto.IxQuestion,
			Questions: []proto.Question{{Question: "用哪个？", Options: []proto.QuestionOption{{Label: "甲"}, {Label: "乙"}}}}}, "1", true},
		{"ended missed", proto.Interaction{ID: "ix_m", Kind: proto.IxPermission}, "拒绝", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, f, notes, resolves := newTextAskService(t, c.ix)
			endAskElsewhere(s, f, c.ix, c.ended)
			s.channelReceive("examplechat", channel.Message{ID: "r1", Text: c.reply}, "act")
			if got := sendOnceText(f); got != "" {
				t.Fatalf("the late reply went into the conversation: %q", got)
			}
			if len(*resolves) != 0 {
				t.Fatalf("resolved again: %v", *resolves)
			}
			if !strings.Contains(noteTexts(notes), "已经在别处处理过了") {
				t.Fatalf("the owner was not told it was handled elsewhere: %q", noteTexts(notes))
			}
		})
	}
}

// Words that do not answer the ended ask, and anything after the first message, are ordinary messages.
func TestChannelOrdinaryMessageAfterAnAskEndedElsewhereGoesOn(t *testing.T) {
	ix := proto.Interaction{ID: "ix_p", Kind: proto.IxPermission}
	s, f, _, _ := newTextAskService(t, ix)
	endAskElsewhere(s, f, ix, true)
	s.channelReceive("examplechat", channel.Message{ID: "r1", Text: "帮我查下天气"}, "act")
	if !strings.Contains(sendOnceText(f), "帮我查下天气") {
		t.Fatalf("an ordinary message was held back: %q", sendOnceText(f))
	}
	s.channelReceive("examplechat", channel.Message{ID: "r2", Text: "好"}, "act")
	if !strings.HasSuffix(sendOnceText(f), "好") {
		t.Fatalf("a message after an ordinary one was held back: %q", sendOnceText(f))
	}
}
