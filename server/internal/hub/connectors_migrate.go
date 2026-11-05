package hub

import (
	"slices"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/connector/proc"
)

// A plugin may take over one of the hub's own connectors while it moves out of the hub (connector.MigratableConnectors:
// Feishu through lark-connector, docs/plan-extensions.md「第 3 步」). The id stays (the knowledge base's chats lark-<feed>,
// the inbox's source and mutes, 设置 › 连接器 all go by it): while the plugin's connector is registered the hub's own
// is out of the registry, and the hub's own loops for it stand down (larkByPlugin); when the plugin is switched off or
// removed, the hub's comes back.

// yieldOwnConnector takes the hub's own connector id out of the registry for a plugin's (a migratable id only).
func (h *Hub) yieldOwnConnector(id string) {
	if !slices.Contains(connector.MigratableConnectors, id) {
		return
	}
	own := h.connectors.Get(id)
	if own == nil {
		return
	}
	if _, isExtension := own.(*proc.Conn); isExtension {
		return
	}
	h.migrated.Store(id, own)
	h.connectors.Remove(id)
	h.logf("connector %s: a plugin takes it over", id)
}

// restoreOwnConnector puts the hub's own connector id back (the plugin's is gone, or could not be added).
func (h *Hub) restoreOwnConnector(id string) {
	v, ok := h.migrated.LoadAndDelete(id)
	if !ok || h.connectors.Get(id) != nil {
		return
	}
	if err := h.addConnector(v.(connector.Connector)); err != nil {
		h.logf("connector %s: back to the hub's own: %v", id, err)
		return
	}
	h.logf("connector %s: back to the hub's own", id)
}

// removePluginConnector takes a plugin's connector out, and gives a migratable id back to the hub.
func (h *Hub) removeExtensionConnector(id string) {
	h.connectors.Remove(id)
	h.restoreOwnConnector(id)
}

// larkByPlugin: a plugin serves Feishu (its connector "lark" is registered): Feishu items keep their blocks and go
// back to it (the manager's inbox_migrate.go; their replies come here as inbox.reply).
func (h *Hub) larkByExtension() bool {
	if h.connectors == nil {
		return false
	}
	return h.connectors.Get("lark") != nil // the hub has none of its own any more
}
