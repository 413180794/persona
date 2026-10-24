package hub

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/mdp/qrterminal/v3"

	"github.com/chyroc/jarvis/server/internal/ai"
)

type MachineConfig struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"` // local | ssh
	SSHHost string `json:"ssh_host,omitempty"`
	Icon    string `json:"icon,omitempty"`
	// RemoteBin is the jarvisd path on the remote machine (default ~/.jarvis/bin/jarvisd).
	RemoteBin string `json:"remote_bin,omitempty"`
}

type Config struct {
	Listen             string `json:"listen"`
	Token              string `json:"token"`
	PublicURL          string `json:"public_url"`
	HookTimeoutMinutes int    `json:"hook_timeout_minutes"`
	// DatabaseURL points at the Postgres container (scripts/deploy.sh creates it and fills this in).
	DatabaseURL string `json:"database_url"`
	// AI configures the Messages API used for in-app AI helpers (defaults to the cc_ds gateway on example-machine).
	AI       ai.Config       `json:"ai"`
	Machines []MachineConfig `json:"machines"`

	dir string
}

func dataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".jarvis")
}

func defaultConfig() *Config {
	host, _ := os.Hostname()
	return &Config{
		Listen:             ":18660",
		Token:              randomHex(24),
		PublicURL:          fmt.Sprintf("http://%s.example.com:18660", host),
		HookTimeoutMinutes: 30,
		Machines: []MachineConfig{
			{ID: "example-machine", Name: "公司 example-machine", Kind: "local", Icon: "server"},
			{ID: "example-machine", Name: "公司 Mac", Kind: "ssh", SSHHost: "example-machine", Icon: "laptop"},
			{ID: "example-machine", Name: "家里的公司 Mac", Kind: "ssh", SSHHost: "example-machine", Icon: "laptop"},
			{ID: "mac-home", Name: "家里的 Mac", Kind: "ssh", SSHHost: "mac-home", Icon: "desktop"},
		},
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
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("%s: token is empty", path)
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
