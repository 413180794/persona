package assistant

import (
	"github.com/nwverse/persona/server/internal/feishubot"
	"github.com/nwverse/persona/server/internal/i18n"
)

// botAnswerCards is how many cards one answer takes at most; the rest is in the app.
const botAnswerCards = 5

// botAnswerCard is one card of an answer and what is sent instead when Feishu refuses it.
type botAnswerCard struct {
	card     *feishubot.Card
	fallback string
}

// answerCards lays out a final answer as the bot's cards (feishubot.RenderAnswer: header, sections, tables, link
// buttons, status tag): the process panel on the first when steps is not nil, foot under the last, and a note when
// it was cut short. None for quiet.
func answerCards(l i18n.Lang, answer string, quiet bool, steps []string, foot string) []botAnswerCard {
	if quiet {
		return nil
	}
	a := feishubot.RenderAnswer(answer, botAnswerPiece, botAnswerCards)
	out := make([]botAnswerCard, len(a.Parts))
	for i, p := range a.Parts {
		c := p.Card(answer)
		if i == 0 && steps != nil {
			processPanel(l, c, steps)
		}
		p.Fill(c)
		if i == len(a.Parts)-1 {
			if a.Clipped {
				c.Note(i18n.T(l, "assistant-feishu_cards.only_beginning"))
			}
			c.Note(foot)
		}
		out[i] = botAnswerCard{card: c, fallback: p.Text}
	}
	return out
}
