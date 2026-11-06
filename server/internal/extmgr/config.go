package extmgr

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/connector/github"
	"github.com/nwverse/persona/server/internal/connector/imessage"
	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/lark"
	"github.com/nwverse/persona/server/internal/proto"
)

// Config is what the manager reads of hub.json for the extensions, connectors and bots: the same keys the hub read
// for them (the file is the hub's; the hub's admin edits keep every key).
type Config struct {
	Listen    string `json:"listen"`
	Token     string `json:"token"`
	PublicURL string `json:"public_url"`
	// Lark: the lark-cli path and the Feishu account (docs/feishu.md).
	Lark lark.Config `json:"lark"`
	// LarkGate and ConnectorGate: the rules for the assistant's Feishu writes and the connectors' writes
	// (internal/connector/gate.go).
	LarkGate      []proto.LarkRule `json:"lark_gate,omitempty"`
	ConnectorGate []connector.Rule `json:"connector_gate,omitempty"`
	GitHub        *github.Config   `json:"github,omitempty"`
	IMessage      *imessage.Config `json:"imessage,omitempty"`
	// Google and Microsoft: mail's sign-ins (docs/mail.md).
	Google                  GoogleConfig      `json:"google"`
	Microsoft               MicrosoftConfig   `json:"microsoft,omitempty"`
	Inbox                   InboxConfig       `json:"inbox"`
	Bots                    BotsConfig        `json:"bots,omitempty"`
	Extensions              []ExtensionConfig `json:"extensions,omitempty"`
	LegacyExtensions        []ExtensionConfig `json:"plugins,omitempty"`
	TrustedExtensionMarkets []string          `json:"trusted_extension_markets,omitempty"`

	// SentLedgerDays is how many days the ledger of what was written as the user is kept (sent_ledger.go): 0 or
	// absent is 90, below 0 keeps it for good.
	SentLedgerDays int `json:"sent_ledger_days,omitempty"`

	dir string
}

// GoogleConfig is the iOS OAuth client behind Gmail's "Sign in with Google" (docs/mail.md).
type GoogleConfig struct {
	ClientID string `json:"client_id"`
	// Proxy is an http:// proxy for Gmail IMAP / SMTP and Google's OAuth / Gmail API.
	Proxy string `json:"proxy,omitempty"`
}

// MicrosoftConfig is Outlook's "Sign in with Microsoft" (docs/mail.md "Connecting Outlook").
type MicrosoftConfig struct {
	ClientID    string `json:"client_id,omitempty"`
	RedirectURI string `json:"redirect_uri,omitempty"`
}

// loadConfig reads dir's hub.json with the environment over it as the hub does (PERSONA_PUBLIC_URL, PERSONA_LISTEN);
// a missing file is an empty one.
func loadConfig(dir string) (*Config, error) {
	cfg := &Config{Listen: ":18660", dir: dir}
	b, err := os.ReadFile(filepath.Join(dir, "hub.json"))
	switch {
	case err == nil:
		if err := json.Unmarshal(b, cfg); err != nil {
			return cfg, fmt.Errorf("parse hub.json: %w", err)
		}
	case !os.IsNotExist(err):
		return cfg, err
	}
	if v := identity.Getenv("JARVIS_PUBLIC_URL"); v != "" {
		cfg.PublicURL = v
	}
	if v := identity.Getenv("JARVIS_LISTEN"); v != "" {
		cfg.Listen = v
	}
	if err := connector.ValidateRules(cfg.ConnectorGate); err != nil {
		cfg.ConnectorGate = nil
		return cfg, fmt.Errorf("hub.json connector_gate: %w", err)
	}
	return cfg, nil
}
