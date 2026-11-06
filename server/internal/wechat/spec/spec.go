// Package spec is what the WeChat plugin declares, without its dependencies: the hub registers jarvisd-wechat with it
// before the program has ever run (a switched-off connector can still be bound), and the program declares the same
// (internal/wechat).
package spec

import (
	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/connector"
)

// ID and Name of the plugin, its connector and its channel; Program is its program next to jarvisd.
const (
	ID      = "wechat"
	Name    = "微信"
	Program = "jarvisd-wechat"
)

// Network is where it goes: iLink and WeChat's media CDN.
var Network = []string{"*.weixin.qq.com"}

// MaxText is how many characters one WeChat message takes (ilink.MaxText).
const MaxText = 3500

// Manifest: a status (bound to whose WeChat, or not) and a sign-in (a QR code to scan in WeChat). Nothing goes to the
// inbox or the knowledge base: iLink only carries the chat with the bot.
func Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapStatus, connector.CapLogin}}
}

// Channel is the channel it contributes (docs/channels.md): pictures and voice (WeChat's own transcript) come in, plain
// text goes out in pieces WeChat takes, what agents ask is answered in text, "typing…" while a turn goes on.
func Channel() channel.Item {
	return channel.Item{ID: ID, Name: Name, Kind: channel.KindIM, Connector: ID, Bind: channel.BindQR,
		Description: "在个人微信里私聊机器人：扫码绑定，消息进主对话，回答以纯文字发回",
		Capabilities: channel.Capabilities{ImagesIn: true, VoiceIn: true, Format: channel.FormatPlain, MaxText: MaxText,
			Asks: channel.AsksText, Push: true, Typing: true}}
}
