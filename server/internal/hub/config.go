package hub

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/nwverse/persona/server/internal/identity"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mdp/qrterminal/v3"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/connector/github"
	"github.com/nwverse/persona/server/internal/connector/imessage"
	"github.com/nwverse/persona/server/internal/lark"
	"github.com/nwverse/persona/server/internal/proto"
)

type MachineConfig struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Kind: local | service (the hub dials the computer's connector, service.go) | link (the computer's jarvis-link
	// dials the hub, link.go) | ssh | tailcat (tailcat only, no ssh). ssh and tailcat are kept for hub.json files that
	// have them (docs/plan-hub-center.md 4).
	Kind    string `json:"kind"`
	SSHHost string `json:"ssh_host,omitempty"`
	// Address is a service computer's connector, host:port (its pin and credential are in Postgres); Proxy an optional
	// socks5://host:port the hub dials it through (a computer reachable only from behind another machine).
	Address string `json:"address,omitempty"`
	Proxy   string `json:"proxy,omitempty"`
	// TailcatAddr is the machine's tailcat address (`jarvisd node tailcat init` there): the way in when ssh does not
	// work, e.g. a computer behind NAT (tailcat.go, docs/server.md「tailcat 打洞」). A secret, like the token.
	TailcatAddr string `json:"tailcat_addr,omitempty"`
	Icon        string `json:"icon,omitempty"`
	// RemoteBin is the jarvisd path on the remote machine (default ~/.jarvis/bin/jarvisd).
	RemoteBin string `json:"remote_bin,omitempty"`
	// ClaudeConfigDirs are Claude Code config directories (CLAUDE_CONFIG_DIR of launchers) besides ~/.claude to look
	// in for sessions; those of running claudes are found by themselves. "~/" is the remote home.
	ClaudeConfigDirs []string            `json:"claude_config_dirs,omitempty"`
	CodexDiscovery   *bool               `json:"codex_discovery,omitempty"`
	CodexHomes       []string            `json:"codex_homes,omitempty"`
	CodexServers     []proto.CodexServer `json:"codex_servers,omitempty"`
	// Launchers adds configured wrappers or overrides the type reported by the node.
	Launchers []sessionLauncher `json:"launchers,omitempty"`
	// Botmux false: the machine's node does not list botmux's sessions and never runs botmux (docs/engines.md「botmux」);
	// unset lists them, as before.
	Botmux *bool `json:"botmux,omitempty"`
	// HerdrAutostart false: the machine's node never starts herdr's server itself when it is not running
	// (docs/engines.md「herdr 没在运行时」); unset starts it when needed.
	HerdrAutostart *bool `json:"herdr_autostart,omitempty"`

	// ClaudeStartDir is the CLAUDE_CONFIG_DIR the agents Persona starts with plain claude use on this machine, for a
	// computer whose claude is logged in under a profile directory rather than ~/.claude (#1459); "~/" is the remote
	// home. Unset, claude uses its default. Launchers (a ~/.zshrc alias, a managed one) keep their own.
	ClaudeStartDir string `json:"claude_start_dir,omitempty"`
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
	// External: the assistant's process (personad assistant) runs elsewhere and connects with a token of its own
	// (personad hub assistant-token); the hub does not start one (assistant_process.go).
	External bool `json:"external,omitempty"`
	// PersonaView: the assistant is taught ```persona-view (a View of the extensions' components, #1850) instead of
	// ```persona-card. Off until every app the user has draws it (UI version 3); a persona-card draws on all of them.
	PersonaView bool `json:"persona_view,omitempty"`
}

// MicrosoftConfig is Outlook's "Sign in with Microsoft" (docs/mail.md「Outlook 接入方式」): an optional default client
// ID (the user normally enters their own in the app; it is never built in) and the redirect URI registered with it.
type MicrosoftConfig struct {
	ClientID    string `json:"client_id,omitempty"`
	RedirectURI string `json:"redirect_uri,omitempty"`
}

type Config struct {
	// Name is what people see this hub as (hubName); empty: its machine's host name.
	Name string `json:"name,omitempty"`
	// ShutdownTimeoutSeconds bounds the entire shutdown, including pool closure (default 8; policy, not a latency measurement).
	ShutdownTimeoutSeconds int    `json:"shutdown_timeout_seconds,omitempty"`
	Listen                 string `json:"listen"`
	Token                  string `json:"token"`
	PublicURL              string `json:"public_url"`
	HookTimeoutMinutes     int    `json:"hook_timeout_minutes"`
	// DatabaseURL points at the Postgres container (scripts/deploy/deploy.sh creates it and fills this in).
	DatabaseURL string `json:"database_url"`
	// AI configures the Messages API used for in-app AI helpers (defaults to the launcher's gateway on the hub machine).
	AI ai.Config `json:"ai"`
	// Lark points at the user's lark-connector agent API (Feishu messages in the 信息 tab).
	Lark lark.Config `json:"lark"`
	// LarkGate: extra rules for the assistant's Feishu writes through the lark-connector skill, before the built-in ones
	// (node/larkgate.go; e.g. {"method":"POST","path":"/conversations/*/messages","action":"allow"}).
	LarkGate []proto.LarkRule `json:"lark_gate,omitempty"`
	// ConnectorGate: rules for connectors' writes (github_comment…), the first match decides; without one the user
	// allows each write on the phone (internal/connector/gate.go; e.g. {"connector":"github","action":"github_comment",
	// "match":{"repo":"example-org/*"},"decision":"allow"}).
	ConnectorGate []connector.Rule `json:"connector_gate,omitempty"`
	// GitHub is the GitHub connector: whose gh login it reads (connector/github, docs/plan-connectors.md).
	GitHub *github.Config `json:"github,omitempty"`
	// IMessage is the Messages connector: which Mac's Messages the app may read (connector/imessage). The messages
	// are not given to any model.
	IMessage *imessage.Config `json:"imessage,omitempty"`
	// Google is the iOS OAuth client behind Gmail's "Sign in with Google" (docs/mail.md).
	Google GoogleConfig `json:"google"`
	// Microsoft: Outlook's sign-in (optional; the app asks for the client ID when there is none).
	Microsoft MicrosoftConfig `json:"microsoft,omitempty"`
	Machines  []MachineConfig `json:"machines"`
	// Assistant personalizes the 对话 tab's assistant (its CLAUDE.md): whose assistant it is and anything it should
	// know about them and their setup (where code lives, which skills reach work data, machines to stay out of).
	Assistant AssistantProfile `json:"assistant"`
	// Inbox tunes the 收件箱's AI triage (docs/inbox.md).
	Inbox InboxConfig `json:"inbox"`
	// Notify tunes when "finished" pushes go out (notify.go); which ones a device gets is its own setting.
	Notify NotifyConfig `json:"notify,omitempty"`
	// Agents: what the hub does on its own about agents' turns (docs/agent-work.md).
	Agents AgentsConfig `json:"agents,omitzero"`
	// KB is the knowledge base under ~/.jarvis/data: chat archives, compressed context, projects, tags (docs/knowledge.md).
	KB KBConfig `json:"kb,omitempty"`
	// Public is how the hub is reached from outside its LAN: TLS, the addresses it hands out (config_public.go).
	Public PublicConfig `json:"public,omitempty"`

	Security SecurityConfig `json:"security,omitempty"`
	// Admin is who may open the admin UI (docs/admin.md).
	Admin AdminConfig `json:"admin,omitempty"`
	// Extensions are processes the hub runs to add things to the app, e.g. the voice conversation
	// (docs/extensions.md「hub.json 里的扩展」); LegacyExtensions are the same under the old key "plugins", still read.
	Extensions       []ExtensionConfig `json:"extensions,omitempty"`
	LegacyExtensions []ExtensionConfig `json:"plugins,omitempty"`
	// Tailcat tunes reaching machines over tailcat (tailcat.go).
	Tailcat TailcatConfig `json:"tailcat,omitempty"`
	// Bots: talking to Persona through a chat app's bot (bots.go, docs/bots.md); switched on in the app.
	Bots BotsConfig `json:"bots,omitempty"`
	// Mesh tunes the links to other hubs (docs/mesh.md, config_mesh.go).
	Mesh MeshConfig `json:"mesh,omitempty"`

	dir string
}

func dataDir() string {
	home, _ := os.UserHomeDir()
	return identity.DataDir(home)
}

// defaultConfig is what a first run writes to ~/.jarvis/hub.json: this machine only, reachable by its host name.
// Other machines (ssh), the public URL and the assistant's notes are filled in there (docs/config.md). With
// JARVIS_LOCAL_NODE=0 (the Docker image, docs/docker.md) there is no machine at all: the hub's own has no herdr or
// claude to drive, every computer is added to hub.json.
func defaultConfig() *Config {
	host, _ := os.Hostname()
	short := strings.Split(host, ".")[0]
	machines := []MachineConfig{{ID: short, Name: short, Kind: "local", Icon: "server"}}
	if identity.Getenv("JARVIS_LOCAL_NODE") == "0" {
		machines = []MachineConfig{}
	}
	return &Config{
		Listen:             ":18660",
		Token:              randomHex(24),
		PublicURL:          fmt.Sprintf("http://%s:18660", host),
		HookTimeoutMinutes: 30,
		Machines:           machines,
	}
}

// envOverrides are settings the environment gives over hub.json's, e.g. a compose file's (docs/docker.md): the
// database's address and the hub's own address and port. Never written back to the file.
var envOverrides = []struct {
	name string
	to   func(*Config) *string
}{
	{"JARVIS_DATABASE_URL", func(c *Config) *string { return &c.DatabaseURL }},
	{"JARVIS_PUBLIC_URL", func(c *Config) *string { return &c.PublicURL }},
	{"JARVIS_LISTEN", func(c *Config) *string { return &c.Listen }},
}

func (c *Config) applyEnv() {
	for _, e := range envOverrides {
		if v := identity.Getenv(e.name); v != "" {
			*e.to(c) = v
		}
	}
}

// LoadConfig reads ~/.jarvis/hub.json, creating it with defaults (and a fresh token) on first run.
func LoadConfig() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir, err := identity.PrepareHome(home)
	if err != nil {
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
		// A hub.json written by someone else first (scripts/deploy/db.sh only puts database_url in it) has no token: keep
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
	cfg.applyEnv()
	if cfg.Token == "" {
		return nil, fmt.Errorf("%s: token is empty", path)
	}
	if err := cfg.Lark.Validate(); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := connector.ValidateRules(cfg.ConnectorGate); err != nil {
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

func Pair(legacy bool, android ...bool) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	return cfg.printPair(context.Background(), legacy, os.Stdout, android...)
}

func (c *Config) printPair(ctx context.Context, legacy bool, out io.Writer, android ...bool) error {
	withAndroid := len(android) > 0 && android[0]
	if legacy && withAndroid {
		return fmt.Errorf("--android 需要临时 token，不能与 --legacy 同时使用")
	}
	if legacy {
		writePair(out, c)
		return nil
	}
	p, err := c.requestPairCode(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "地址：%s\n配对链接：%s\n\n用 Persona App 扫码配对：\n\n", p.URL, p.PairURL)
	qrterminal.GenerateHalfBlock(p.PairURL, qrterminal.L, out)
	fmt.Fprintf(out, "\n有效期至：%s\n", p.ExpiresAt.UTC().Format(time.RFC3339))
	if p.FP != "" {
		fmt.Fprintf(out, "公钥指纹：%s\n", p.FP)
	}
	if withAndroid {
		fmt.Fprintf(out, "Android 安装包：%s/api/v1/app/android/apk?%s\n", strings.TrimRight(c.PublicURL, "/"), url.Values{"token": {p.Token}}.Encode())
	}
	return nil
}

// PairMachine prints a pairing link for one link machine's jarvis-link (docs/link.md): the link alone on stdout,
// so it can be piped (`… | jarvis-link pair -`), what to do with it on stderr.
func PairMachine(machine string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	return cfg.printLinkPair(context.Background(), machine, os.Stdout, os.Stderr)
}

func (c *Config) printLinkPair(ctx context.Context, machine string, out, help io.Writer) error {
	p, err := c.requestPairCodeFor(ctx, machine)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, p.PairURL)
	fmt.Fprintf(help, "\n这是给「%s」的 Persona Link 用的配对链接，%d 分钟内有效，只能用一次。\n"+
		"在那台电脑上：菜单栏的 Persona Link › 配对 / 修改连接… 里粘贴；或者运行 jarvis-link pair '<链接>'（新装的也可以用 persona-link）。\n",
		machine, max(1, int(time.Until(p.ExpiresAt).Round(time.Minute).Minutes())))
	return nil
}

func (c *Config) requestPairCode(ctx context.Context) (*pairCode, error) {
	return c.requestPairCodeFor(ctx, "")
}

func (c *Config) requestPairCodeFor(ctx context.Context, machine string) (*pairCode, error) {
	_, port, err := net.SplitHostPort(c.Listen)
	if n, parseErr := strconv.Atoi(port); err != nil || parseErr != nil || n <= 0 || n > 65535 {
		return nil, fmt.Errorf("hub listen 配置没有有效端口；请检查配置，或用 --legacy 打印旧格式二维码")
	}
	endpoint := "http://" + net.JoinHostPort("127.0.0.1", port) + "/api/v1/auth/pair-codes"
	var body io.Reader
	if machine != "" {
		b, _ := json.Marshal(map[string]string{"machine": machine})
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 5 * time.Second, Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接本机 hub：hub 可能未运行，或未监听回环地址；请先启动 hub，或用 --legacy 打印旧格式二维码")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if machine != "" {
			var e struct {
				Error string `json:"error"`
			}
			_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&e)
			return nil, fmt.Errorf("本机 hub 没有生成配对链接（HTTP %d）：%s", resp.StatusCode, e.Error)
		}
		return nil, fmt.Errorf("本机 hub 生成临时 token 失败（HTTP %d）；请确认 hub 已升级且配置一致，或用 --legacy 打印旧格式二维码", resp.StatusCode)
	}
	var p pairCode
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&p); err != nil || p.Token == "" || p.URL == "" || !time.Now().Before(p.ExpiresAt) {
		return nil, fmt.Errorf("本机 hub 返回了无效的配对信息；请升级 hub，或用 --legacy 打印旧格式二维码")
	}
	p.PairURL = p.pairURL()
	return &p, nil
}

// writePair prints the server, token, the pairing QR code for the iPhone and the same link for the Mac (no camera:
// it pastes the link into 连接 hub).
func writePair(w io.Writer, cfg *Config) {
	fmt.Fprintf(w, "Server: %s\nToken:  %s\n\n用 Persona App 扫码配对：\n\n", cfg.PublicURL, cfg.Token)
	qrterminal.GenerateHalfBlock(cfg.PairURL(), qrterminal.L, w)
	fmt.Fprintf(w, "\nMac 上拷贝这条链接，在 Persona 的「连接 hub」里粘贴：\n%s\n", cfg.PairURL())
}
