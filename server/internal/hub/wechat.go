package hub

import (
	"encoding/json"
	"path/filepath"

	"github.com/nwverse/persona/server/internal/plugin"
	wechat "github.com/nwverse/persona/server/internal/wechat/spec"
)

// wechatManifest is the WeChat bot as a built-in plugin (docs/wechat.md): the program at path, contributing its
// connector (a status and a QR sign-in, known before the program ever runs) and hearing the agents' events. It runs
// while the connector is on, like Telegram's, and reaches the hub's API with the hub's token (hub_api): it sends the
// owner's WeChat messages to the main conversation and answers what agents ask.
func wechatManifest(h *Hub, path string) *plugin.Manifest {
	m := wechat.Manifest()
	item := plugin.ConnectorItem{ID: wechat.ID, Name: wechat.Name}
	for _, c := range m.Capabilities {
		item.Capabilities = append(item.Capabilities, string(c))
	}
	items, _ := json.Marshal([]plugin.ConnectorItem{item})
	events, _ := json.Marshal([]plugin.EventsItem{{ID: wechat.ID, Subscribe: wechat.Topics}})
	return &plugin.Manifest{ManifestVersion: plugin.ManifestVersion, ID: wechat.ID, Name: wechat.Name, Version: h.version,
		Description: "微信机器人：在个人微信里和 Persona 对话（personad 自带）", Engines: plugin.Engines{PluginProtocol: plugin.Protocol},
		Runtime:     &plugin.Runtime{Command: plugin.Command{Path: filepath.Base(path)}},
		Contributes: map[string]json.RawMessage{"connectors": items, "events": events},
		Permissions: plugin.Permissions{HubAPI: true, Network: wechat.Network}}
}
