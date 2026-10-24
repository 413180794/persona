package transcript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chyroc/jarvis/server/internal/proto"
)

// parse runs one line through ParseLine and returns the items.
func parse(t *testing.T, line string) ([]proto.Item, *Meta) {
	t.Helper()
	meta := &Meta{}
	return ParseLine(1, []byte(line), meta), meta
}

func only(t *testing.T, items []proto.Item) proto.Item {
	t.Helper()
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d: %+v", len(items), items)
	}
	return items[0]
}

func TestUserStringContent(t *testing.T) {
	items, meta := parse(t, `{"type":"user","uuid":"u1","timestamp":"2026-10-24T10:00:00Z","cwd":"/tmp/p","message":{"role":"user","content":"hello there"}}`)
	it := only(t, items)
	if it.Role != "user" || it.Kind != proto.KindText || it.Text != "hello there" {
		t.Fatalf("unexpected item %+v", it)
	}
	if it.ID != "u1#0" || it.Seq != 1 {
		t.Fatalf("unexpected identity %+v", it)
	}
	if it.TS.IsZero() {
		t.Fatal("timestamp was not copied")
	}
	if meta.Cwd != "/tmp/p" {
		t.Fatalf("meta cwd = %q", meta.Cwd)
	}
}

func TestUserBlocksToolUseAndResultPairing(t *testing.T) {
	args, _ := parse(t, `{"type":"assistant","uuid":"a1","message":{"role":"assistant","model":"claude-test","content":[
		{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls -la"}}]}}`)
	use := only(t, args)
	if use.Kind != proto.KindToolUse || use.ToolUseID != "toolu_1" || use.ToolName != "Bash" {
		t.Fatalf("unexpected tool_use %+v", use)
	}
	var input map[string]string
	if err := json.Unmarshal(use.ToolInput, &input); err != nil || input["command"] != "ls -la" {
		t.Fatalf("tool input = %s (%v)", use.ToolInput, err)
	}

	result, _ := parse(t, `{"type":"user","uuid":"u2","message":{"role":"user","content":[
		{"type":"text","text":"here you go"},
		{"type":"tool_result","tool_use_id":"toolu_1","content":[
			{"type":"text","text":"total 0"},
			{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGVsbG8="}}]}]}}`)
	if len(result) != 3 {
		t.Fatalf("want text + tool_result + image, got %+v", result)
	}
	if result[0].Kind != proto.KindText || result[0].Text != "here you go" {
		t.Fatalf("unexpected first block %+v", result[0])
	}
	tr := result[1]
	if tr.Kind != proto.KindToolResult || tr.ToolUseID != "toolu_1" || tr.Text != "total 0" {
		t.Fatalf("unexpected tool_result %+v", tr)
	}
	img := result[2]
	if img.Kind != proto.KindImage || img.ToolUseID != "toolu_1" || img.Image == nil || img.Image.Mime != "image/png" {
		t.Fatalf("unexpected image %+v", img)
	}
}

func TestToolResultErrorFlagAndPlainString(t *testing.T) {
	items, _ := parse(t, `{"type":"user","uuid":"u3","message":{"role":"user","content":[
		{"type":"tool_result","tool_use_id":"t2","is_error":true,"content":"permission denied"}]}}`)
	it := only(t, items)
	if !it.IsError || it.Text != "permission denied" {
		t.Fatalf("unexpected result %+v", it)
	}
}

func TestAssistantThinkingTextAndModel(t *testing.T) {
	items, meta := parse(t, `{"type":"assistant","uuid":"a2","message":{"role":"assistant","model":"claude-sonnet","content":[
		{"type":"thinking","thinking":"let me think"},
		{"type":"text","text":"  "},
		{"type":"text","text":"all done"}]}}`)
	if len(items) != 2 {
		t.Fatalf("blank text blocks must be dropped, got %+v", items)
	}
	if items[0].Kind != proto.KindThinking || items[0].Text != "let me think" {
		t.Fatalf("unexpected thinking %+v", items[0])
	}
	if items[1].Kind != proto.KindText || items[1].Text != "all done" {
		t.Fatalf("unexpected text %+v", items[1])
	}
	if meta.Model != "claude-sonnet" || meta.LastText != "all done" {
		t.Fatalf("meta model=%q last=%q", meta.Model, meta.LastText)
	}

	_, synthetic := parse(t, `{"type":"assistant","message":{"role":"assistant","model":"<synthetic>","content":[{"type":"text","text":"x"}]}}`)
	if synthetic.Model != "" {
		t.Fatalf("synthetic model must be ignored, got %q", synthetic.Model)
	}
}

func TestCommandMessage(t *testing.T) {
	items, _ := parse(t, `{"type":"user","uuid":"u4","message":{"role":"user","content":"<command-name>/compact</command-name>\n            <command-message>compact</command-message>\n            <command-args></command-args>"}}`)
	it := only(t, items)
	if it.Kind != proto.KindText || it.Text != "/compact" {
		t.Fatalf("unexpected item %+v", it)
	}

	items, _ = parse(t, `{"type":"user","uuid":"u5","message":{"role":"user","content":"<command-name>/model</command-name><command-args>opus</command-args>"}}`)
	if it := only(t, items); it.Text != "/model opus" {
		t.Fatalf("command args were dropped: %q", it.Text)
	}
}

func TestLocalCommandOutputBecomesNotice(t *testing.T) {
	items, _ := parse(t, `{"type":"user","uuid":"u6","message":{"role":"user","content":"<local-command-stdout>Total cost: $0.04</local-command-stdout>"}}`)
	it := only(t, items)
	if it.Role != "system" || it.Kind != proto.KindNotice || it.Text != "Total cost: $0.04" {
		t.Fatalf("unexpected notice %+v", it)
	}
}

func TestSystemReminderIsStrippedFromUserText(t *testing.T) {
	items, _ := parse(t, `{"type":"user","uuid":"u7","message":{"role":"user","content":"real message\n<system-reminder>secret context</system-reminder>"}}`)
	it := only(t, items)
	if it.Text != "real message" {
		t.Fatalf("system-reminder leaked: %q", it.Text)
	}

	if items, _ := parse(t, `{"type":"user","uuid":"u8","message":{"role":"user","content":"<system-reminder>only a reminder</system-reminder>"}}`); len(items) != 0 {
		t.Fatalf("reminder-only message should produce nothing, got %+v", items)
	}
}

func TestMetaAndSidechainAreSkipped(t *testing.T) {
	if items, _ := parse(t, `{"type":"user","uuid":"u9","isMeta":true,"message":{"role":"user","content":"caveat: ..."}}`); len(items) != 0 {
		t.Fatalf("isMeta line should be skipped, got %+v", items)
	}
	if items, _ := parse(t, `{"type":"assistant","uuid":"a9","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"side"}]}}`); len(items) != 0 {
		t.Fatalf("sidechain line should be skipped, got %+v", items)
	}
}

func TestCompactBoundaryAndSummary(t *testing.T) {
	items, _ := parse(t, `{"type":"system","subtype":"compact_boundary","uuid":"s1"}`)
	if it := only(t, items); it.Kind != proto.KindNotice || it.Role != "system" || it.Text != "上下文已压缩" {
		t.Fatalf("unexpected boundary notice %+v", it)
	}

	items, _ = parse(t, `{"type":"user","uuid":"u10","isCompactSummary":true,"message":{"role":"user","content":"long summary"}}`)
	if it := only(t, items); it.Kind != proto.KindNotice || it.Text != "已从压缩的上下文继续" {
		t.Fatalf("unexpected summary notice %+v", it)
	}
}

func TestAITitleIsMetaOnly(t *testing.T) {
	items, meta := parse(t, `{"type":"ai-title","aiTitle":"修复构建"}`)
	if len(items) != 0 {
		t.Fatalf("ai-title should not produce items, got %+v", items)
	}
	if meta.Title != "修复构建" {
		t.Fatalf("title = %q", meta.Title)
	}
}

func TestInterruptedNotice(t *testing.T) {
	items, _ := parse(t, `{"type":"user","uuid":"u11","message":{"role":"user","content":"[Request interrupted by user]"}}`)
	if it := only(t, items); it.Kind != proto.KindNotice || it.Text != "已中断" {
		t.Fatalf("unexpected interrupt notice %+v", it)
	}
}

func TestTaskNotificationBecomesNotice(t *testing.T) {
	items, _ := parse(t, `{"type":"user","uuid":"u12","message":{"role":"user","content":"<task-notification><summary>后台完成</summary></task-notification>"}}`)
	if it := only(t, items); it.Kind != proto.KindNotice || it.Text != "后台完成" {
		t.Fatalf("unexpected notice %+v", it)
	}
}

func TestGarbageLineIsIgnored(t *testing.T) {
	items, meta := parse(t, `not json at all`)
	if len(items) != 0 {
		t.Fatalf("garbage should produce no items, got %+v", items)
	}
	if !meta.Updated.IsZero() {
		t.Fatalf("garbage must not update meta, got %v", meta.Updated)
	}
}

// writeFile writes lines joined by newlines plus the given tail (usually "" or a partial line).
func writeFile(t *testing.T, dir string, lines []string, tail string) string {
	t.Helper()
	path := filepath.Join(dir, "session.jsonl")
	body := strings.Join(lines, "\n")
	if len(lines) > 0 {
		body += "\n"
	}
	body += tail
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRefreshIncrementalWithPartialLine(t *testing.T) {
	dir := t.TempDir()
	line1 := `{"type":"user","uuid":"u1","message":{"role":"user","content":"one"}}`
	line2 := `{"type":"user","uuid":"u2","message":{"role":"user","content":"two"}}`
	line3 := `{"type":"user","uuid":"u3","message":{"role":"user","content":"three"}}`
	line4 := `{"type":"user","uuid":"u4","message":{"role":"user","content":"four"}}`
	path := writeFile(t, dir, []string{line1, line2, line3}, line4[:20])

	f := NewFile(path)
	fresh, err := f.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh) != 3 {
		t.Fatalf("want 3 complete lines, got %d: %+v", len(fresh), fresh)
	}
	if f.LastSeq() != 3 {
		t.Fatalf("LastSeq = %d", f.LastSeq())
	}

	// A second refresh without changes returns nothing.
	if again, err := f.Refresh(); err != nil || len(again) != 0 {
		t.Fatalf("idle refresh returned %+v, %v", again, err)
	}

	// The rest of the partial line arrives.
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fh.WriteString(line4[20:] + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := fh.Close(); err != nil {
		t.Fatal(err)
	}
	fresh, err = f.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh) != 1 || fresh[0].Text != "four" || fresh[0].Seq != 4 {
		t.Fatalf("want the fourth line, got %+v", fresh)
	}
	if f.LastSeq() != 4 {
		t.Fatalf("LastSeq = %d", f.LastSeq())
	}
}

func TestRefreshRestartsWhenFileShrinks(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, []string{
		`{"type":"user","uuid":"u1","message":{"role":"user","content":"one"}}`,
		`{"type":"user","uuid":"u2","message":{"role":"user","content":"two"}}`,
	}, "")
	f := NewFile(path)
	if _, err := f.Refresh(); err != nil {
		t.Fatal(err)
	}

	writeFile(t, dir, []string{`{"type":"user","uuid":"u9","message":{"role":"user","content":"fresh"}}`}, "")
	fresh, err := f.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh) != 1 || fresh[0].Text != "fresh" || fresh[0].Seq != 1 {
		t.Fatalf("want a fresh parse, got %+v", fresh)
	}
}

func TestRefreshMissingFile(t *testing.T) {
	f := NewFile(filepath.Join(t.TempDir(), "nope.jsonl"))
	if _, err := f.Refresh(); err == nil {
		t.Fatal("want an error for a missing file")
	}
}

// pageFixture builds six lines; the last one yields two items (text + tool_use).
func pageFixture(t *testing.T) *File {
	t.Helper()
	lines := []string{
		`{"type":"user","uuid":"u1","message":{"role":"user","content":"one"}}`,
		`{"type":"user","uuid":"u2","message":{"role":"user","content":"two"}}`,
		`{"type":"user","uuid":"u3","message":{"role":"user","content":"three"}}`,
		`{"type":"user","uuid":"u4","message":{"role":"user","content":"four"}}`,
		`{"type":"user","uuid":"u5","message":{"role":"user","content":"five"}}`,
		`{"type":"assistant","uuid":"a6","message":{"role":"assistant","content":[{"type":"text","text":"six"},{"type":"tool_use","id":"t6","name":"Read","input":{"file_path":"/x"}}]}}`,
	}
	f := NewFile(writeFile(t, t.TempDir(), lines, ""))
	if _, err := f.Refresh(); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestPageFromTheEnd(t *testing.T) {
	f := pageFixture(t)
	items, more := f.Page(0, 2)
	if !more {
		t.Fatal("has_more should be true when older items exist")
	}
	if len(items) != 2 || items[0].Seq != 6 || items[1].Seq != 6 {
		t.Fatalf("a page must not split a line: %+v", items)
	}
	if items[0].Kind != proto.KindText || items[1].Kind != proto.KindToolUse {
		t.Fatalf("unexpected kinds %+v", items)
	}
}

func TestPageBeforeSeq(t *testing.T) {
	f := pageFixture(t)
	items, more := f.Page(4, 2)
	if len(items) != 2 || items[0].Seq != 2 || items[1].Seq != 3 {
		t.Fatalf("before_seq is exclusive: %+v", items)
	}
	if !more {
		t.Fatal("seq 1 is still older")
	}

	items, more = f.Page(2, 2)
	if len(items) != 1 || items[0].Seq != 1 || items[0].Text != "one" {
		t.Fatalf("unexpected page %+v", items)
	}
	if more {
		t.Fatal("nothing older than seq 1")
	}
}

func TestAfterSeq(t *testing.T) {
	f := pageFixture(t)
	items := f.After(5, 10)
	if len(items) != 2 {
		t.Fatalf("want both items of line 6, got %+v", items)
	}
	if items[0].Seq != 6 || items[1].Seq != 6 {
		t.Fatalf("unexpected seqs %+v", items)
	}
	if got := f.After(0, 3); len(got) != 3 || got[2].Seq != 3 {
		t.Fatalf("limit not honoured: %+v", got)
	}
	if got := f.After(6, 10); len(got) != 0 {
		t.Fatalf("nothing after the last line, got %+v", got)
	}
}

func TestClipKeepsUTF8Intact(t *testing.T) {
	long := strings.Repeat("汉", maxTextLen)
	got := clip(long)
	if len(got) <= maxTextLen {
		t.Fatalf("clip should have truncated, len=%d", len(got))
	}
	if strings.ContainsRune(got, '�') {
		t.Fatal("clip cut a rune in half")
	}
	if clip("short") != "short" {
		t.Fatal("short text must pass through")
	}
}

func TestImageTooLargeIsDropped(t *testing.T) {
	data := strings.Repeat("A", maxImageLen+4)
	line := `{"type":"user","uuid":"u1","message":{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + data + `"}}]}}`
	items, _ := parse(t, line)
	if len(items) != 0 {
		t.Fatalf("oversized image should be dropped, got %d items", len(items))
	}
}

func TestClipJSONKeepsRunesIntact(t *testing.T) {
	// A leading ASCII byte shifts the cut point into the middle of a rune.
	body := "a" + strings.Repeat("汉", maxTextLen)
	raw := json.RawMessage(`{"content":"` + body + `"}`)
	var out map[string]string
	if err := json.Unmarshal(clipJSON(raw), &out); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(out["content"], '�') {
		t.Fatal("clipJSON cut a rune in half")
	}
	// clipTo appends a "\n…（已截断，共 N 字节）" suffix, so allow a little slack over the limit.
	if len(out["content"]) > maxTextLen/2+64 {
		t.Fatalf("content was not shortened: %d bytes", len(out["content"]))
	}
}

func TestRecapDropsConfigHint(t *testing.T) {
	var m Meta
	items := ParseLine(1, []byte(`{"type":"system","subtype":"away_summary","uuid":"u","content":"目标是 X。下一步 Y。 (disable recaps in /config)"}`), &m)
	if len(items) != 1 || items[0].Kind != proto.KindRecap || items[0].Text != "目标是 X。下一步 Y。" {
		t.Fatalf("items = %+v", items)
	}
}

func TestFirstUserFromContentBlocks(t *testing.T) {
	var m Meta
	ParseLine(1, []byte(`{"type":"user","uuid":"u","message":{"role":"user","content":[{"type":"text","text":"example-machine 负载"}]}}`), &m)
	if m.FirstUser != "example-machine 负载" {
		t.Fatalf("first user = %q", m.FirstUser)
	}
}

func TestQueuedInputShowsAsUserText(t *testing.T) {
	var m Meta
	items := ParseLine(1, []byte(`{"type":"attachment","uuid":"a","attachment":{"type":"queued_command","prompt":"顺便修一下这个","commandMode":"prompt","origin":{"kind":"human"}}}`), &m)
	if len(items) != 1 || items[0].Role != "user" || items[0].Kind != proto.KindText || items[0].Text != "顺便修一下这个" {
		t.Fatalf("items = %+v", items)
	}
	// Notifications the harness queues are not user input.
	if got := ParseLine(2, []byte(`{"type":"attachment","uuid":"b","attachment":{"type":"queued_command","prompt":"x","commandMode":"task-notification"}}`), &m); len(got) != 0 {
		t.Fatalf("task notification shown: %+v", got)
	}
	if got := ParseLine(3, []byte(`{"type":"attachment","uuid":"c","attachment":{"type":"environment"}}`), &m); len(got) != 0 {
		t.Fatalf("other attachment shown: %+v", got)
	}
}

func TestLastActivityIgnoresSystemLines(t *testing.T) {
	var m Meta
	ParseLine(1, []byte(`{"type":"assistant","uuid":"a","timestamp":"2026-10-20T10:00:00Z","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}`), &m)
	ParseLine(2, []byte(`{"type":"system","subtype":"away_summary","uuid":"s","timestamp":"2026-10-24T10:00:00Z","content":"recap"}`), &m)
	ParseLine(3, []byte(`{"type":"ai-title","aiTitle":"t","timestamp":"2026-10-24T11:00:00Z"}`), &m)
	if got := m.LastActivity.Format("2006-01-02"); got != "2026-10-20" {
		t.Fatalf("last activity = %s", got)
	}
}

func TestPastedContentTagsAreStripped(t *testing.T) {
	var m Meta
	items := ParseLine(1, []byte(`{"type":"user","uuid":"u","message":{"role":"user","content":"\n\n<pasted_content id=\"ae9b\">\n我用语音输入的一段话\n</pasted_content id=\"ae9b\">"}}`), &m)
	if len(items) != 1 || items[0].Text != "我用语音输入的一段话" {
		t.Fatalf("items = %+v", items)
	}
	nested := ParseLine(2, []byte(`{"type":"user","uuid":"v","message":{"role":"user","content":"a <\\pasted_content id=\"x\">b<\\/pasted_content id=\"x\">"}}`), &m)
	if len(nested) != 1 || nested[0].Text != "a b" {
		t.Fatalf("nested = %+v", nested)
	}
}

func TestQueueOperations(t *testing.T) {
	var m Meta
	for i, l := range []string{
		`{"type":"queue-operation","operation":"enqueue","content":"A"}`,
		`{"type":"queue-operation","operation":"enqueue","content":"B"}`,
		`{"type":"queue-operation","operation":"enqueue","content":"C"}`,
		`{"type":"queue-operation","operation":"dequeue"}`,
		`{"type":"queue-operation","operation":"remove","content":"C","reason":"absorbed_mid_turn"}`,
	} {
		ParseLine(i+1, []byte(l), &m)
	}
	if len(m.Queue) != 1 || m.Queue[0] != "B" {
		t.Fatalf("queue = %v", m.Queue)
	}
	ParseLine(9, []byte(`{"type":"queue-operation","operation":"popAll","content":"B"}`), &m)
	if len(m.Queue) != 0 {
		t.Fatalf("queue after popAll = %v", m.Queue)
	}
}

func TestDeveloperInfoAndQueueTags(t *testing.T) {
	var m Meta
	lines := []string{
		`{"type":"user","uuid":"u1","timestamp":"2026-10-24T10:00:00Z","gitBranch":"main","version":"2.1.310","message":{"role":"user","content":"hi"}}`,
		`{"type":"permission-mode","permissionMode":"bypassPermissions"}`,
		`{"type":"assistant","uuid":"a1","timestamp":"2026-10-24T10:00:05Z","message":{"id":"m1","role":"assistant","content":[{"type":"thinking","thinking":"x"}],"usage":{"input_tokens":2,"cache_read_input_tokens":1000,"cache_creation_input_tokens":10,"output_tokens":50}}}`,
		`{"type":"assistant","uuid":"a2","timestamp":"2026-10-24T10:00:06Z","message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":2,"cache_read_input_tokens":1000,"cache_creation_input_tokens":10,"output_tokens":50}}}`,
		`{"type":"system","subtype":"compact_boundary","uuid":"c"}`,
		`{"type":"queue-operation","operation":"enqueue","content":"\n<pasted_content id=\"ab\">\n语音输入\n</pasted_content id=\"ab\">"}`,
	}
	for i, l := range lines {
		ParseLine(i+1, []byte(l), &m)
	}
	if m.GitBranch != "main" || m.Version != "2.1.310" || m.PermissionMode != "bypassPermissions" ||
		m.ContextTokens != 1012 || m.OutputTokens != 50 || m.Compactions != 1 || m.Turns != 1 || m.StartedAt.IsZero() {
		t.Fatalf("meta = %+v", m)
	}
	if len(m.Queue) != 1 || m.Queue[0] != "语音输入" {
		t.Fatalf("queue = %q", m.Queue)
	}
}
