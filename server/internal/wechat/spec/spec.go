// Package spec is what the WeChat plugin declares, without its dependencies: the hub registers jarvisd-wechat with it
// before the program has ever run (a switched-off connector can still be bound), and the program declares the same
// (internal/wechat).
package spec

import "github.com/nwverse/persona/server/internal/connector"

// ID and Name of the plugin and its connector; Program is its program next to jarvisd.
const (
	ID      = "wechat"
	Name    = "微信"
	Program = "jarvisd-wechat"
)

// Topics are the hub's events it hears: an agent it drives asks something (sent to WeChat as text), or is done.
var Topics = []string{"agent.needs_you", "agent.finished"}

// Network is where it goes: iLink and WeChat's media CDN.
var Network = []string{"*.weixin.qq.com"}

// Manifest: a status (bound to whose WeChat, or not) and a sign-in (a QR code to scan in WeChat). Nothing goes to the
// inbox or the knowledge base: iLink only carries the chat with the bot.
func Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapStatus, connector.CapLogin}}
}
