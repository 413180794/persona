package hub

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/managerconf"

	"github.com/mdp/qrterminal/v3"

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
	// work, e.g. a computer behind NAT (tailcat.go, docs/server.md "tailcat hole punching"). A secret, like the token.
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
	Launchers []assistantwire.ConfiguredLauncher `json:"launchers,omitempty"`
	// Botmux false: the machine's node does not list botmux's sessions and never runs botmux (docs/engines.md "botmux");
	// unset lists them, as before.
	Botmux *bool `json:"botmux,omitempty"`
	// HerdrAutostart false: the machine's node never starts herdr's server itself when it is not running
	// (docs/engines.md "When herdr is not running"); unset starts it when needed.
	HerdrAutostart *bool `json:"herdr_autostart,omitempty"`
	// HerdrAutoinstall true: when the machine has no herdr, the node's own starts (at node start, before a start or
	// resume) install the pinned herdr first (docs/engines.md "When herdr is not installed"); unset only the Agents list's "Start" does.
	HerdrAutoinstall bool `json:"herdr_autoinstall,omitempty"`
	// ClaudeGlobalHooks true: the machine's node puts report-only hooks into its default claude config directory's
	// settings.json, so the sessions Persona did not start report their turns and cards too (docs/claude-integration.md
	// "Global report-only hooks"). Off unless set; false (or removing the key) takes them out again.
	ClaudeGlobalHooks bool `json:"claude_global_hooks,omitempty"`
	// UsageWindows false: the machine's node does not read the agents' subscription rate-limit windows (Codex's
	// rollouts, a Claude Code status line drop) and the manager does not ask it (docs/usage-windows.md, #2432); unset
	// reads them. hub.json's usage section (the account-pool gateways) is the manager's: it reads and writes it itself.
	UsageWindows *bool `json:"usage_windows,omitempty"`

	// ClaudeStartDir is the CLAUDE_CONFIG_DIR the agents Persona starts with plain claude use on this machine, for a
	// computer whose claude is logged in under a profile directory rather than ~/.claude (#1459); "~/" is the remote
	// home. Unset, claude uses its default. Launchers (a ~/.zshrc alias, a managed one) keep their own.
	ClaudeStartDir string `json:"claude_start_dir,omitempty"`
}

// TailcatConfig: DERPMapURL is a DERP map of your own relays (a derper) instead of Tailscale's public ones; the nodes
// were set up with the same (`jarvisd node tailcat init --derp-map-url`).
type TailcatConfig struct {
	DERPMapURL string `json:"derp_map_url,omitempty"`
}

// AssistantProfile is the hub's part of hub.json's assistant section; owner, notes, machine and launcher are the
// manager's, in its manager.json (internal/managerconf).
type AssistantProfile struct {
	// External: the assistant's process (personad assistant) runs elsewhere and connects with a token of its own
	// (personad hub assistant-token); the hub does not start one (assistant_process.go).
	External bool `json:"external,omitempty"`
	// PersonaView: the assistant is taught ```persona-view (a View of the extensions' components, #1850) instead of
	// ```persona-card. Off until every app the user has draws it (UI version 3); a persona-card draws on all of them.
	PersonaView bool `json:"persona_view,omitempty"`
	// HTMLViews: cards' html components may run (sandboxed, where a client passed its security checks) and call the
	// persona.reply / persona.open bridge. Only with PersonaView; nil (absent) is on, false turns them into fallback.
	HTMLViews *bool `json:"html_views,omitempty"`
}

// HTMLViewsOn is assistant.html_views with its default (on); it means nothing while PersonaView is off.
func (p AssistantProfile) HTMLViewsOn() bool { return p.HTMLViews == nil || *p.HTMLViews }

type Config struct {
	// Name is what people see this hub as (hubName); empty: its machine's host name.
	Name string `json:"name,omitempty"`
	// ShutdownTimeoutSeconds bounds the entire shutdown, including pool closure (default 8; policy, not a latency measurement).
	ShutdownTimeoutSeconds int    `json:"shutdown_timeout_seconds,omitempty"`
	Listen                 string `json:"listen"`
	Token                  string `json:"token"`
	PublicURL              string `json:"public_url"`
	// DatabaseURL points at the Postgres container (scripts/deploy/deploy.sh creates it and fills this in).
	DatabaseURL string `json:"database_url"`
	// The manager's own sections (managerconf.Moved: the connectors, extensions, bots, mail sign-ins, Lark, usage
	// sources, ai, inbox, kb, agents, hook_timeout_minutes and assistant.owner / notes / machine / launcher) are in
	// its manager.json now (docs/config.md "manager.json"): the hub reads none of them, and an older hub.json's copies
	// stay as they were.
	Machines []MachineConfig `json:"machines"`
	// Assistant personalizes the chat tab's assistant (its CLAUDE.md): whose assistant it is and anything it should
	// know about them and their setup (where code lives, which skills reach work data, machines to stay out of).
	Assistant AssistantProfile `json:"assistant"`
	// Notify tunes when "finished" pushes go out (notify.go); which ones a device gets is its own setting.
	Notify NotifyConfig `json:"notify,omitempty"`
	// Public is how the hub is reached from outside its LAN: TLS, the addresses it hands out (config_public.go).
	Public PublicConfig `json:"public,omitempty"`

	Security SecurityConfig `json:"security,omitempty"`
	// Admin is who may open the admin UI (docs/admin.md).
	Admin AdminConfig `json:"admin,omitempty"`
	// Tailcat tunes reaching machines over tailcat (tailcat.go).
	Tailcat TailcatConfig `json:"tailcat,omitempty"`
	// Mesh tunes the links to other hubs (docs/mesh.md, config_mesh.go).
	Mesh MeshConfig `json:"mesh,omitempty"`
	// Link tunes the heartbeat with the computers' Persona Links (config_link.go, docs/link.md "Heartbeat").
	Link LinkConfig `json:"link,omitzero"`
	// Personas tunes the looks: whose level a tiered look shows (persona_tiers.go, docs/persona.md).
	Personas PersonasConfig `json:"personas,omitempty"`

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
func defaultLocalMachine() MachineConfig {
	host, _ := os.Hostname()
	short := strings.Split(host, ".")[0]
	return MachineConfig{ID: short, Name: short, Kind: "local", Icon: "server"}
}

func defaultConfig() *Config {
	host, _ := os.Hostname()
	machines := []MachineConfig{defaultLocalMachine()}
	if identity.Getenv("JARVIS_LOCAL_NODE") == "0" {
		machines = []MachineConfig{}
	}
	return &Config{
		Listen:    ":18660",
		Token:     randomHex(24),
		PublicURL: fmt.Sprintf("http://%s:18660", host),
		Machines:  machines,
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
	cfg.dir = dir
	return cfg, nil
}

// noPublicURLHint is what `hub pair` says when hub.json's public_url is empty.
const noPublicURLHint = "hub 还不知道手机从哪个地址连它（public_url 是空的）：Docker 里在 .env 写上 " +
	"JARVIS_PUBLIC_URL=http://<这台机器的局域网地址>:18660，再运行 docker compose up -d；" +
	"不用 Docker 的，在 hub.json 里填 public_url 后重启 hub。也可以在管理界面的「网络」里填地址"

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
// so it can be piped (`… | jarvis-link pair -`), what to do with it on stderr. add: hub.json gets the machine first
// when it has none of that id (named name; confirmed: the master token over loopback added it).
func PairMachine(machine string, add bool, name string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	return cfg.printLinkPair(context.Background(), machine, add, name, os.Stdout, os.Stderr)
}

func (c *Config) printLinkPair(ctx context.Context, machine string, add bool, name string, out, help io.Writer) error {
	body := map[string]any{"machine": machine}
	if add {
		body = map[string]any{"add": map[string]string{"id": machine, "name": name}}
	}
	p, err := c.requestPairCodeWith(ctx, body)
	if err != nil {
		var e *pairRefusal
		if !add && errors.As(err, &e) && e.code == "unknown_machine" {
			return fmt.Errorf("hub.json 里没有要「电脑来连」的电脑「%s」；加 --add 自动添加：personad hub pair --machine %s --add [--name <名字>]", machine, machine)
		}
		return err
	}
	// A hub that does not know "add" (an older binary still running) ignores it and makes an app's code, which trades
	// for a full device credential: never hand that out as a computer's link.
	if p.Machine != machine {
		return fmt.Errorf("本机 hub 没有生成电脑「%s」的配对链接：hub 可能还没升级或没重启；升级并重启 hub 后再试", machine)
	}
	fmt.Fprintln(out, p.PairURL)
	shown := machine
	if p.Name != "" {
		shown = p.Name
	}
	if p.Added {
		fmt.Fprintf(help, "\n已添加：%s\n", shown)
	}
	fmt.Fprintf(help, "\n这是给「%s」的 Persona Link 用的配对链接，%d 分钟内有效，只能用一次。\n"+
		"在那台电脑上：菜单栏的 Persona Link › 配对 / 修改连接… 里粘贴；或者运行 jarvis-link pair '<链接>'（新装的也可以用 persona-link）。\n",
		shown, max(1, int(time.Until(p.ExpiresAt).Round(time.Minute).Minutes())))
	return nil
}

// pairRefusal is the hub not making a computer's code: its status, code and words.
type pairRefusal struct {
	status    int
	code, msg string
}

func (e *pairRefusal) Error() string {
	return fmt.Sprintf("本机 hub 没有生成配对链接（HTTP %d）：%s", e.status, e.msg)
}

func (c *Config) requestPairCode(ctx context.Context) (*pairCode, error) {
	p, err := c.requestPairCodeWith(ctx, nil)
	if err != nil {
		return nil, err
	}
	return p.pairCode, nil
}

// requestPairCodeWith asks the running hub for a pairing code over loopback with the master token: body nil for an
// app's, {"machine"} or {"add"} for a computer's.
func (c *Config) requestPairCodeWith(ctx context.Context, reqBody map[string]any) (*pairCodeReply, error) {
	_, port, err := net.SplitHostPort(c.Listen)
	if n, parseErr := strconv.Atoi(port); err != nil || parseErr != nil || n <= 0 || n > 65535 {
		return nil, fmt.Errorf("hub listen 配置没有有效端口；请检查配置，或用 --legacy 打印旧格式二维码")
	}
	endpoint := "http://" + net.JoinHostPort("127.0.0.1", port) + "/api/v1/auth/pair-codes"
	var body io.Reader
	if reqBody != nil {
		b, _ := json.Marshal(reqBody)
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
		if reqBody != nil {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
			var e struct {
				Code string `json:"code"`
			}
			_ = json.Unmarshal(b, &e)
			// With the cause the hub moved to detail (#5728).
			return nil, &pairRefusal{status: resp.StatusCode, code: e.Code, msg: apierr.Describe(b)}
		}
		return nil, fmt.Errorf("本机 hub 生成临时 token 失败（HTTP %d）；请确认 hub 已升级且配置一致，或用 --legacy 打印旧格式二维码", resp.StatusCode)
	}
	p := pairCodeReply{pairCode: &pairCode{}}
	err = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&p)
	if err == nil && p.Token != "" && p.URL == "" && time.Now().Before(p.ExpiresAt) {
		// A hub that does not know its own address (a first Docker start leaves public_url empty: the container cannot
		// see the NAS's LAN address, docs/docker.md): the code is fine, the address a phone would dial is missing.
		return nil, errors.New(noPublicURLHint)
	}
	if err != nil || p.Token == "" || p.URL == "" || !time.Now().Before(p.ExpiresAt) {
		return nil, fmt.Errorf("本机 hub 返回了无效的配对信息；请升级 hub，或用 --legacy 打印旧格式二维码")
	}
	p.PairURL = p.pairURL()
	return &p, nil
}

// writePair prints the server, token, the pairing QR code for the iPhone and the same link for the Mac (no camera:
// it pastes the link into "Connect hub").
func writePair(w io.Writer, cfg *Config) {
	fmt.Fprintf(w, "Server: %s\nToken:  %s\n\n用 Persona App 扫码配对：\n\n", cfg.PublicURL, cfg.Token)
	qrterminal.GenerateHalfBlock(cfg.PairURL(), qrterminal.L, w)
	fmt.Fprintf(w, "\nMac 上拷贝这条链接，在 Persona 的「连接 hub」里粘贴：\n%s\n", cfg.PairURL())
}

// sayMovedSections says once, as the hub starts, which of the manager's sections hub.json still has: the hub reads
// none of them (the manager reads its manager.json, which took them over once), so an edit there does nothing.
func (h *Hub) sayMovedSections() {
	if h.cfg == nil || h.cfg.dir == "" {
		return
	}
	b, err := os.ReadFile(filepath.Join(h.cfg.dir, "hub.json"))
	if err != nil {
		return
	}
	moved := managerconf.In(b)
	if len(moved) == 0 {
		return
	}
	if !h.cfg.Assistant.External {
		h.logf("config: hub.json's %s are the manager's and not read here: the built-in manager takes them over into "+
			"its manager.json (%s) once and reads them there; change them there", strings.Join(moved, ", "),
			managerconf.DefaultFile(h.cfg.dir))
		return
	}
	// The built-in manager runs elsewhere: the hello's sections are handed to it at the handshake, the rest are its
	// machine's to have.
	var handed, rest []string
	for _, k := range moved {
		switch {
		case k == "inbox": // its models and the AI twin's machine are handed over, the rest is that machine's
			handed, rest = append(handed, "inbox's models"), append(rest, "the rest of inbox")
		case managerconf.InHello(k):
			handed = append(handed, k)
		default:
			rest = append(rest, k)
		}
	}
	if len(handed) > 0 {
		h.logf("config: hub.json's %s are the manager's: the hub hands them to the built-in manager at the handshake "+
			"(assistant.external), which takes them over into the manager.json on its machine once; change them there",
			strings.Join(handed, ", "))
	}
	if len(rest) > 0 {
		h.logf("config: hub.json's %s are the manager's and read by nothing here: set them in the manager.json on the "+
			"machine the built-in manager runs on (assistant.external)", strings.Join(rest, ", "))
	}
}
