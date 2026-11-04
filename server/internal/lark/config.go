// Package lark is hub.json's "lark": how the hub and the assistant run lark-cli, Feishu's official open-platform CLI
// (docs/plan-lark-cli.md). Feishu through lark-connector (the user's own web client) is a plugin's now
// (docs/plan-plugins.md「第 3 步」); its old fields are still read, only to say so.
package lark

import "errors"

// Config is hub.json's lark.
type Config struct {
	// CLI is lark-cli; nil: not used.
	CLI *CLIConfig `json:"cli,omitempty"`
	// Prefer is the primary source when a plugin serves Feishu as well: "lark-connector" or "cli".
	Prefer string `json:"prefer,omitempty"`

	// Account, BaseURL, Container, Token and NativeOLKToken pointed the hub at lark-connector before a plugin served it:
	// not used any more (the hub logs a line when they are there), except Account, the lark-connector account the
	// Feishu items a plugin hands over came to.
	Account        int    `json:"account,omitempty"`
	BaseURL        string `json:"base_url,omitempty"`
	Container      string `json:"container,omitempty"`
	Token          string `json:"token,omitempty"`
	NativeOLKToken string `json:"native_olk_token,omitempty"`
}

// CLIConfig is how the hub and the assistant run lark-cli. Its app and the user's login stay in lark-cli's own store.
type CLIConfig struct {
	Bin     string `json:"bin,omitempty"`     // command name or absolute path; default lark-cli, looked up in a login shell
	Profile string `json:"profile,omitempty"` // lark-cli --profile; "" is its default profile
}

// MaxAccount is the largest account number.
const MaxAccount = 999999999

// lark-connectorConfigured: the old lark-connector fields are there (the hub says they are not read any more).
func (cfg Config) lark-connectorConfigured() bool { return cfg.Token != "" || cfg.Container != "" }

func (cfg Config) Validate() error {
	if cfg.Account < 0 || cfg.Account > MaxAccount {
		return errors.New("lark.account must be an integer between 1 and 999999999 (or 0 for the primary account)")
	}
	switch cfg.Prefer {
	case "", "lark-connector", "cli":
	default:
		return errors.New(`lark.prefer must be "lark-connector" or "cli"`)
	}
	return nil
}
