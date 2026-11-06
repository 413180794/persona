package assistant

import (
	"context"
	"strings"
	"testing"
	"time"
)

// titleTopic takes markdown only for syntax at the start of a line and a paired inline wrap; the same characters
// mid-line are text (#3869). Tag blocks keep coming out whole.
func TestTitleTopic(t *testing.T) {
	for in, want := range map[string]string{
		"修复 C# 编译错误":                     "修复 C# 编译错误",
		"A* 寻路":                          "A* 寻路",
		"跟进 #3471":                       "跟进 #3471",
		"a>b":                            "a>b",
		"### 修复 C#":                      "修复 C#",
		"> 引用风格标题":                       "引用风格标题",
		"- 待办":                           "待办",
		"+ 加号列表":                         "加号列表",
		">x 无空格引用":                       "x 无空格引用",
		"`code` 包裹":                      "code 包裹",
		"**加粗**标题":                       "加粗标题",
		"**没闭合的加粗":                       "**没闭合的加粗", // a lone marker is not a wrap
		"`只有左反引号":                        "`只有左反引号",
		"⏰ 定时任务":                         "定时任务",
		"提醒 ⏰ 开会":                        "提醒 ⏰ 开会", // the clock only goes at the start of a line
		`<at id="u1">某人</at> 跟进`:         "跟进",
		`<at>某人 跟进`:                      `<at>某人 跟进`, // a tag without its closing pair is not a block, and the > stays
		"# A\n- B\n* C#":                 "A B C#",
		"> - 嵌套的引用列表":                    "嵌套的引用列表",
		"- # 修复 #3471":                   "修复 #3471",
		">>>> 多层引用":                      "多层引用",
		"####### 七级井号":                   "####### 七级井号", // seven hashes are not a heading
		"##no-space":                     "##no-space",
		strings.Repeat(">", 20000) + "x": "x", // a long quote chain is one anchored, linear match
		strings.Repeat("修", 20):          strings.Repeat("修", maxTitleTopic),
	} {
		if got := titleTopic(in); got != want {
			t.Errorf("titleTopic(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseTitle(t *testing.T) {
	for _, c := range []struct {
		out, kind, topic string
		ok               bool
	}{
		{"修复 | 搜索框和顶栏重叠", "修复", "搜索框和顶栏重叠", true},
		{"  \n「研究｜竞品定价」\n", "研究", "竞品定价", true},
		{"1101 | 发布 | 周五灰度安排。", "发布", "周五灰度安排", true}, // a date it added anyway, a full stop
		{"闲聊 | 周末去哪", "", "周末去哪", false},              // not a kind
		{"修复 | 搜索框在搜索时和顶栏重叠而且键盘弹起后错位", "修复", "搜索框在搜索时和顶栏重叠而且键盘弹起后错位", false},
		{"修复搜索框", "", "", false},
		{"", "", "", false},
	} {
		kind, topic, ok := parseTitle(c.out)
		if kind != c.kind || topic != c.topic || ok != c.ok {
			t.Errorf("parseTitle(%q) = %q %q %v", c.out, kind, topic, ok)
		}
	}
}

// MMDD is the day in the user's time zone though the hub runs in UTC; a broken answer is asked for once more, then
// cut or replaced by "explore | the first 14 characters".
func TestMakeTitle(t *testing.T) {
	started := time.Date(2026, 11, 1, 16, 30, 0, 0, time.UTC) // 00:30 on 11-02 in Shanghai
	ctx := context.Background()
	first, reply := "搜索的时候搜索框和顶栏叠在一起了，帮我看看", "是 safeAreaBar 和搜索框抽屉一起用导致的……"
	model := func(replies ...string) (*Service, *fakeHub) {
		f := newFakeHub()
		f.settings = &Settings{Timezone: "Asia/Shanghai"}
		f.replies = replies
		return newTestService(t, f), f
	}

	s, f := model("修复 | 搜索框和顶栏重叠")
	if got := s.makeTitle(ctx, started, first, reply); got != "1102 | 修复 | 搜索框和顶栏重叠" {
		t.Fatalf("%q", got)
	}
	if c := f.modelCalls(); len(c) != 1 || !strings.Contains(c[0].User, first) || !strings.Contains(c[0].User, reply) {
		t.Fatalf("the model reads the first exchange: %+v", c)
	}

	s, f = model("Bug fix: search bar", "修复 | 搜索框和顶栏重叠")
	if got := s.makeTitle(ctx, started, first, reply); got != "1102 | 修复 | 搜索框和顶栏重叠" {
		t.Fatalf("retried once: %q", got)
	}
	if c := f.modelCalls(); len(c) != 2 || !strings.Contains(c[1].User, "Bug fix: search bar") {
		t.Fatalf("the retry says what was wrong: %+v", c)
	}

	s, _ = model("修复 | 搜索框在搜索时和顶栏重叠而且键盘弹起后错位")
	if got := s.makeTitle(ctx, started, first, reply); got != "1102 | 修复 | 搜索框在搜索时和顶栏重叠而且" {
		t.Fatalf("a topic still too long is cut: %q", got)
	}

	s, _ = model("随便聊聊")
	if got := s.makeTitle(ctx, started, "<jarvis-context>\n今天\n</jarvis-context>\n**帮我** 看看明天的会议安排和要准备的材料", ""); got != "1102 | 探索 | 帮我 看看明天的会议安排和要" {
		t.Fatalf("the fallback: %q", got)
	}
	if got := s.makeTitle(ctx, started, "<persona-context>\n今天\n</persona-context>\n**帮我** 看看明天的会议安排和要准备的材料", ""); got != "1102 | 探索 | 帮我 看看明天的会议安排和要" {
		t.Fatalf("the fallback past a <persona-context>: %q", got)
	}
	s, f = model()
	f.noModel = true
	if got := s.makeTitle(ctx, started, "", ""); got != "1102 | 探索 | 新对话" {
		t.Fatalf("no model, no message: %q", got)
	}
}
