package assistant

// Golden tests for the prompts the assistant writes (docs/prompts.md), compared byte for byte with
// testdata/prompts/<name>.golden. After changing a prompt on purpose, regenerate with
//
//	UPDATE_GOLDEN=1 go test ./internal/assistant -run Golden
//
// and read the diff before committing it. All inputs are placeholders.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "prompts", name+".golden")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (UPDATE_GOLDEN=1 writes it)", name, err)
	}
	if got != string(want) {
		t.Errorf("%s differs from %s\n--- got ---\n%s\n--- want ---\n%s", name, path, got, want)
	}
}

func TestGoldenSessionTitle(t *testing.T) {
	f := newFakeHub()
	f.settings = &Settings{Timezone: "Asia/Shanghai"}
	f.replies = []string{"改个名字", "修复 | 搜索框和顶栏重叠"}
	s := newTestService(t, f)
	started := time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC)
	if got := s.makeTitle(context.Background(), started, "搜索的时候搜索框和顶栏叠在一起了", "是两个组件一起用导致的。"); got != "1101 | 修复 | 搜索框和顶栏重叠" {
		t.Fatalf("%q", got)
	}
	c := f.modelCalls()
	if len(c) != 2 {
		t.Fatalf("%d calls", len(c))
	}
	golden(t, "session_title.system", c[0].System)
	golden(t, "session_title.user", c[0].User)
	golden(t, "session_title.retry", c[1].User)
}

func TestGoldenRelay(t *testing.T) {
	golden(t, "relay", relayText(relayMsg{From: "b2e4d6f8-0000-4000-8000-000000000002", FromTitle: `1101 | 研究 | "示例"<主题>`,
		Tool: "conversation_send", Text: "示例消息：把结果整理一下", Hop: 2}))
}

func TestGoldenTaskPrompt(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai") // passed in: the output doesn't depend on the machine's zone
	due := time.Date(2026, 10, 31, 1, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name      string
		task      Task
		late      time.Duration
		manual    bool
		kb        bool
		continued bool
	}{
		{"once_late", Task{Title: "看 CI", Prompt: "看看 CI 跑完没有", Repeat: repeatOnce}, 25 * time.Minute, false, true, false},
		{"once_untitled", Task{Prompt: "提醒我喝水", Repeat: repeatOnce}, time.Minute, false, true, false},
		{"interval_late", Task{Prompt: "巡检各台电脑上的 Agent，有卡住的告诉我", Repeat: repeatInterval, EveryMinutes: 120}, 3 * time.Hour, false, false, false},
		{"interval_manual", Task{Title: "巡检", Prompt: "巡检", Repeat: repeatInterval, EveryMinutes: 30}, 3 * time.Hour, true, true, false},
		{"daily_kb", Task{Title: "早报", Prompt: "给我今天的早报", Repeat: repeatDaily}, 2 * time.Hour, false, true, false},
		{"daily_nokb", Task{Title: "早报", Prompt: "给我今天的早报", Repeat: repeatDaily}, 0, false, false, false},
		// Later runs in the task's own conversation: no knowledge-base hint or "don't reschedule" again.
		{"daily_continued", Task{Title: "早报", Prompt: "给我今天的早报", Repeat: repeatDaily}, 0, false, true, true},
		{"interval_continued_late", Task{Title: "巡检", Prompt: "巡检", Repeat: repeatInterval, EveryMinutes: 30}, 3 * time.Hour, false, true, true},
	} {
		golden(t, "task_prompt."+c.name, taskPrompt(c.task, due, due.Add(c.late), loc, c.manual, c.kb, c.continued))
	}
}

func TestGoldenDefaultRoutines(t *testing.T) {
	for _, r := range defaultRoutines() {
		golden(t, "routine."+r.ID, r.Prompt)
	}
}
