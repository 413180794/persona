package hub

import (
	"encoding/json"
	"path/filepath"

	"github.com/nwverse/persona/server/internal/plugin"
	wechat "github.com/nwverse/persona/server/internal/wechat/spec"
)

// wechatManifest is the WeChat bot as a built-in plugin (docs/wechat.md): the program at path, contributing its
// connector (a status and a QR sign-in, known before the program ever runs) and its channel (docs/channels.md). It runs
// while the connector is on, like Telegram's. The hub's pipeline does the rest (where a message goes, the turn, what
// is asked): the program only talks to WeChat, and needs no hub token.
func wechatManifest(h *Hub, path string) *plugin.Manifest {
	m := wechat.Manifest()
	item := plugin.ConnectorItem{ID: wechat.ID, Name: wechat.Name}
	for _, c := range m.Capabilities {
		item.Capabilities = append(item.Capabilities, string(c))
	}
	items, _ := json.Marshal([]plugin.ConnectorItem{item})
	channels, _ := json.Marshal([]any{wechat.Channel()})
	return &plugin.Manifest{ManifestVersion: plugin.ManifestVersion, ID: wechat.ID, Name: wechat.Name, Version: h.version,
		Description: "微信机器人：在个人微信里和 Persona 对话（personad 自带）", Engines: plugin.Engines{PluginProtocol: plugin.Protocol},
		Runtime:     &plugin.Runtime{Command: plugin.Command{Path: filepath.Base(path)}},
		Contributes: map[string]json.RawMessage{"connectors": items, "channel": channels},
		Permissions: plugin.Permissions{Network: wechat.Network}}
}
