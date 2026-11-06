package transcript

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// A Codex tool's input is clipped like a Claude tool_use's (clipJSON): a rollout that writes a big file carries the
// whole file in its FileChange, and that went out whole in every page and push.
func TestCodexToolInputClipped(t *testing.T) {
	big := strings.Repeat("汉字 line\n", 300_000) // ~3.6 MB
	enc := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	lines := []string{
		`{"timestamp":"2026-10-26T05:06:04.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"FileChange","id":"f1","changes":{"/r/big.txt":{"type":"add","content":` + enc(big) + `}},"status":"completed"}}}`,
		`{"timestamp":"2026-10-26T05:06:05.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"McpToolCall","id":"c1","server":"cua","tool":"js","arguments":{"code":` + enc(big) + `},"status":"completed","result":{"content":[{"type":"text","text":"ok"}]}}}}`,
		`{"timestamp":"2026-10-26T05:06:06.000Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","id":"e1","command":["/bin/zsh","-lc",` + enc("cat <<'EOF' > x\n"+big+"EOF") + `],"status":"completed","aggregated_output":"","exit_code":0}}}`,
	}
	meta := &Meta{Codex: true}
	var uses []proto.Item
	for i, l := range lines {
		for _, it := range ParseLine(i+1, []byte(l), meta) {
			if it.Kind == proto.KindToolUse {
				uses = append(uses, it)
			}
		}
	}
	if len(uses) != 3 {
		t.Fatalf("want 3 tool calls, got %d", len(uses))
	}
	for _, u := range uses {
		if len(u.ToolInput) > maxInputLen {
			t.Errorf("%s input is %d bytes, want at most %d", u.ToolName, len(u.ToolInput), maxInputLen)
		}
		var in map[string]any
		if err := json.Unmarshal(u.ToolInput, &in); err != nil {
			t.Errorf("%s input is not JSON: %v", u.ToolName, err)
		}
		if !json.Valid(u.ToolInput) || !strings.Contains(fmt.Sprint(in), "已截断") {
			t.Errorf("%s input does not say it was cut", u.ToolName)
		}
	}
	// The small fields stay whole.
	var fc map[string]string
	_ = json.Unmarshal(uses[0].ToolInput, &fc)
	if fc["file_path"] != "/r/big.txt" || fc["change"] != "add" {
		t.Errorf("FileChange fields: %v", fc)
	}
}
