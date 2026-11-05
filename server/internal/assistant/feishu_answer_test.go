package assistant

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
)

// cardTags is the element tags of a card and its JSON.
func cardTags(t *testing.T, c botAnswerCard) ([]string, string) {
	t.Helper()
	js := c.card.JSON()
	var m struct {
		Body struct {
			Elements []map[string]any `json:"elements"`
		} `json:"body"`
	}
	if err := json.Unmarshal([]byte(js), &m); err != nil {
		t.Fatal(err)
	}
	var tags []string
	for _, e := range m.Body.Elements {
		tags = append(tags, e["tag"].(string))
	}
	return tags, js
}

func TestAnswerCards(t *testing.T) {
	if got := answerCards(i18n.ZhHans, "# 标题\n正文", true, []string{"step"}, "foot"); got != nil {
		t.Fatalf("quiet: %v", got)
	}

	const foot = "FOOT-MARK"
	long := "# 长\n" + strings.Repeat("## 节\n"+strings.Repeat("字", 3000)+"\n", 4)
	cards := answerCards(i18n.ZhHans, long, false, []string{"运行 · ls"}, foot)
	if len(cards) < 2 {
		t.Fatalf("%d cards", len(cards))
	}
	for i, c := range cards {
		tags, js := cardTags(t, c)
		last := i == len(cards)-1
		if strings.Contains(js, foot) != last || strings.Count(js, foot) > 1 {
			t.Fatalf("card %d: foot %d times", i, strings.Count(js, foot))
		}
		if (i == 0) != (len(tags) > 0 && tags[0] == "collapsible_panel") || strings.Count(strings.Join(tags, " "), "collapsible_panel") > 1 {
			t.Fatalf("card %d: tags %v", i, tags)
		}
		if c.fallback == "" {
			t.Fatalf("card %d: no fallback", i)
		}
	}

	cards = answerCards(i18n.ZhHans, "正文", false, nil, foot)
	tags, js := cardTags(t, cards[0])
	if len(cards) != 1 || strings.Contains(js, "collapsible_panel") || strings.Count(js, foot) != 1 {
		t.Fatalf("steps nil: tags %v", tags)
	}
}
