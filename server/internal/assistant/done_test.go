package assistant

import (
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/prompts"
)

func TestAutoDoneSkip(t *testing.T) {
	now := time.Now()
	reopened := now.Add(-time.Minute)
	for _, tc := range []struct {
		t      Todo
		work   time.Time
		worked bool
		want   string
	}{
		{Todo{}, now, true, "not_a_task"},
		{Todo{Origin: "c", DoneAt: &now}, now, true, "done"},
		{Todo{Origin: "c", ReopenedAt: &reopened}, now.Add(-2 * time.Minute), true, "reopened"},
		{Todo{Origin: "c", ReopenedAt: &reopened}, time.Time{}, false, "reopened"},
		{Todo{Origin: "c", ReopenedAt: &reopened}, now, true, ""},
		{Todo{Origin: "c"}, time.Time{}, false, ""},
	} {
		if got := autoDoneSkip(tc.t, tc.work, tc.worked); got != tc.want {
			t.Errorf("autoDoneSkip(%+v, worked %v) = %q, want %q", tc.t, tc.worked, got, tc.want)
		}
	}
}

func TestDoneSignals(t *testing.T) {
	for _, tc := range []struct {
		text      string
		pr, build bool
	}{
		{"PR https://github.com/o/r/pull/12 已合并", true, false},
		{"提了 https://github.com/o/r/pull/12，等 review", false, false},
		{"merged https://github.com/o/r/pull/12; TestFlight build 202611041200 is up", true, true},
		{"发了 TestFlight，构建号 1234", false, true},
		{"没有全改完", false, false},
	} {
		got := doneSignals([]prompts.TaskDoneReply{{Text: tc.text}})
		if got.MergedPR != tc.pr || got.Build != tc.build {
			t.Errorf("doneSignals(%q) = %+v", tc.text, got)
		}
	}
	if s := clipMiddle(strings.Repeat("a", 10)+strings.Repeat("b", 10), 4); s != "aaaa\n…\nbbbb" {
		t.Fatalf("clipMiddle = %q", s)
	}
}

func TestSameRequest(t *testing.T) {
	if !sameRequest("<jarvis-context kind=\"context\">x</jarvis-context>\niPad 横屏时登录页崩溃，修好并补测试", "iPad 横屏时登录页崩溃，修好并补测试") ||
		sameRequest("再把「全部 N」挪到最左边", "iPad 横屏时登录页崩溃，修好并补测试") {
		t.Fatal("sameRequest")
	}
}
