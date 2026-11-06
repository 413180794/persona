package hub

import (
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// A question's push says its choices and how many questions follow, so the lock screen tells what can be picked (#4293).
func TestQuestionPushShowsItsChoices(t *testing.T) {
	ix := proto.Interaction{ID: "ix_q", Kind: proto.IxQuestion, SessionTitle: "部署",
		Questions: []proto.Question{
			{Question: "这次部署到哪？", Options: []proto.QuestionOption{{Label: "测试环境"}, {Label: "生产环境"}}},
			{Question: "要不要通知群里？", Options: []proto.QuestionOption{{Label: "要"}, {Label: "不要"}}},
		}}
	body, _ := alertOf(t, interactionPayloadIn(i18n.ZhHans, ix, "m1"))["body"].(string)
	if want := "这次部署到哪？\n测试环境 / 生产环境\n还有 1 个问题"; body != want {
		t.Fatalf("body %q, want %q", body, want)
	}
}

// Many choices: the first few, then how many more; long ones are clipped. No choices: the question alone.
func TestQuestionPushKeepsItShort(t *testing.T) {
	opts := []proto.QuestionOption{{Label: strings.Repeat("长", 40)}, {Label: "b"}, {Label: "c"}, {Label: "d"}, {Label: "e"}, {Label: "f"}}
	ix := proto.Interaction{Kind: proto.IxQuestion, Questions: []proto.Question{{Question: "选哪个？", Options: opts}}}
	body, _ := alertOf(t, interactionPayloadIn(i18n.ZhHans, ix, "m1"))["body"].(string)
	lines := strings.Split(body, "\n")
	if len(lines) != 2 || !strings.HasSuffix(lines[1], " / d 等 2 个") || strings.Contains(lines[1], strings.Repeat("长", 21)) {
		t.Fatalf("body %q", body)
	}
	ix.Questions = []proto.Question{{Question: "随便说点什么？"}}
	if body := alertOf(t, interactionPayloadIn(i18n.ZhHans, ix, "m1"))["body"]; body != "随便说点什么？" {
		t.Fatalf("free-text question body %q", body)
	}
	ix.Cwd = "/home/user/.persona/health" // a health conversation's card says nothing of what it is about
	if body := alertOf(t, interactionPayloadIn(i18n.ZhHans, ix, "m1"))["body"]; strings.Contains(body.(string), "随便") {
		t.Fatalf("health card body %q", body)
	}
}

// English says "1 more question", not "1 more questions" (plural forms); Japanese reads its own text.
func TestQuestionPushCountsInEachLanguage(t *testing.T) {
	two := []proto.Question{{Question: "Where to?", Options: []proto.QuestionOption{{Label: "staging"}}}, {Question: "Tell the group?"}}
	three := append(append([]proto.Question(nil), two...), proto.Question{Question: "When?"})
	for _, c := range []struct {
		l    i18n.Lang
		qs   []proto.Question
		want string
	}{
		{i18n.En, two, "Where to?\nstaging\n1 more question"},
		{i18n.En, three, "Where to?\nstaging\n2 more questions"},
		{i18n.Ja, two, "Where to?\nstaging\nほかに 1 問"},
	} {
		ix := proto.Interaction{Kind: proto.IxQuestion, Questions: c.qs}
		if body := alertOf(t, interactionPayloadIn(c.l, ix, "m1"))["body"]; body != c.want {
			t.Errorf("%s, %d questions: body %q, want %q", c.l, len(c.qs), body, c.want)
		}
	}
}

// A label with a line break stays on the choices' line; a blank question leaves no empty first line.
func TestQuestionPushKeepsItsLines(t *testing.T) {
	ix := proto.Interaction{Kind: proto.IxQuestion, Questions: []proto.Question{{Question: "  ",
		Options: []proto.QuestionOption{{Label: "测试\n环境"}, {Label: "生产环境"}}}}}
	if body := alertOf(t, interactionPayloadIn(i18n.ZhHans, ix, "m1"))["body"]; body != "测试 环境 / 生产环境" {
		t.Fatalf("body %q", body)
	}
}
