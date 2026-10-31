package hub

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/ai"
)

func TestTabSlug(t *testing.T) {
	cases := map[string]string{
		"duo-sidebar":                          "duo-sidebar",
		"  Fix Login\n(because the task…)":     "fix-login",
		"`inbox_agent`":                        "inbox-agent",
		"修复 Duo 侧边栏":                           "duo",
		"--release--notes--":                   "release-notes",
		"a-very-long-name-that-keeps-going-on": "a-very-long-name-that-ke",
		"名字":                                   "",
	}
	for in, want := range cases {
		if got := tabSlug(in); got != want {
			t.Errorf("tabSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestAutoTabNameLive names a few tasks with a real model:
// JARVIS_LIVE_AI='{"base_url":"…","api_key_file":"…","model":"…"}' go test -run AutoTabNameLive -v ./internal/hub
func TestAutoTabNameLive(t *testing.T) {
	raw := os.Getenv("JARVIS_LIVE_AI")
	if raw == "" {
		t.Skip("JARVIS_LIVE_AI not set")
	}
	var cfg ai.Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	h := newTestHub(t)
	h.logger = log.New(os.Stderr, "", 0)
	h.ai = ai.New(cfg)
	for _, p := range []string{"iPhone Duo 展开点击切换 agent，侧边会先收起再展开请修复", "把终端从 tab 栏去掉",
		"研究和实现怎么做飞书的 AI 分身：自动收集重要信息，判断是否需要处理，用 agent 处理并草拟回复", "fix the flaky login test on CI"} {
		start := time.Now()
		name := h.autoTabName(context.Background(), p)
		t.Logf("%-6s %q ← %s", time.Since(start).Round(10*time.Millisecond), name, p)
		if name == "" {
			t.Errorf("no name for %q", p)
		}
	}
}

func TestDefaultTabName(t *testing.T) {
	for _, n := range []string{"", "jarvis", "jarvis-3", "1", "12", "agent-4"} {
		if !defaultTabName.MatchString(n) {
			t.Errorf("%q should count as nobody's choice", n)
		}
	}
	for _, n := range []string{"leader", "leader-kiro", "ai-person-kiro", "jarvis-app", "agent"} {
		if defaultTabName.MatchString(n) {
			t.Errorf("%q was chosen by someone", n)
		}
	}
	if got := uniqueName("fix-login", map[string]bool{"fix-login": true, "fix-login-2": true}); got != "fix-login-3" {
		t.Errorf("uniqueName = %q", got)
	}
}
