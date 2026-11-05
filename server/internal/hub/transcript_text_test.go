package hub

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/transcript"
)

// userLine is a transcript line with a user message saying s, as Claude Code writes it.
func userLine(t *testing.T, s string) []proto.Item {
	t.Helper()
	content, _ := json.Marshal(s)
	var meta transcript.Meta
	items := transcript.ParseLine(1, []byte(`{"type":"user","uuid":"u1","message":{"role":"user","content":`+string(content)+`}}`), &meta)
	if len(items) != 1 {
		t.Fatalf("%q: %d items", s, len(items))
	}
	return items
}

// The node's lines, parsed by the node's own code, read in the app's language; zh-Hans gets them as they were.
func TestLocalizedTranscriptItems(t *testing.T) {
	note := func(xml string) string { return "<task-notification>" + xml + "</task-notification>" }
	cases := []struct {
		in     string
		en, ja string
	}{
		{note(`<task-id>a</task-id><status>failed</status><summary>Background command "npm test" failed with exit code 1</summary>`),
			"Background command failed (exit code 1) · npm test", "バックグラウンドコマンド失敗（終了コード1） · npm test"},
		{note(`<task-id>a</task-id><status>killed</status><summary>Background command "tail -f x" was stopped</summary>`),
			"Background command stopped · tail -f x", "バックグラウンドコマンド停止 · tail -f x"},
		{note(`<task-id>a</task-id><status>completed</status><summary>Agent "Review the diff" finished</summary><result>ok</result>`),
			"Subagent finished · Review the diff", "サブエージェント完了 · Review the diff"},
		{note(`<task-id>m</task-id><summary>Monitor event: "Watch CI"</summary><event>build 42 passed</event>`),
			"Monitor event · build 42 passed", "Monitorイベント · build 42 passed"},
		{note(`<task-id>z</task-id><summary>Something new happened</summary>`), "Something new happened", "Something new happened"},
		{"[Request interrupted by user]", "Interrupted", "中断しました"},
	}
	for _, c := range cases {
		items := userLine(t, c.in)
		zh := items[0].Text
		if got := localizedItems(i18n.ZhHans, items); got[0].Text != zh {
			t.Fatalf("zh-Hans changed %q into %q", zh, got[0].Text)
		}
		for l, want := range map[i18n.Lang]string{i18n.En: c.en, i18n.Ja: c.ja} {
			got := localizedItems(l, items)
			if got[0].Text != want {
				t.Errorf("%s: %q → %q, want %q", l, zh, got[0].Text, want)
			}
		}
		if items[0].Text != zh {
			t.Fatal("the node's items were written to")
		}
	}
}

func TestLocalizedNotices(t *testing.T) {
	for _, c := range []struct{ zh, en string }{
		{"/loop 到点，开始新一轮", "/loop is due, starting a new round"},
		{"/loop 到点，开始新一轮：check the build", "/loop is due, starting a new round: check the build"},
		{"Codex 事件：mcp_progress（完整记录在原会话中）", "Codex event: mcp_progress (the full record is in the original session)"},
		{"Codex 扩展事件：x.y（完整记录在原会话中）", "Codex extension event: x.y (the full record is in the original session)"},
		{"上下文已压缩", "上下文已压缩"},                                   // the apps find the compaction divider by it
		{"API 错误：Overloaded（正在重试）", "API 错误：Overloaded（正在重试）"}, // the apps' API error card parses it
		{"something else", "something else"},
	} {
		it := proto.Item{Role: "system", Kind: proto.KindNotice, Text: c.zh}
		if got := localizedItems(i18n.En, []proto.Item{it})[0].Text; got != c.en {
			t.Errorf("%q → %q, want %q", c.zh, got, c.en)
		}
	}
}

func TestLocalizedTeammateAndLinks(t *testing.T) {
	items := []proto.Item{
		{Role: "user", Kind: proto.KindText, From: "队友", Text: "**出错停下**：tests failed"},
		{Role: "user", Kind: proto.KindText, From: "docs", Text: "**分配任务 #3**：Write the docs\n\nDetails here"},
		{Role: "user", Kind: proto.KindText, From: "docs", Text: "空闲，等待新的任务"},
		{Role: "user", Kind: proto.KindText, From: "另一个会话", Text: "**计划已批准**\n\nGo ahead"},
		{Role: "assistant", Kind: proto.KindText, Text: "Done: [打开新建的对话](jarvis://session/m1/abc)"},
		{Role: "user", Kind: proto.KindToolResult, Text: "已加载工具 Read\n已加载工具 Grep"},
	}
	got := localizedItems(i18n.En, items)
	want := []proto.Item{
		{Role: "user", Kind: proto.KindText, From: "Teammate", Text: "**Stopped on an error**: tests failed"},
		{Role: "user", Kind: proto.KindText, From: "docs", Text: "**Task #3 assigned**: Write the docs\n\nDetails here"},
		{Role: "user", Kind: proto.KindText, From: "docs", Text: "Idle, waiting for a new task"},
		{Role: "user", Kind: proto.KindText, From: "Another session", Text: "**Plan approved**\n\nGo ahead"},
		{Role: "assistant", Kind: proto.KindText, Text: "Done: [Open the new chat](jarvis://session/m1/abc)"},
		{Role: "user", Kind: proto.KindToolResult, Text: "Loaded tool Read\nLoaded tool Grep"},
	}
	for i := range want {
		if got[i].From != want[i].From || got[i].Text != want[i].Text {
			t.Errorf("%d: %q %q, want %q %q", i, got[i].From, got[i].Text, want[i].From, want[i].Text)
		}
	}
	if items[0].From != "队友" {
		t.Fatal("the node's items were written to")
	}
}

// transcript.Activity's lines, the node's code again, in the user's language.
func TestLocalizedActivity(t *testing.T) {
	act := func(name, input string) string { return transcript.Activity(name, json.RawMessage(input)) }
	for _, c := range []struct{ zh, en, ja string }{
		{act("Read", `{"file_path":"/x/tasks.go"}`), "Reading tasks.go", "tasks.goを読み取り中"},
		{act("Bash", `{"command":"go test ./..."}`), "Running tests", "テスト実行中"},
		{act("Bash", `{"command":"gh pr checks 12 --watch"}`), "Waiting for CI", "CI待ち"},
		{act("Bash", `{"command":"echo hi","description":"Say hi"}`), "Running Say hi", "Say hiを実行中"},
		{act("WebSearch", `{"query":"go generics"}`), "Searching the web for go generics", "Web検索中：go generics"},
		{act("Grep", `{}`), "Searching", "検索中"},
		{act("mcp__jarvis__mail_send", `{}`), "Calling mail_send", "mail_sendを呼び出し中"},
		{act("AskUserQuestion", `{}`), "Asking you a question", "質問しています"},
		{"在做别的", "在做别的", "在做别的"},
	} {
		if got := activityText(i18n.En, c.zh); got != c.en {
			t.Errorf("en %q → %q, want %q", c.zh, got, c.en)
		}
		if got := activityText(i18n.Ja, c.zh); got != c.ja {
			t.Errorf("ja %q → %q, want %q", c.zh, got, c.ja)
		}
	}
	// A long one is cut to a task card's line again.
	long := act("Read", `{"file_path":"/x/a_very_long_file_name_that_goes_on_and_on_and_on.go"}`)
	if got := []rune(activityText(i18n.En, long)); len(got) > activityLen || got[len(got)-1] != '…' {
		t.Fatalf("%q", string(got))
	}
}

func TestLocalizeSession(t *testing.T) {
	bg := []proto.BackgroundTask{{ID: "b1", Kind: "shell", Description: "超时转到后台的命令"}}
	queued := []string{"【来自 另一个会话】look at this", "my own line"}
	s := proto.Session{Activity: "在跑测试", Background: bg, Queued: queued}
	zh := s
	localizeSession(i18n.ZhHans, &zh)
	if zh.Activity != "在跑测试" || zh.Background[0].Description != "超时转到后台的命令" || zh.Queued[0] != queued[0] {
		t.Fatalf("zh-Hans changed it: %+v", zh)
	}
	localizeSession(i18n.En, &s)
	if s.Activity != "Running tests" || s.Background[0].Description != "Command moved to the background after a timeout" ||
		!slices.Equal(s.Queued, []string{"[From Another session] look at this", "my own line"}) {
		t.Fatalf("%+v", s)
	}
	if bg[0].Description != "超时转到后台的命令" || queued[0] != "【来自 另一个会话】look at this" {
		t.Fatal("the node's slices were written to")
	}
}
