package transcript

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Between two turns a long session can hold many MB of lines that show nothing (Codex keeps the whole context in
// its history lines after a compaction). A page back parsed its budget of them, found no item and answered an empty
// page with has_more: the app had no seq to ask further back from, and the older history was out of reach
// (2026-10-31, 3 of 76 large Codex rollouts on one machine). A page now always reaches the next item.
func TestAPageBackReachesPastLinesThatShowNothing(t *testing.T) {
	defer func(w int64) { windowBytes = w }(windowBytes)
	windowBytes = 4 << 10
	var b strings.Builder
	line := func(s string) { b.WriteString(s + "\n") }
	line(`{"type":"user","uuid":"u1","timestamp":"2026-10-30T10:00:00Z","message":{"role":"user","content":"第一个问题"}}`)
	line(`{"type":"assistant","uuid":"a1","timestamp":"2026-10-30T10:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"回答一"}]}}`)
	for i := range 40 {
		line(fmt.Sprintf(`{"type":"file-history-snapshot","messageId":"m%d","snapshot":{"pad":"%s"}}`, i, strings.Repeat("x", 2000)))
	}
	line(`{"type":"user","uuid":"u2","timestamp":"2026-10-30T11:00:00Z","message":{"role":"user","content":"第二个问题"}}`)
	line(`{"type":"assistant","uuid":"a2","timestamp":"2026-10-30T11:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"回答二"}]}}`)
	p := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	f := NewFile(p)
	if _, err := f.Refresh(); err != nil {
		t.Fatal(err)
	}
	last, more, _ := f.PageTurns(0, 1, 2000)
	if len(last) != 2 || !more || last[0].Text != "第二个问题" {
		t.Fatalf("first page %+v more %v", last, more)
	}
	older, more, _ := f.PageTurns(last[0].Seq, 1, 2000)
	if len(older) != 2 || older[0].Text != "第一个问题" || more {
		t.Fatalf("the page before: %d items, more %v", len(older), more)
	}
	// Pages by count (older clients) too.
	g := NewFile(p)
	if _, err := g.Refresh(); err != nil {
		t.Fatal(err)
	}
	if older, _, _ := g.Page(last[0].Seq, 50); len(older) != 2 {
		t.Fatalf("a page by count before the gap: %d items", len(older))
	}
}
