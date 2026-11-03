// Package spec is what the Telegram connector declares, without its dependencies: the hub registers jarvisd-telegram
// with it before the program has ever run (a switched-off connector can still be signed in to), and the program
// declares the same (connector/telegram).
package spec

import "github.com/nwverse/persona/server/internal/connector"

// ID and Name of the connector.
const (
	ID   = "telegram"
	Name = "Telegram"
)

// Manifest: read-only, so no actions (docs/plan-telegram.md).
func Manifest() connector.Manifest {
	return connector.Manifest{
		Capabilities: []connector.Capability{connector.CapStatus, connector.CapTools, connector.CapEvents, connector.CapInbox,
			connector.CapArchive, connector.CapLogin},
		Kinds: []string{"dm", "mention"},
	}
}
