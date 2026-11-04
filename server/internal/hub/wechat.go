package hub

import (
	"encoding/json"
	"path/filepath"

	"github.com/nwverse/persona/server/internal/extension"
	wechat "github.com/nwverse/persona/server/internal/wechat/spec"
)

// wechatManifest is the WeChat bot as a built-in plugin (docs/wechat.md): the program at path, contributing its
// connector (a status and a QR sign-in, known before the program ever runs) and its channel (docs/channels.md). It runs
// while the connector is on. The hub's pipeline does the rest (where a message goes, the turn, what
// is asked): the program only talks to WeChat, and needs no hub token.
func wechatManifest(h *Hub, path string) *extension.Manifest {
	m := wechat.Manifest()
	item := extension.ConnectorItem{ID: wechat.ID, Name: wechat.Name}
	for _, c := range m.Capabilities {
		item.Capabilities = append(item.Capabilities, string(c))
	}
	items, _ := json.Marshal([]extension.ConnectorItem{item})
	channels, _ := json.Marshal([]any{wechat.Channel()})
	return &extension.Manifest{ManifestVersion: extension.ManifestVersion, ID: wechat.ID, Name: wechat.Name, Version: h.version,
		Description: "微信机器人：在个人微信里和 Persona 对话（personad 自带）", Engines: extension.Engines{ExtensionProtocol: extension.Protocol},
		Runtime:     &extension.Runtime{Command: extension.Command{Path: filepath.Base(path)}},
		Contributes: map[string]json.RawMessage{"connectors": items, "channel": channels},
		Permissions: extension.Permissions{Network: wechat.Network}}
}
