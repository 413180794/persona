package assistant

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/transcript"
)

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

// The node's reasons for running an Agent SDK conversation on the CLI read in the app's language, the cause as it was;
// a reason the hub does not know, and Chinese, stay as they are.
func TestRuntimeFallbackInEachLanguage(t *testing.T) {
	for _, c := range []struct{ node, en string }{
		{fmt.Sprintf(proto.FallbackShellLauncher, "kcc"), "Launcher kcc is a shell command on this computer; the Agent SDK can’t use its settings"},
		{proto.FallbackNoNode + "exec: \"node\": not found", "No Node.js 22 or later: exec: \"node\": not found"},
		{proto.FallbackNoClaude + "nope", "Can’t find claude: nope"},
		{proto.FallbackNoBridge, "This personad doesn’t include the Agent SDK bridge"},
		{proto.FallbackNoBridgeScript, "The Agent SDK bridge package has no bridge.mjs"},
		{proto.FallbackReadBridge + "x", "Couldn’t read the Agent SDK bridge: x"},
		{proto.FallbackWriteBridge + "y", "Couldn’t write the Agent SDK bridge: y"},
		{proto.FallbackNoBridge + "，还有别的", proto.FallbackNoBridge + "，还有别的"},
		{"别的原因", "别的原因"},
		{"", ""},
	} {
		s := proto.Session{RuntimeFallback: c.node}
		localizeSession(i18n.En, &s)
		if s.RuntimeFallback != c.en {
			t.Errorf("%q: %q", c.node, s.RuntimeFallback)
		}
		s = proto.Session{RuntimeFallback: c.node}
		localizeSession(i18n.ZhHans, &s)
		if s.RuntimeFallback != c.node {
			t.Errorf("zh %q: %q", c.node, s.RuntimeFallback)
		}
	}
}
