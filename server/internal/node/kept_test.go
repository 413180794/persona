package node

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The assistant's main conversation stays listed when 80 newer transcripts and a week of quiet would push it out
// (2026-11-01: the Jarvis tab had no 主对话 on a busy machine).
func TestKeptSessionOutlastsTheOfflineCut(t *testing.T) {
	n, _ := claudeHome(t)
	n.metas, n.bgScans = map[string]quickMeta{}, map[string]*bgScan{}
	dir := filepath.Join(n.home, ".claude", "projects", "-src-app")
	_ = os.MkdirAll(dir, 0o700)
	write := func(id string, age time.Duration) {
		p := filepath.Join(dir, id+".jsonl")
		body := `{"type":"user","uuid":"u1","timestamp":"2026-10-20T10:00:00Z","cwd":"/src/app","message":{"role":"user","content":"hi"}}` + "\n" +
			`{"type":"assistant","uuid":"a1","timestamp":"2026-10-20T10:00:05Z","message":{"role":"assistant","content":[{"type":"text","text":"hello"}]}}` + "\n"
		_ = os.WriteFile(p, []byte(body+strings.Repeat(" ", 4096)+"\n"), 0o600)
		at := time.Now().Add(-age)
		_ = os.Chtimes(p, at, at)
	}
	const main = "dddddddd-0000-0000-0000-000000000001"
	write(main, 10*24*time.Hour) // older than the offline window
	for i := 0; i < 85; i++ {
		write(fmt.Sprintf("eeeeeeee-0000-0000-0000-%012d", i), time.Duration(i+1)*time.Minute)
	}
	listed := func() bool {
		for _, s := range n.scanOffline() {
			if s.SessionID == main {
				return true
			}
		}
		return false
	}
	if listed() {
		t.Fatal("without keep the old one is not listed (the test's premise)")
	}
	n.setKept([]string{main})
	if !listed() {
		t.Fatal("a kept session is listed however old and however many newer ones there are")
	}
	if got := len(n.scanOffline()); got != 81 {
		t.Fatalf("the newest 80 plus the kept one, got %d", got)
	}
}
