package assistant

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/proto"
)

// Formatting for tool results and the hub's logs. The hub has the same small helpers for its own callers.

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func ago(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟前", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d 小时前", int(d.Hours()))
	default:
		return fmt.Sprintf("%d 天前", int(d.Hours()/24))
	}
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

var stateNames = map[string]string{
	proto.StateNeedsYou: "需要你", proto.StateWorking: "工作中", proto.StateUnread: "完成未看", proto.StateIdle: "空闲",
	proto.StateOffline: "离线", proto.StateUnknown: "未知",
}

// transcriptText is a conversation's recent items for a model to read (agent_read, conversation_read): the
// messages, tool calls in one line each, failed results, cut to the last 40 000 bytes.
func transcriptText(head string, items []proto.Item) string {
	var b strings.Builder
	b.WriteString(head + "\n\n")
	for _, it := range items {
		switch it.Kind {
		case proto.KindText:
			who := "Agent"
			if it.Role == "user" {
				who = "User"
				if it.From != "" {
					who = "Message from " + it.From
				}
			}
			fmt.Fprintf(&b, "%s: %s\n\n", who, clip(it.Text, 4000))
		case proto.KindThinking:
			if t := strings.TrimSpace(it.Text); t != "" {
				fmt.Fprintf(&b, "(agent, between tools) %s\n", clip(oneLine(t), 400))
			}
		case proto.KindToolUse:
			fmt.Fprintf(&b, "→ %s %s\n", it.ToolName, clip(oneLine(toolSummary(it.ToolName, it.ToolInput)), 200))
		case proto.KindToolResult:
			if it.IsError {
				fmt.Fprintf(&b, "  ✗ %s\n", clip(oneLine(it.Text), 300))
			}
		case proto.KindNotice, proto.KindRecap:
			if it.Text != "" {
				fmt.Fprintf(&b, "(%s)\n", clip(oneLine(it.Text), 300))
			}
		case proto.KindImage:
			b.WriteString("[image]\n")
		}
	}
	out := b.String()
	if len(out) > 40000 {
		out = "…(earlier part cut)\n" + out[len(out)-40000:]
	}
	return out
}

// toolSummary is the one-line gist of a tool call (command, file, url…).
func toolSummary(name string, input json.RawMessage) string {
	var m map[string]any
	_ = json.Unmarshal(input, &m)
	for _, k := range []string{"command", "file_path", "url", "query", "pattern", "description", "prompt", "path"} {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	b, _ := json.Marshal(m)
	return string(b)
}
