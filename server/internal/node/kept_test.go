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
// (2026-11-01: the Persona tab had no 主对话 on a busy machine).
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

// A transcript whose tail holds no words is still listed when it is bigger than the tail read: the main conversation
// whose last turn stopped mid tool call under one of Claude Code's huge api-request-blob lines went missing from every
// list, and the app and the web had no session for 主对话 (#1163). A small probe that never said anything is still left
// out.
func TestTailWithoutWordsKeepsBigTranscriptsListed(t *testing.T) {
	n, _ := claudeHome(t)
	n.metas, n.bgScans = map[string]quickMeta{}, map[string]*bgScan{}
	dir := filepath.Join(n.home, ".claude", "projects", "-src-app")
	_ = os.MkdirAll(dir, 0o700)
	head := `{"type":"user","uuid":"u1","timestamp":"2026-11-04T01:00:00Z","cwd":"/src/app","message":{"role":"user","content":"整理一下今天的事"}}` + "\n" +
		`{"type":"assistant","uuid":"a1","timestamp":"2026-11-04T01:00:05Z","message":{"role":"assistant","content":[{"type":"text","text":"好的"}]}}` + "\n"
	blob := `{"type":"api-request-blob","timestamp":"2026-11-04T02:02:00Z","blob":"` + strings.Repeat("x", 400<<10) // bigger than the tail read + `"}` + "\n"
	toolTurn := `{"type":"assistant","uuid":"a2","timestamp":"2026-11-04T02:02:29Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"…"}]}}` + "\n" +
		`{"type":"assistant","uuid":"a3","timestamp":"2026-11-04T02:02:35Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}` + "\n" +
		`{"type":"user","uuid":"u2","timestamp":"2026-11-04T02:02:37Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}` + "\n"
	const main = "dddddddd-0000-0000-0000-000000000002"
	const other = "dddddddd-0000-0000-0000-000000000003"
	const probe = "dddddddd-0000-0000-0000-000000000004"
	_ = os.WriteFile(filepath.Join(dir, main+".jsonl"), []byte(head+blob+toolTurn), 0o600)
	_ = os.WriteFile(filepath.Join(dir, other+".jsonl"), []byte(head+blob+toolTurn), 0o600)
	_ = os.WriteFile(filepath.Join(dir, probe+".jsonl"),
		[]byte(`{"type":"system","timestamp":"2026-11-04T02:00:00Z","cwd":"/src/app"}`+"\n"+strings.Repeat(" ", 4096)+"\n"), 0o600)
	n.setKept([]string{main})
	listed := map[string]bool{}
	for _, s := range n.scanOffline() {
		listed[s.SessionID] = true
	}
	if !listed[main] {
		t.Fatal("the main conversation is listed whatever its tail holds")
	}
	if !listed[other] {
		t.Fatal("a transcript bigger than the tail read is listed though its tail has no words")
	}
	if listed[probe] {
		t.Fatal("a small transcript that never said anything is still left out")
	}
}
