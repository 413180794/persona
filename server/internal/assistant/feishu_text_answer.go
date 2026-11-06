package assistant

import (
	"context"
	"strings"

	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// botAnswerQuestion lets the owner's next plain message answer a waiting single-question card (IxQuestion,
// one question) in the same Feishu topic, like its press: channel.MatchOption reads a number, ordinal or option name
// (several for a multi-select), and interactions.resolve carries the message's one-use owner action. Forms, plans,
// permissions and terminal dialogs take no typed answer (#4675). A message written inside another agent's topic never
// answers a card outside that topic; a message in the root chat answers the newest card anywhere. False leaves the
// message to the agent; a card no longer pending is closed, saying whether it was answered elsewhere, expired or was
// withdrawn, and the words are swallowed rather than let into a conversation.
func (s *Service) botAnswerQuestion(ctx context.Context, messageID, text, action, root, parent string) bool {
	text = strings.TrimSpace(text)
	if action == "" || text == "" {
		return false
	}
	inTopic := root != "" || parent != ""
	b := &s.fbot
	b.mu.Lock()
	var card botCard
	for _, c := range b.cards {
		if c.Ix.Kind != proto.IxQuestion || len(c.Ix.Questions) != 1 || c.MessageID == "" {
			continue
		}
		if inTopic && !cardInTopic(c, root, parent) {
			continue // words in another agent's topic are not stolen for a card elsewhere
		}
		if card.Ix.ID == "" || c.At.After(card.At) {
			card = c
		}
	}
	b.mu.Unlock()
	if card.Ix.ID == "" {
		return false
	}
	ix := card.Ix
	label, ok := channel.MatchOption(ix.Questions[0], text)
	if !ok {
		return false
	}
	l := s.botLang(ctx)
	var got struct {
		OK     bool   `json:"ok"`
		Status string `json:"status"`
	}
	if err := s.appCall(ctx, AppInteractions, InteractionsGet, map[string]any{"interaction_id": ix.ID}, &got); err != nil {
		s.logf("bots: typed answer %s: %v", ix.ID, err)
		return false
	}
	if !got.OK {
		b.mu.Lock()
		delete(b.cards, ix.ID)
		b.mu.Unlock()
		s.botCardDone(ctx, card.MessageID, ix, endedText(l, got.Status, "assistant-feishu_cards.handled"))
		s.botNote(ctx, messageID, endedText(l, got.Status, "assistant-channels_plugin.answered_elsewhere"))
		return true
	}
	r := proto.Resolution{Answers: map[string]string{ix.Questions[0].Question: label}}
	// Close the card optimistically, as a press does; put it back when the answer could not be handed over and the card
	// still waits (botRestoreCard).
	b.mu.Lock()
	cur, had := b.cards[ix.ID]
	delete(b.cards, ix.ID)
	b.mu.Unlock()
	switch ok, err := s.resolveInteraction(ctx, channel.Feishu, ix.ID, r, action); {
	case err != nil:
		s.logf("bots: typed answer %s: %v", ix.ID, err)
		if had {
			s.botRestoreCard(ctx, ix.ID, cur)
		}
		s.botNote(ctx, messageID, i18n.T(l, "assistant-channels_plugin.answer_not_sent",
			i18n.Args{"error": clip(oneLine(err.Error()), 200)}))
	case !ok:
		s.botCardDone(ctx, card.MessageID, ix, i18n.T(l, "assistant-feishu_cards.handled"))
		s.botNote(ctx, messageID, i18n.T(l, "assistant-channels_plugin.answered_elsewhere"))
	default:
		s.botCardDone(ctx, card.MessageID, ix, resolutionText(l, ix, r))
	}
	return true
}

// cardInTopic says whether a topic message belongs to the card's topic: it replies to the card itself (the card is
// then the topic root), or to the run anchor the card was posted under (the topic's root is the run's first message).
func cardInTopic(c botCard, root, parent string) bool {
	return c.MessageID == root || c.MessageID == parent ||
		(c.Thread != "" && (c.Thread == root || c.Thread == parent))
}
