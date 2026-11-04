// Package voice is the built-in voice plugin (`jarvisd voice`, docs/extensions.md「内置语音扩展」): full-duplex voice
// conversations in the app over a realtime service that speaks the GPT-Live wire. The hub starts it like any plugin
// and relays /api/v1/plugins/<id>/… to the HTTP server it serves on JARVIS_PLUGIN_SOCKET.
//
// One plugin can have several upstreams (providers): each is a /v1/live endpoint, its dialect, its credential. The
// models of all of them make one list, and the model the app picks decides which upstream a conversation goes to.
package voice

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/nwverse/persona/server/internal/identity"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Dialects of the GPT-Live wire.
const (
	// DialectLive: the public GPT-Live API (session.start, session.input_audio.append, session.delegation.created +
	// response.event{function_call}, session.commentary.append), also served by deployments that speak it for their
	// own models. Function tools are declared in the session.
	DialectLive = "live"
	// DialectCodex: what Codex speaks to gpt-live-1-codex (codex-rs "frameless bidi", OpenAI-Alpha: quicksilver=v2):
	// session.update, input_audio.append, delegation.created{item} and delegation.context.append. No function tools:
	// the model hands work over as delegations.
	DialectCodex = "codex"
)

// Config is the plugin's configuration: hub.json's plugin entry `config`, handed over as JARVIS_PLUGIN_CONFIG.
type Config struct {
	Providers []Provider `json:"providers"`
	// Default is the model a conversation uses when the app names none ("<provider>/<model>"); empty: the first
	// provider's default.
	Default string `json:"default,omitempty"`
	// Owner is how the voice model calls the user; Notes is appended to its instructions.
	Owner string `json:"owner,omitempty"`
	Notes string `json:"notes,omitempty"`
}

// Provider is one upstream realtime service.
type Provider struct {
	ID      string `json:"id"`             // a-z0-9_-, the prefix of its model ids
	Name    string `json:"name,omitempty"` // shown after the model's name when there are several providers
	URL     string `json:"url"`            // the session socket: wss://<host>/v1/live
	Dialect string `json:"dialect,omitempty"`
	// The credential, sent as Authorization: Bearer: Key itself, the first line of KeyFile, or what KeyCommand (sh -c)
	// prints. A command is run again for every conversation, so a credential renewed elsewhere is picked up.
	Key        string `json:"key,omitempty"`
	KeyFile    string `json:"key_file,omitempty"`
	KeyCommand string `json:"key_command,omitempty"`
	// SSH dials the service through that machine (`ssh -W host:port <ssh>`): for a hub machine that cannot reach it.
	// TLS still runs end to end from here.
	SSH string `json:"ssh,omitempty"`
	// Models offered; empty lists the service's own (live: GET <url>/models) or the dialect's default.
	Models       []string `json:"models,omitempty"`
	DefaultModel string   `json:"default_model,omitempty"`
	// Tools (live dialect): declare Persona's function tools in session.start, for a deployment that accepts them
	// there. The public GPT-Live API refuses session.tools: without them the model's client delegations go to the
	// Persona main conversation, as with the codex dialect.
	Tools bool `json:"tools,omitempty"`
	// InputRate is the microphone's sample rate (16000 or 24000); 0 is the dialect's (live 16000, codex 24000).
	InputRate int `json:"input_rate,omitempty"`
}

var (
	providerIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
	modelPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
	voicePattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
)

// LoadConfig reads JARVIS_PLUGIN_CONFIG.
func LoadConfig() (Config, error) {
	raw := strings.TrimSpace(identity.Getenv("JARVIS_PLUGIN_CONFIG"))
	if raw == "" {
		return Config{}, errors.New("JARVIS_PLUGIN_CONFIG is empty: give the plugin entry in hub.json a config with providers (docs/extensions.md)")
	}
	var c Config
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return Config{}, fmt.Errorf("JARVIS_PLUGIN_CONFIG: %w", err)
	}
	return c, c.validate()
}

func (c *Config) validate() error {
	if len(c.Providers) == 0 {
		return errors.New("config: no providers")
	}
	seen := map[string]bool{}
	for i := range c.Providers {
		p := &c.Providers[i]
		if !providerIDPattern.MatchString(p.ID) {
			return fmt.Errorf("config: provider id %q: a-z0-9_-, at most 32", p.ID)
		}
		if seen[p.ID] {
			return fmt.Errorf("config: provider %s twice", p.ID)
		}
		seen[p.ID] = true
		if p.Dialect == "" {
			p.Dialect = DialectLive
		}
		if p.Dialect != DialectLive && p.Dialect != DialectCodex {
			return fmt.Errorf("config: provider %s: dialect %q (live or codex)", p.ID, p.Dialect)
		}
		if !strings.HasPrefix(p.URL, "wss://") && !strings.HasPrefix(p.URL, "ws://") {
			return fmt.Errorf("config: provider %s: url must be ws:// or wss://", p.ID)
		}
		if p.InputRate != 0 && p.InputRate != 16000 && p.InputRate != 24000 {
			return fmt.Errorf("config: provider %s: input_rate is 16000 or 24000", p.ID)
		}
		for _, m := range append(p.Models, p.DefaultModel) {
			if m != "" && !modelPattern.MatchString(m) {
				return fmt.Errorf("config: provider %s: model %q", p.ID, m)
			}
		}
	}
	return nil
}

func (p *Provider) name() string {
	if p.Name != "" {
		return p.Name
	}
	return p.ID
}

func (p *Provider) tools() bool { return p.Dialect == DialectLive && p.Tools }

func (p *Provider) inputRate() int {
	switch {
	case p.InputRate != 0:
		return p.InputRate
	case p.Dialect == DialectCodex:
		return 24000
	default:
		return 16000
	}
}

// defaultModel is the provider's own default: configured, else the first configured, else the dialect's.
func (p *Provider) defaultModel() string {
	switch {
	case p.DefaultModel != "":
		return p.DefaultModel
	case len(p.Models) > 0:
		return p.Models[0]
	case p.Dialect == DialectCodex:
		return "gpt-live-1-codex"
	}
	return ""
}

// splitModel parses "<provider>/<model>".
func splitModel(id string) (provider, model string, ok bool) {
	provider, model, ok = strings.Cut(id, "/")
	return provider, model, ok && providerIDPattern.MatchString(provider) && modelPattern.MatchString(model)
}

func expandHome(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, rest)
	}
	return p
}
