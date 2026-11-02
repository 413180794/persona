package hub

import (
	"context"
	"encoding/json"

	"github.com/chyroc/jarvis/server/internal/connector"
	"github.com/chyroc/jarvis/server/internal/kb"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// larkConnector is Feishu (with removed) as a connector (docs/plan-connectors.md「三、迁移」): its chats and removed posts
// for the knowledge base, its documents, meetings and events as items, its inbox (inbox_own.go) and its read tools.
// The gate's rules and 设置 › 连接器's row move to it in M5–M6. The realtime events stay with the hub
// (lark_events.go): they also refresh the app's 信息 tab and personal agents, and start /agent at once.
type larkConnector struct{ h *Hub }

func (larkConnector) ID() string   { return "lark" }
func (larkConnector) Name() string { return "飞书" }

// ChatSources: Feishu's chats (lark-…) and the removed posts I am part of (moments-…), in this order.
func (c larkConnector) ChatSources() []kb.Source {
	return []kb.Source{&larkSource{c: c.h.lark}, &momentsSource{c: c.h.lark}}
}

func (c larkConnector) ItemSource() kb.ItemSource {
	return &larkItems{c: c.h.lark, loc: c.h.userLocation()}
}

// legacyConnectors have their own rows in 设置 › 连接器 (FeishuConnectorView, the mail page) until M6: GET
// /api/v1/connectors leaves them out, or the app would show them twice.
var legacyConnectors = map[string]bool{"lark": true, "mail": true}

func (h *Hub) listedConnectors(ctx context.Context) []connector.Info {
	out := []connector.Info{}
	for _, info := range h.connectors.Infos(ctx) {
		if !legacyConnectors[info.ID] {
			out = append(out, info)
		}
	}
	return out
}

// Tools: reading Feishu (prompts/mcp/lark), the assistant's only. Writes go through the lark-connector skill and the
// node's gate (lark_gate); lark_agent_reply answers a /agent request and stays the hub's.
func (larkConnector) Tools() []proto.Tool {
	return []proto.Tool{mcpTool("lark_chats"), mcpTool("lark_messages"), mcpTool("lark_search")}
}

func (c larkConnector) Call(ctx context.Context, name string, args json.RawMessage) (string, error) {
	return c.h.runLarkTool(ctx, name, args)
}
