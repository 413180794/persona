package hub

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/mdp/qrterminal/v3"

	"github.com/chyroc/jarvis/server/internal/ai"
	"github.com/chyroc/jarvis/server/internal/lark"
	"github.com/chyroc/jarvis/server/internal/proto"
)

type MachineConfig struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"` // local | ssh | tailcat (tailcat only, no ssh)
	SSHHost string `json:"ssh_host,omitempty"`
	// TailcatAddr is the machine's tailcat address (`jarvisd node tailcat init` there): the way in when ssh does not
	// work, e.g. a computer behind NAT (tailcat.go, docs/server.md「tailcat 打洞」). A secret, like the token.
	TailcatAddr string `json:"tailcat_addr,omitempty"`
	Icon        string `json:"icon,omitempty"`
	// RemoteBin is the jarvisd path on the remote machine (default ~/.jarvis/bin/jarvisd).
	RemoteBin string `json:"remote_bin,omitempty"`
	// ClaudeConfigDirs are Claude Code config directories (CLAUDE_CONFIG_DIR of launchers) besides ~/.claude to look
	// in for sessions; those of running claudes are found by themselves. "~/" is the remote home.
	ClaudeConfigDirs []string `json:"claude_config_dirs,omitempty"`
}

// GoogleConfig is a Google Cloud OAuth client of type iOS (no secret: the client ID is public), plus the proxy
// Gmail traffic takes (some networks reach few of Google's IPv4 addresses; docs/mail.md).
type GoogleConfig struct {
	ClientID string `json:"client_id"`
	// Proxy is an http:// proxy for Gmail IMAP / SMTP and Google's OAuth / Gmail API, e.g. a local proxy on the hub's machine.
	Proxy string `json:"proxy,omitempty"`
}

// TailcatConfig: DERPMapURL is a DERP map of your own relays (a derper) instead of Tailscale's public ones; the nodes
// were set up with the same (`jarvisd node tailcat init --derp-map-url`).
type TailcatConfig struct {
	DERPMapURL string `json:"derp_map_url,omitempty"`
}

type AssistantProfile struct {
	Owner string `json:"owner,omitempty"` // how the assistant refers to the user
	Notes string `json:"notes,omitempty"` // Markdown appended to the assistant's instructions
	// Defaults until changed in the app: the machine it runs on (default: the hub's own) and the launcher.
	Machine  string `json:"machine,omitempty"`
	Launcher string `json:"launcher,omitempty"`
}

type Config struct {
	Listen             string `json:"listen"`
	Token              string `json:"token"`
	PublicURL          string `json:"public_url"`
	HookTimeoutMinutes int    `json:"hook_timeout_minutes"`
	// DatabaseURL points at the Postgres container (scripts/deploy.sh creates it and fills this in).
	DatabaseURL string `json:"database_url"`
	// AI configures the Messages API used for in-app AI helpers (defaults to the launcher's gateway on the hub machine).
	AI ai.Config `json:"ai"`
	// Lark points at the user's lark-connector agent API (Feishu messages in the 信息 tab).
	Lark lark.Config `json:"lark"`
	// LarkGate: extra rules for the assistant's Feishu writes through the lark-connector skill, before the built-in ones
	// (node/larkgate.go; e.g. {"method":"POST","path":"/conversations/*/messages","action":"allow"}).
	LarkGate []proto.LarkRule `json:"lark_gate,omitempty"`
	// Google is the iOS OAuth client behind Gmail's "Sign in with Google" (docs/mail.md).
	Google   GoogleConfig    `json:"google"`
	Machines []MachineConfig `json:"machines"`
	// Assistant personalizes the 对话 tab's assistant (its CLAUDE.md): whose assistant it is and anything it should
	// know about them and their setup (where code lives, which skills reach work data, machines to stay out of).
	Assistant AssistantProfile `json:"assistant"`
	// Inbox tunes the 收件箱's AI triage (docs/inbox.md).
	Inbox InboxConfig `json:"inbox"`
	// Notify tunes when "finished" pushes go out (notify.go); which ones a device gets is its own setting.
	Notify NotifyConfig `json:"notify,omitempty"`
	// KB is the knowledge base under ~/.jarvis/data: chat archives, compressed context, projects, tags (docs/knowledge.md).
	KB KBConfig `json:"kb,omitempty"`
	// Plugins are processes the hub runs to add things to the app, e.g. the voice conversation (docs/plugins.md).
	Plugins []PluginConfig `json:"plugins,omitempty"`
	// LarkAgent: a message of the user's starting with /agent anywhere in Feishu starts an agent with the context of
	// where it was asked, and its answer is posted as the reply (lark_agent.go, docs/lark-agent.md).
	LarkAgent LarkAgentConfig `json:"lark_agent,omitempty"`
	// Tailcat tunes reaching machines over tailcat (tailcat.go).
	Tailcat TailcatConfig `json:"tailcat,omitempty"`

	dir string
}

func dataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".jarvis")
}

// defaultConfig is what a first run writes to ~/.jarvis/hub.json: this machine only, reachable by its host name.
// Other machines (ssh), the public URL and the assistant's notes are filled in there (docs/config.md).
func defaultConfig() *Config {
	host, _ := os.Hostname()
	short := strings.Split(host, ".")[0]
	return &Config{
		Listen:             ":18660",
		Token:              randomHex(24),
		PublicURL:          fmt.Sprintf("http://%s:18660", host),
		HookTimeoutMinutes: 30,
		Machines:           []MachineConfig{{ID: short, Name: short, Kind: "local", Icon: "server"}},
	}
}

// LoadConfig reads ~/.jarvis/hub.json, creating it with defaults (and a fresh token) on first run.
func LoadConfig() (*Config, error) {
	dir := dataDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "hub.json")
	cfg := defaultConfig()
	b, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		out, _ := json.MarshalIndent(cfg, "", "  ")
		if err := os.WriteFile(path, out, 0o600); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(b, cfg); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		// A hub.json written by someone else first (scripts/db.sh only puts database_url in it) has no token: keep
		// the generated one, or every start (and `jarvisd hub pair`) would make up a different one.
		var raw map[string]any
		if err := json.Unmarshal(b, &raw); err == nil {
			if _, ok := raw["token"]; !ok {
				raw["token"] = cfg.Token
				out, _ := json.MarshalIndent(raw, "", "  ")
				if err := os.WriteFile(path, out, 0o600); err != nil {
					return nil, err
				}
			}
		}
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("%s: token is empty", path)
	}
	if err := cfg.Lark.Validate(); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.HookTimeoutMinutes <= 0 {
		cfg.HookTimeoutMinutes = 30
	}
	cfg.dir = dir
	return cfg, nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// PairURL is what the app's QR scanner understands.
func (c *Config) PairURL() string {
	return "jarvis://pair?" + url.Values{"url": {c.PublicURL}, "token": {c.Token}}.Encode()
}

func Pair() error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	fmt.Printf("Server: %s\nToken:  %s\n\n用 Jarvis App 扫码配对：\n\n", cfg.PublicURL, cfg.Token)
	qrterminal.GenerateHalfBlock(cfg.PairURL(), qrterminal.L, os.Stdout)
	fmt.Println()
	return nil
}
