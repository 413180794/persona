package textfmt

import (
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestFormatting(t *testing.T) {
	if got := Clip("一二三四五", 3); got != "一二三…" {
		t.Errorf("Clip = %q", got)
	}
	if got := Clip("一二三", 3); got != "一二三" {
		t.Errorf("Clip kept = %q", got)
	}
	if got := OneLine("  a\n\tb   c \n"); got != "a b c" {
		t.Errorf("OneLine = %q", got)
	}
	if got := FirstLine("\n\n## 计划\n第一步"); got != "计划" {
		t.Errorf("FirstLine = %q", got)
	}
	if got := Short("abcdef1234"); got != "abcdef12" {
		t.Errorf("Short = %q", got)
	}
	if Ago(time.Time{}) != "" || Ago(time.Now()) != "刚刚" || Ago(time.Now().Add(-5*time.Minute)) != "5 分钟前" ||
		Ago(time.Now().Add(-3*time.Hour)) != "3 小时前" || Ago(time.Now().Add(-49*time.Hour)) != "2 天前" {
		t.Error("Ago")
	}
	if StateNames[proto.StateNeedsYou] != "需要你" {
		t.Error("StateNames")
	}
}

func TestAgentLabel(t *testing.T) {
	for _, tc := range []struct {
		s    proto.Session
		want string
	}{
		{proto.Session{Title: "修登录", Cwd: "/code/app"}, "修登录"},
		{proto.Session{Title: "Claude Code", Cwd: "/code/app"}, "app"},
		{proto.Session{Cwd: "/code/app", SpaceName: "客户端"}, "客户端 › app"},
		{proto.Session{Title: "修登录", SpaceName: "客户端", SlotName: "修登录"}, "客户端 › 修登录"},
		{proto.Session{Title: "修登录", SpaceName: "客户端", SlotName: "左"}, "客户端 › 左「修登录」"},
	} {
		if got := AgentLabel(tc.s); got != tc.want {
			t.Errorf("AgentLabel(%+v) = %q, want %q", tc.s, got, tc.want)
		}
	}
}
