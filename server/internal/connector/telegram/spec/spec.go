// Package spec is what the Telegram connector declares, without its dependencies: the hub registers jarvisd-telegram
// with it before the program has ever run (a switched-off connector can still be signed in to), and the program
// declares the same (connector/telegram).
package spec

import (
	"slices"

	"github.com/nwverse/persona/server/internal/connector"
)

// ID and Name of the connector.
const (
	ID   = "telegram"
	Name = "Telegram"
)

// CLITools and CLIActions are what the tg CLI calls (its reads, its writes): unlisted, so the models' tool lists keep
// only the assistant's three read tools; the telegram skill tells the assistant how to use the CLI.
var (
	CLITools = []string{"telegram_list_chats", "telegram_read_messages", "telegram_search_messages", "telegram_get_message",
		"telegram_get_chat", "telegram_list_members"}
	CLIActions = []string{"telegram_send", "telegram_forward", "telegram_react", "telegram_mark_read"}
)

// Manifest: the assistant's read tools, the CLI's reads and its writes (actions: each through the hub's gate).
func Manifest() connector.Manifest {
	return connector.Manifest{
		Capabilities: []connector.Capability{connector.CapStatus, connector.CapTools, connector.CapActions, connector.CapEvents,
			connector.CapInbox, connector.CapArchive, connector.CapLogin},
		Unlisted: append(slices.Clone(CLITools), CLIActions...),
		Kinds:    []string{"dm", "mention"},
	}
}
