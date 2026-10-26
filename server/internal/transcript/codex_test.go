package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chyroc/jarvis/server/internal/proto"
)

func TestCodexRollout(t *testing.T) {
	lines := []string{
		`{"timestamp":"2026-10-26T05:05:58.393Z","type":"session_meta","payload":{"id":"01a1","cwd":"/r/dots","cli_version":"0.159.3"}}`,
		`{"timestamp":"2026-10-26T05:05:59.475Z","type":"turn_context","payload":{"model":"gpt-6.1-sol","approval_policy":"never"}}`,
		`{"timestamp":"2026-10-26T05:05:59.515Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","id":"u1","content":[{"type":"text","text":"why is it stuck"}]}}}`,
		`{"timestamp":"2026-10-26T05:06:00.000Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"dup"}]}}`,
		`{"timestamp":"2026-10-26T05:06:01.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","id":"m1","phase":"commentary","content":[{"type":"Text","text":"Looking at the tabs first."}]}}}`,
		`{"timestamp":"2026-10-26T05:06:02.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","id":"e1","command":["/bin/zsh","-lc","herdr agent list"],"status":"completed","aggregated_output":"ok\n","exit_code":0}}}`,
		`{"timestamp":"2026-10-26T05:06:03.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","id":"e2","command":["/bin/zsh","-lc","false"],"status":"failed","aggregated_output":"","exit_code":1}}}`,
		`{"timestamp":"2026-10-26T05:06:04.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"FileChange","id":"f1","changes":{"/r/dots/a.txt":{"type":"update","unified_diff":"@@ -1 +1 @@\n-a\n+b\n"}},"status":"completed"}}}`,
		`{"timestamp":"2026-10-26T05:06:05.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"McpToolCall","id":"c1","server":"cua","tool":"js","arguments":{"code":"1"},"status":"completed","result":{"content":[{"type":"text","text":"2"}],"isError":false}}}}`,
		`{"timestamp":"2026-10-26T05:06:06.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"output_tokens":211},"last_token_usage":{"input_tokens":26279}}}}`,
		`{"timestamp":"2026-10-26T05:06:07.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","id":"m2","phase":"final_answer","content":[{"type":"Text","text":"Tab 1 waits on a prompt."}]}}}`,
		`{"timestamp":"2026-10-26T05:06:08.000Z","type":"event_msg","payload":{"type":"turn_aborted","reason":"interrupted"}}`,
	}
	path := filepath.Join(t.TempDir(), "rollout-2026-10-26T13-05-38-01a1.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := NewFile(path)
	items, err := f.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range items {
		got = append(got, it.Role+":"+it.Kind+":"+it.ToolName)
	}
	want := []string{
		"user:text:", "assistant:thinking:",
		"assistant:tool_use:Bash", "user:tool_result:", "assistant:tool_use:Bash", "user:tool_result:",
		"assistant:tool_use:Edit", "user:tool_result:", "assistant:tool_use:mcp__cua__js", "user:tool_result:",
		"assistant:text:", "system:notice:",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("items\n got %v\nwant %v", got, want)
	}
	if string(items[2].ToolInput) != `{"command":"herdr agent list"}` || items[3].IsError || !items[5].IsError {
		t.Fatalf("bash: %s %+v %+v", items[2].ToolInput, items[3], items[5])
	}
	if !strings.Contains(string(items[6].ToolInput), `"diff":"@@ -1 +1 @@\n-a\n+b\n"`) || items[6].ToolUseID != items[7].ToolUseID {
		t.Fatalf("edit: %s", items[6].ToolInput)
	}
	m := f.Meta()
	if !m.Codex || m.Cwd != "/r/dots" || m.Model != "gpt-6.1-sol" || m.FirstUser != "why is it stuck" ||
		m.LastText != "Tab 1 waits on a prompt." || m.ContextTokens != 26279 || m.Turns != 1 {
		t.Fatalf("meta: %+v", m)
	}
	if items[11].Text != "已中断" || items[0].Kind != proto.KindText {
		t.Fatalf("notice: %+v", items[11])
	}
}
