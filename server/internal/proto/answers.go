package proto

import (
	"sort"
	"strings"
)

// QuestionAnswer is one question of a card and what the user answered.
type QuestionAnswer struct {
	Question, Answer string
}

// HasDuplicateQuestionText reports whether two questions have the same text after trimming surrounding whitespace.
// Answers are keyed by the question text (Resolution.Answers), so alike-worded questions could not each carry one.
func HasDuplicateQuestionText(qs []Question) bool {
	seen := make(map[string]bool, len(qs))
	for _, q := range qs {
		t := strings.TrimSpace(q.Question)
		if seen[t] {
			return true
		}
		seen[t] = true
	}
	return false
}

// QuestionAnswers pairs a question card's questions with the answers, in the card's order; answers keyed by a
// question the card does not have (an older app's) follow, sorted. Shared by the assistant's record of an answered
// card and the hub's todo decisions (#1618).
func QuestionAnswers(ix Interaction, r Resolution) []QuestionAnswer {
	var out []QuestionAnswer
	seen := map[string]bool{}
	for _, q := range ix.Questions {
		if a, ok := r.Answers[q.Question]; ok {
			out = append(out, QuestionAnswer{Question: q.Question, Answer: a})
			seen[q.Question] = true
		}
	}
	var rest []string
	for q := range r.Answers {
		if !seen[q] {
			rest = append(rest, q)
		}
	}
	sort.Strings(rest)
	for _, q := range rest {
		out = append(out, QuestionAnswer{Question: q, Answer: r.Answers[q]})
	}
	return out
}
