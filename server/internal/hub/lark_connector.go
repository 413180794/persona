package hub

import (
	"context"

	"github.com/chyroc/jarvis/server/internal/connector"
	"github.com/chyroc/jarvis/server/internal/kb"
)

// larkConnector is Feishu (with removed) as a connector (docs/plan-connectors.md「三、迁移」): so far its chats and
// removed posts for the knowledge base and its documents, meetings and events as items. The inbox, the tools, the
// gate's rules and 设置 › 连接器's row move to it step by step (M3–M6). The realtime events stay with the hub
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
