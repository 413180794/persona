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
	"github.com/nwverse/persona/server/internal/managerconf"
	"github.com/nwverse/persona/server/internal/proto"
)

// Config is the manager's configuration for the extensions, connectors and bots: Listen, Token, PublicURL and Inbox
// are hub.json's (the hub's own file, read as it is), the rest the manager's own manager.json's (managerconf.Moved,
// docs/config.md "manager.json").
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

	dir      string
	warnings []string // said once when the manager starts (loadConfig)
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

// loadConfig reads dir's hub.json with the environment over it as the hub does (PERSONA_PUBLIC_URL, PERSONA_LISTEN),
// and the manager's sections from file (its manager.json): hub.json's copies of those are not read. file "": all of
// it from hub.json (tests). A missing file is an empty one.
func loadConfig(dir, file string) (*Config, error) {
	cfg := &Config{Listen: ":18660", dir: dir}
	b, err := os.ReadFile(filepath.Join(dir, "hub.json"))
	switch {
	case err == nil:
		if file != "" {
			o, err := managerconf.ParseObject(b)
			if err != nil {
				return cfg, fmt.Errorf("parse hub.json: %w", err)
			}
			b = managerconf.Without(o, managerconf.Moved)
		}
		if err := json.Unmarshal(b, cfg); err != nil {
			return cfg, fmt.Errorf("parse hub.json: %w", err)
		}
	case !os.IsNotExist(err):
		return cfg, err
	}
	if file != "" {
		// A manager.json that does not read leaves its sections out, and says so: hub.json's part still holds.
		if mine, err := managerconf.Sections(file); err != nil {
			cfg.warnings = append(cfg.warnings, fmt.Sprintf("%v: its sections are not read until it is fixed", err))
		} else if err := json.Unmarshal(mine, cfg); err != nil {
			cfg.warnings = append(cfg.warnings, fmt.Sprintf("parse %s: %v: its sections are not read until it is fixed", file, err))
			cfg = cfgWithout(cfg)
		}
	}
	if v := identity.Getenv("JARVIS_PUBLIC_URL"); v != "" {
		cfg.PublicURL = v
	}
	if v := identity.Getenv("JARVIS_LISTEN"); v != "" {
		cfg.Listen = v
	}
	if err := connector.ValidateRules(cfg.ConnectorGate); err != nil {
		cfg.ConnectorGate = nil
		return cfg, fmt.Errorf("connector_gate: %w", err)
	}
	if err := cfg.Lark.Validate(); err != nil { // was the hub's check at start; now the manager's, which uses it
		cfg.warnings = append(cfg.warnings, err.Error()+": lark is not read")
		cfg.Lark = lark.Config{}
	}
	if cfg.Lark.LegacyGatewayConfigured() {
		cfg.warnings = append(cfg.warnings, "lark.container / token / base_url are not read any more: Feishu through a Lark "+
			"gateway is an extension's now; set them in its settings (docs/feishu.md)")
	}
	return cfg, nil
}

// cfgWithout is cfg with only hub.json's part (after manager.json's sections failed to unmarshal halfway).
func cfgWithout(cfg *Config) *Config {
	return &Config{Listen: cfg.Listen, Token: cfg.Token, PublicURL: cfg.PublicURL, Inbox: cfg.Inbox, dir: cfg.dir,
		warnings: cfg.warnings}
}
