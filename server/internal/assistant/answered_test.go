package assistant

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/screen"
)

func items(pairs ...string) []answeredItem {
	var out []answeredItem
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, answeredItem{Question: pairs[i], Answer: pairs[i+1]})
	}
	return out
}

func sameItems(t *testing.T, label string, got, want []answeredItem) {
	t.Helper()
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if string(g) != string(w) {
		t.Errorf("%s:\n got %s\nwant %s", label, g, w)
	}
}

// Every question of a card is kept with its answer, in the card's order and with newlines; the summary line stays.
func TestAnsweredMultipleQuestions(t *testing.T) {
	ix := proto.Interaction{Kind: proto.IxQuestion, Questions: []proto.Question{
		{Question: "第一步放在哪里？"}, {Question: "示例插件\n叫什么？"}, {Question: "要不要写文档？"}}}
	r := proto.Resolution{Answers: map[string]string{
		"示例插件\n叫什么？": "example-plugin",
		"第一步放在哪里？":   "放到独立仓库（推荐）\n先只迁一个",
		"旧版的问题":      "旧版的回答",
	}}
	sameItems(t, "items", answeredItems("", ix, r), items(
		"第一步放在哪里？", "放到独立仓库（推荐）\n先只迁一个",
		"示例插件\n叫什么？", "example-plugin",
		"要不要写文档？", "没回答",
		"旧版的问题", "旧版的回答"))
	if q, a := interactionQuestion("", ix), resolutionAnswer("", ix, r); q != "第一步放在哪里？" || a != "放到独立仓库（推荐） 先只迁一个；example-plugin；旧版的回答" {
		t.Errorf("summary = %q / %q", q, a)
	}
	sameItems(t, "dismissed", answeredItems("", ix, proto.Resolution{Dismiss: true}), items(
		"第一步放在哪里？", "忽略了", "示例插件\n叫什么？", "忽略了", "要不要写文档？", "忽略了"))
}

// A long answer is kept in full up to a text's limit; the record as a whole stays under its total.
func TestAnsweredLongTexts(t *testing.T) {
	long := strings.Repeat("长回答。", 600) // 2400 runes: kept whole
	ix := proto.Interaction{Kind: proto.IxQuestion, Questions: []proto.Question{{Question: "怎么做？"}}}
	got := answeredItems("", ix, proto.Resolution{Answers: map[string]string{"怎么做？": long}})
	if len(got) != 1 || got[0].Answer != long {
		t.Fatalf("long answer clipped: %d runes", utf8.RuneCountInString(got[0].Answer))
	}
	if a := resolutionAnswer("", ix, proto.Resolution{Answers: map[string]string{"怎么做？": long}}); utf8.RuneCountInString(a) != answeredLineMax+1 {
		t.Errorf("summary is %d runes", utf8.RuneCountInString(a))
	}

	huge := strings.Repeat("字", answeredTextMax*2)
	var qs []proto.Question
	answers := map[string]string{}
	for i := 0; i < 30; i++ {
		q := strings.Repeat("问", 10) + string(rune('A'+i))
		qs = append(qs, proto.Question{Question: q})
		answers[q] = huge
	}
	got = answeredItems("", proto.Interaction{Kind: proto.IxQuestion, Questions: qs}, proto.Resolution{Answers: answers})
	total := 0
	for _, it := range got {
		total += utf8.RuneCountInString(it.Question) + utf8.RuneCountInString(it.Answer)
		if n := utf8.RuneCountInString(it.Answer); n > answeredTextMax+1 {
			t.Fatalf("an answer of %d runes", n)
		}
	}
	if total > answeredTotalMax+len(got) || len(got) == 0 || len(got) > answeredItemsMax || !strings.HasSuffix(got[len(got)-1].Answer, "…") {
		t.Fatalf("%d items, %d runes", len(got), total)
	}
}

// A plan's record has the plan itself and how it was approved, or why it was not.
func TestAnsweredPlan(t *testing.T) {
	yes, no := true, false
	ix := proto.Interaction{Kind: proto.IxPlan, ToolName: "ExitPlanMode", Plan: "## 计划\n1. 改服务端\n2. 改客户端\n"}
	sameItems(t, "approved", answeredItems("", ix, proto.Resolution{Approve: &yes, Mode: "acceptEdits"}),
		items("批准这份计划？\n\n## 计划\n1. 改服务端\n2. 改客户端", "批准了 · 自动接受修改"))
	sameItems(t, "rejected", answeredItems("", ix, proto.Resolution{Approve: &no, Feedback: "先别动客户端\n只改服务端"}),
		items("批准这份计划？\n\n## 计划\n1. 改服务端\n2. 改客户端", "拒绝了：\n先别动客户端\n只改服务端"))
	if a := resolutionAnswer("", ix, proto.Resolution{Approve: &no, Feedback: "先别动客户端\n只改服务端"}); a != "拒绝了：先别动客户端 只改服务端" {
		t.Errorf("summary = %q", a)
	}
	sameItems(t, "no plan text", answeredItems("", proto.Interaction{Kind: proto.IxPlan}, proto.Resolution{Approve: &yes}),
		items("批准计划", "批准了"))
}

// A permission's record says what the tool would do and why; a rejection keeps its reason, a password field only
// that it was filled in.
func TestAnsweredPermission(t *testing.T) {
	yes, no := true, false
	ix := proto.Interaction{Kind: proto.IxPermission, ToolName: "Bash", Summary: "npm publish\n--tag next", Reason: "发布预览版",
		Fields: []proto.Field{{Key: "otp", Label: "验证码"}, {Key: "pw", Label: "口令", Secret: true}, {Key: "note", Label: "备注"}}}
	sameItems(t, "allowed", answeredItems("", ix, proto.Resolution{Allow: &yes, Always: true,
		Values: map[string]string{"pw": "hunter2", "note": "只发预览", "otp": "123456", "extra": "x"}}),
		items("使用 Bash\nnpm publish\n--tag next\n原因：发布预览版",
			"批准了 · 以后同类不再问\n验证码："+answeredSecret("")+"\n口令："+answeredSecret("")+"\n备注：只发预览\nextra："+answeredSecret("")))
	sameItems(t, "denied", answeredItems("", ix, proto.Resolution{Allow: &no, Message: "换个时间再发"}),
		items("使用 Bash\nnpm publish\n--tag next\n原因：发布预览版", "拒绝了：\n换个时间再发"))
	b, _ := json.Marshal(answeredItems("", ix, proto.Resolution{Allow: &yes, Values: map[string]string{"pw": "hunter2", "otp": "123456"}}))
	if strings.Contains(string(b), "hunter2") || strings.Contains(string(b), "123456") {
		t.Fatalf("a secret was kept: %s", b)
	}
}

// A terminal dialog's record names the options picked and the text typed into one; never a password.
func TestAnsweredScreen(t *testing.T) {
	d := &screen.Dialog{Detail: "rm -rf build", Question: "Do you want to proceed?", Options: []screen.Option{
		{Key: "1", Label: "Yes"}, {Key: "2", Label: "No"}, {Key: "3", Label: "Type something.", Kind: "type_something"}}}
	ix := proto.Interaction{Kind: proto.IxScreen, Summary: "Do you want to proceed?", Dialog: d}
	sameItems(t, "option", answeredItems("", ix, proto.Resolution{Option: "1"}), items("rm -rf build\nDo you want to proceed?", "Yes"))
	sameItems(t, "typed", answeredItems("", ix, proto.Resolution{Option: "3", Text: "先备份再删"}), items("rm -rf build\nDo you want to proceed?", "先备份再删"))
	if a := resolutionAnswer("", ix, proto.Resolution{Option: "2"}); a != "选了 No" {
		t.Errorf("summary = %q", a)
	}
	if a := resolutionAnswer("", ix, proto.Resolution{Option: "3", Text: "先备份\n再删"}); a != "选了 先备份 再删" {
		t.Errorf("typed summary = %q", a)
	}
	secret := proto.Interaction{Kind: proto.IxScreen, Summary: "Password:", Secret: true}
	got := answeredItems("", secret, proto.Resolution{Text: "hunter2", Keys: []string{"enter"}})
	if b, _ := json.Marshal(got); strings.Contains(string(b), "hunter2") {
		t.Fatalf("a password was kept: %s", b)
	}
	sameItems(t, "secret", got, items("Password:", "输入了密码（不保存）"))
}

// An MCP form's record has its fields by title; a password-like one only says it was filled in.
func TestAnsweredElicitation(t *testing.T) {
	ix := proto.Interaction{Kind: proto.IxElicitation, Elicitation: &proto.Elicitation{Mode: "form", Message: "登录示例服务",
		RequestedSchema: json.RawMessage(`{"properties":{"user":{"title":"用户名"},"pass":{"title":"密码","format":"password"},"days":{"title":"天数"}}}`)}}
	r := proto.Resolution{Action: "accept", Content: map[string]any{"user": "me", "pass": "hunter2", "days": 3.0}}
	sameItems(t, "accept", answeredItems("", ix, r), items("登录示例服务", "提交了\n天数：3\n密码："+answeredSecret("")+"\n用户名：me"))
	if q, a := interactionQuestion("", ix), resolutionAnswer("", ix, r); q != "登录示例服务" || a != "提交了" {
		t.Errorf("summary = %q / %q", q, a)
	}
	sameItems(t, "decline", answeredItems("", ix, proto.Resolution{Action: "decline"}), items("登录示例服务", "拒绝了"))
}

// A record from before #1231 (question and answer only) still reads, with no items; a new one keeps both.
func TestAnsweredBodyCompat(t *testing.T) {
	var old answeredBody
	if err := json.Unmarshal([]byte(`{"question":"使用 Bash","answer":"批准了"}`), &old); err != nil || old.Items != nil || old.Answer != "批准了" {
		t.Fatalf("old body = %+v %v", old, err)
	}
	b, _ := json.Marshal(answeredBody{Question: "怎么改？", Answer: "B", Items: items("怎么改？", "B")})
	if string(b) != `{"question":"怎么改？","answer":"B","items":[{"question":"怎么改？","answer":"B"}]}` {
		t.Fatalf("new body = %s", b)
	}
}

func TestToolDisplayNameShowsPersonaServer(t *testing.T) {
	for in, want := range map[string]string{
		"mcp__persona__mail_send":         "Persona · mail send",
		"mcp__plugin_persona__agent_read": "Persona · agent read",
		"mcp__persona":                    "Persona",
		"mcp__github__create_issue":       "github · create issue",
		"mcp__persona_extra__x":           "persona_extra · x",
		"Bash":                            "Bash",
		"":                                "",
	} {
		if got := toolDisplayName(in); got != want {
			t.Errorf("toolDisplayName(%q) = %q, want %q", in, got, want)
		}
	}
}
