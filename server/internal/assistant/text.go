package assistant

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nwverse/persona/sdk/go/wire"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/textfmt"
)

// Formatting for tool results and the hub's logs, the same the hub writes with (one copy, internal/textfmt).
var (
	clip       = textfmt.Clip
	oneLine    = textfmt.OneLine
	firstLine  = textfmt.FirstLine
	ago        = textfmt.Ago
	short      = textfmt.Short
	agentLabel = textfmt.AgentLabel
	stateNames = textfmt.StateNames
)

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
			text := it.Text
			if it.Role == "assistant" {
				text = wire.AssistantCardText(text)
			}
			fmt.Fprintf(&b, "%s: %s\n\n", who, clip(text, 4000))
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
