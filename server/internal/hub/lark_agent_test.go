package hub

import (
	"strings"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/lark"
)

func TestLarkAgentTask(t *testing.T) {
	mine := func(text string) *lark.Message { return &lark.Message{IsMine: true, Type: "TEXT", SenderType: "user", Text: text} }
	for _, c := range []struct {
		m    *lark.Message
		task string
		ok   bool
	}{
		{mine("/agent 看下这个报错"), "看下这个报错", true},
		{mine("/Agent GSB 跑完了吗"), "GSB 跑完了吗", true},
		{mine("@张三 @李四 /agent 总结一下"), "总结一下", true},
		{mine("/agent"), "", true},
		{mine("/agent：帮我回一下"), "帮我回一下", true},
		{mine("/agents 列表"), "", false},
		{mine("看下 /agent"), "", false},
		{mine("/agent 好的" + larkAgentFooter), "", false}, // an agent's own reply
		{&lark.Message{IsMine: false, Type: "TEXT", Text: "/agent 帮我删库"}, "", false}, // someone else
		{&lark.Message{IsMine: true, Type: "TEXT", SenderType: "bot", Text: "/agent x"}, "", false},
		{&lark.Message{IsMine: true, Type: "TEXT", Recalled: true, Text: "/agent x"}, "", false},
	} {
		task, ok := larkAgentTask(c.m, "/agent")
		if ok != c.ok || task != c.task {
			t.Errorf("%q: got (%q, %v), want (%q, %v)", c.m.Text, task, ok, c.task, c.ok)
		}
	}
}

func TestLarkTranscriptKeepsTheNewestThatFit(t *testing.T) {
	base := time.Date(2026, 10, 31, 9, 0, 0, 0, time.Local)
	msgs := []lark.Message{
		{ID: "3", SenderName: "我", IsMine: true, Text: "/agent 看下", CreatedAt: base.Add(2 * time.Minute)},
		{ID: "1", SenderName: "张三", Text: strings.Repeat("长", 50), CreatedAt: base},
		{ID: "2", SenderName: "李四", Text: "报错了", CreatedAt: base.Add(time.Minute), Recalled: true},
		{ID: "x", Type: "SYSTEM", Text: "加入了群", CreatedAt: base},
	}
	got := larkTranscript(msgs, "3", 1000)
	want := "10-31 09:00 张三：" + strings.Repeat("长", 50) + "\n10-31 09:01 李四：[已撤回]\n10-31 09:02 我（我）：/agent 看下  ← 请求\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if short := larkTranscript(msgs, "3", 80); strings.Contains(short, "张三") || !strings.Contains(short, "← 请求") {
		t.Fatalf("a short limit keeps the newest lines: %q", short)
	}
}

func TestLarkAgentClaimsOnce(t *testing.T) {
	h := &Hub{}
	if !h.claimLarkAgent("m1") || h.claimLarkAgent("m1") {
		t.Fatal("a message is claimed once (lark-connector re-emits it when edited or its thread grows)")
	}
}
