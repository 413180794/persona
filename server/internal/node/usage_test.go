package node

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func claudeReply(ts time.Time, id, model string, in, write1h, read, out int) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"id":%q,"model":%q,"content":[],"usage":{"input_tokens":%d,`+
		`"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":%d,"cache_creation":{"ephemeral_1h_input_tokens":%d}}}}`+"\n",
		ts.Format(time.RFC3339Nano), id, model, in, write1h, read, out, write1h)
}

func TestUsageMeterClaude(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 30, 0, time.UTC)
	cutoff := now.Add(-usageWindow)
	p := filepath.Join(t.TempDir(), "s.jsonl")
	body := claudeReply(now.Add(-30*time.Hour), "old", "claude-opus-5-5", 1, 1, 1, 1) + // before the window
		claudeReply(now, "m1", "claude-opus-5-5", 2, 100, 1000, 50) +
		claudeReply(now, "m1", "claude-opus-5-5", 2, 100, 1000, 50) + // the same reply's next content block
		`{"type":"user","timestamp":"` + now.Format(time.RFC3339) + `","message":{"content":"usage"}}` + "\n" +
		claudeReply(now, "m2", "<synthetic>", 0, 0, 0, 0) +
		claudeReply(now.Add(time.Minute), "m3", "claude-sonnet-5-5", 5, 0, 10, 7) +
		`{"type":"assistant","timestamp":"` // unfinished line: left for the next scan
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	m := newUsageMeter()
	m.scan(p, cutoff)
	if len(m.buckets) != 2 {
		t.Fatalf("buckets = %d, want 2: %+v", len(m.buckets), m.buckets)
	}
	b := m.buckets[usageKey{now.Unix() / 60 * 60, "claude-opus-5-5"}]
	if b == nil || b.Input != 2 || b.CacheWrite != 100 || b.CacheRead != 1000 || b.Output != 50 || b.Requests != 1 {
		t.Fatalf("opus bucket = %+v", b)
	}
	if want := int64(len(body) - len(`{"type":"assistant","timestamp":"`)); m.files[p].off != want {
		t.Fatalf("offset = %d, want %d (before the unfinished line)", m.files[p].off, want)
	}
	// The line is finished later: only it is read.
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(now.Add(time.Minute).Format(time.RFC3339Nano) + `","message":{"id":"m4","model":"claude-sonnet-5-5","usage":{"output_tokens":3}}}` + "\n")
	f.Close()
	m.dirty = map[usageKey]bool{}
	m.scan(p, cutoff)
	s := m.buckets[usageKey{now.Unix()/60*60 + 60, "claude-sonnet-5-5"}]
	if s == nil || s.Output != 10 || s.Requests != 2 || len(m.dirty) != 1 {
		t.Fatalf("sonnet bucket = %+v, dirty %v", s, m.dirty)
	}
	// Reports: everything first, then only what changed, then nothing.
	params, ok := m.report(now)
	if !ok || !params["full"].(bool) {
		t.Fatalf("first report = %v", params)
	}
	if _, ok := m.report(now.Add(time.Second)); ok {
		t.Fatal("a report without changes")
	}
}

func TestUsageMeterCodex(t *testing.T) {
	now := time.Now().UTC()
	line := func(kind, payload string) string {
		return fmt.Sprintf(`{"timestamp":%q,"type":%q,"payload":%s}`+"\n", now.Format(time.RFC3339Nano), kind, payload)
	}
	count := func(total, in, cached, out int) string {
		return line("event_msg", fmt.Sprintf(`{"type":"token_count","info":{"total_token_usage":{"total_tokens":%d},`+
			`"last_token_usage":{"input_tokens":%d,"cached_input_tokens":%d,"output_tokens":%d}}}`, total, in, cached, out))
	}
	body := line("turn_context", `{"model":"gpt-6-codex"}`) + count(1000, 900, 800, 100) +
		count(1000, 900, 800, 100) + // repeated with a rate limit update
		line("event_msg", `{"type":"token_count","info":null}`) + count(1500, 450, 400, 50)
	p := filepath.Join(t.TempDir(), "rollout-2026-10-29T10-00-00-0199a213-81c0-7800-8aa1-bbab2a035a53.jsonl")
	os.WriteFile(p, []byte(body), 0o600)
	m := newUsageMeter()
	m.scan(p, now.Add(-time.Hour))
	b := m.buckets[usageKey{now.Unix() / 60 * 60, "gpt-6-codex"}]
	if b == nil || b.Requests != 2 || b.Input != 150 || b.CacheRead != 1200 || b.Output != 150 {
		t.Fatalf("codex bucket = %+v", b)
	}
}

func TestUsageStartOffset(t *testing.T) {
	now := time.Now().UTC()
	cutoff := now.Add(-time.Hour)
	var sb strings.Builder
	pad := strings.Repeat("x", 4000)
	for i := 0; sb.Len() < 12<<20; i++ {
		fmt.Fprintf(&sb, `{"type":"user","timestamp":%q,"pad":%q}`+"\n", now.Add(-5*time.Hour).Format(time.RFC3339Nano), pad)
	}
	old := sb.Len()
	sb.WriteString(claudeReply(now, "m1", "claude-opus-5-5", 1, 0, 0, 9))
	p := filepath.Join(t.TempDir(), "big.jsonl")
	os.WriteFile(p, []byte(sb.String()), 0o600)
	off := startOffset(p, int64(sb.Len()), cutoff)
	if off == 0 || off > int64(old) {
		t.Fatalf("start offset = %d, want inside the old part (%d bytes)", off, old)
	}
	m := newUsageMeter()
	m.scan(p, cutoff)
	if b := m.buckets[usageKey{now.Unix() / 60 * 60, "claude-opus-5-5"}]; b == nil || b.Output != 9 {
		t.Fatalf("bucket = %+v", b)
	}
}

func TestEachLineSkipsHugeLines(t *testing.T) {
	huge := strings.Repeat("y", usageMaxLine+10)
	var got []string
	n := eachLine(strings.NewReader("a\n"+huge+"\nb\npartial"), func(l []byte) { got = append(got, string(l)) })
	if strings.Join(got, ",") != "a,b" || n != int64(len("a\n"+huge+"\nb\n")) {
		t.Fatalf("lines = %v, consumed %d", got, n)
	}
}
