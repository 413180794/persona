// Package proto holds the wire types shared by hub, node and clients. See docs/protocol.md.
package proto

import (
	"encoding/base64"
	"encoding/json"
	"image"
	_ "image/gif" // image sizes for lazy placeholders
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/screen"
)

type Machine struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Kind          string    `json:"kind"` // local | ssh | tailcat | link
	Icon          string    `json:"icon"` // server | laptop | desktop
	SSHHost       string    `json:"ssh_host,omitempty"`
	Online        bool      `json:"online"`
	Error         string    `json:"error,omitempty"`
	Hostname      string    `json:"hostname,omitempty"`
	OS            string    `json:"os,omitempty"`
	Arch          string    `json:"arch,omitempty"`
	Home          string    `json:"home,omitempty"`
	HerdrVersion  string    `json:"herdr_version,omitempty"`
	ClaudeVersion string    `json:"claude_version,omitempty"`
	LastSeen      time.Time `json:"last_seen,omitempty"`
	// NodeVersion is the machine's jarvisd build; NodeMismatch: not the hub's build (deploy.sh missed it, or
	// deployed it alone). Hub and node read terminal dialogs with the same code, so a mismatch can refuse answers.
	NodeVersion  string `json:"node_version,omitempty"`
	NodeMismatch bool   `json:"node_mismatch,omitempty"`
	// Deploying: the hub is installing its own node build there (POST …/deploy); DeployError: why the last one failed.
	Deploying   bool   `json:"deploying,omitempty"`
	DeployError string `json:"deploy_error,omitempty"`
	// CanDeploy is explicit so clients can distinguish an older hub without capability reporting.
	CanDeploy    *bool  `json:"can_deploy,omitempty"`
	DeployReason string `json:"deploy_reason,omitempty"`
	DeployPhase  string `json:"deploy_phase,omitempty"`
	// System: OS and hardware, from the node's last connection (kept across hub restarts).
	System *System `json:"system,omitempty"`
	// Link: kind link, while the computer's jarvis-link is connected (docs/link.md).
	Link *MachineLink `json:"link,omitempty"`
	// Transport is how the hub reaches it now: local | ssh | tailcat | link (empty while offline); Tailcat: it can be
	// reached over tailcat (an address is set); TailcatPath, over tailcat: direct (NAT traversal worked) | relay.
	Transport   string `json:"transport,omitempty"`
	Tailcat     bool   `json:"tailcat,omitempty"`
	TailcatPath string `json:"tailcat_path,omitempty"`
	// Engine is the terminal host the machine's node drives (EngineHerdr when never chosen); Engines, what the node
	// found on the machine (empty while offline). docs/protocol.md「引擎」.
	Engine  string   `json:"engine"`
	Engines []Engine `json:"engines"`
	// Computer: operating it through Cua Driver, while the plugin cua is on and it is a Mac (Computer).
	Computer *Computer `json:"computer,omitempty"`
	// Browser: its ego lite, while the plugin ego is on and it is a Mac (Browser).
	Browser *Browser `json:"browser,omitempty"`
	// Hub mesh (docs/mesh.md): the hub that connects to this machine (its owner), its name, the machine's own id
	// (~/.jarvis/machine-uid, empty for an old node or one never reached) and how the hub the client talks to gets
	// there. A hub without peers fills them in for its own machines too.
	HubID      string        `json:"hub_id,omitempty"`
	HubName    string        `json:"hub_name,omitempty"`
	MachineUID string        `json:"machine_uid,omitempty"`
	Route      *MachineRoute `json:"route,omitempty"`
	// Paths: every way the center has to this computer (docs/plan-hub-center.md 5), when there is more than one or it
	// is a member hub's; the machine detail and the admin page show them.
	Paths []MachinePath `json:"paths,omitempty"`
}

// MachineRoute is how the center reaches a machine now (docs/plan-hub-center.md 5): via one of its own ways (local |
// service | link | ssh | tailcat, hops 0) or through a member hub (hub, hops 1); RTTMS is the node's round trip
// when it was measured.
type MachineRoute struct {
	Via   string `json:"via"`
	Hops  int    `json:"hops"`
	RTTMS *int   `json:"rtt_ms,omitempty"`
}

// MachinePath is one way to a machine: how, through which hub (the center itself for its own), whether it can be used
// now, its last round trip, and whether it is the one in use.
type MachinePath struct {
	Via    string `json:"via"`
	HubID  string `json:"hub_id"`
	OK     bool   `json:"ok"`
	RTTMS  int    `json:"rtt_ms,omitempty"`
	Active bool   `json:"active,omitempty"`
}

// MachineLink is a live jarvis-link: its build, the hub URL it went to and since when.
type MachineLink struct {
	Version string    `json:"version,omitempty"`
	Address string    `json:"address,omitempty"`
	Since   time.Time `json:"since"`
}

// Terminal engines: the terminal host(s) a machine's node drives, chosen per machine. EngineHerdrBoth runs classic
// herdr and herdr-core side by side (docs/engines.md「并列」): a machine used both locally and through the Herdr
// app's remote sessions.
const (
	EngineHerdr     = "herdr"      // the classic herdr CLI
	EngineHerdrCore = "herdr-core" // the runtime of the Herdr desktop app
	EngineHerdrBoth = "herdr-both" // classic herdr and herdr-core together
	EngineOrca      = "orca"       // the Orca desktop app's runtime
	// EnginePaseo is not a terminal: the Paseo daemon's agents, through its runtime (node/runtimecontrol.go,
	// docs/paseo.md). New sessions and sessions to continue go to Paseo.
	EnginePaseo = "paseo"
)

// EngineNames are the engines a machine may use, with the names the app shows.
var EngineNames = map[string]string{
	EngineHerdr:     "herdr",
	EngineHerdrCore: "Herdr App",
	EngineHerdrBoth: "herdr + Herdr App",
	EngineOrca:      "Orca",
	EnginePaseo:     "Paseo",
}

// Engine is one engine as found on a machine: Available when its socket is reachable (herdr: or its binary is there).
// Note says why one that is there cannot be used (Orca too old or too new), in Chinese for the app.
type Engine struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	Note      string `json:"note,omitempty"`
}

// Computer is a Mac the assistant may operate through Cua Driver (the built-in plugin cua, docs/computer-use.md). In
// proto.Machine only while the plugin is on, and only for a Mac. Enabled: the user turned it on for this machine; the
// rest is what the node found when last asked (computer.status), and only for an enabled one.
type Computer struct {
	Enabled   bool   `json:"enabled"`
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	// Daemon: Cua Driver's daemon (CuaDriver.app) runs; nothing works without it.
	Daemon bool `json:"daemon"`
	// Accessibility, ScreenRecording: the daemon's macOS permissions, granted | missing | unknown.
	Accessibility   string `json:"accessibility,omitempty"`
	ScreenRecording string `json:"screen_recording,omitempty"`
	// Desktop: active (this user's desktop is on screen) | locked (its screen is locked) | away (logged out, or another
	// user is on the console). Cua Driver needs it active.
	Desktop string `json:"desktop,omitempty"`
	// ScreenSharing: macOS's Screen Sharing answers on 127.0.0.1:5900, so the app can show this Mac's live screen
	// (docs/computer-use.md「实时画面」). Not needed to operate it.
	ScreenSharing bool `json:"screen_sharing"`
	// NoAutoLock: 「扩展开着时不自动锁屏」 is on for this Mac (the hub's, from the plugin's settings).
	NoAutoLock bool `json:"no_auto_lock,omitempty"`
	// Ready: all of the above in place. Note says what is missing and what to do, in Chinese for the app.
	Ready   bool      `json:"ready"`
	Note    string    `json:"note,omitempty"`
	Checked time.Time `json:"checked,omitempty"`
	// Job: installing Cua Driver or asking for its permissions from the app (computer.install / computer.grant),
	// the one running or the last one since the node started.
	Job *ComputerJob `json:"job,omitempty"`
}

// Browser is a Mac's ego lite, the agent browser the assistant may drive (the built-in plugin ego,
// docs/plan-ego-browser.md). In proto.Machine only while the plugin is on, and only for a Mac; Enabled: the user turned
// it on for this machine. The rest is what the node found when last asked (browser.status).
type Browser struct {
	Enabled bool `json:"enabled"`
	// Installed: /Applications/ego lite.app is there; Version, its version.
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	// SetUp: its first-run setup is done (it registers ego-browser and records it in its Local State); Imported: that
	// setup imported another browser's data (sign-ins, cookies…).
	SetUp    bool `json:"set_up"`
	Imported bool `json:"imported"`
	// Analytics: Chromium's usage statistics, on | off | unknown.
	Analytics string `json:"analytics,omitempty"`
	// Ready: a read-only call through ego-browser worked a moment ago. Note: what is missing, in Chinese for the app.
	Ready         bool         `json:"ready"`
	Note          string       `json:"note,omitempty"`
	ScreenSharing bool         `json:"screen_sharing"`
	Job           *ComputerJob `json:"job,omitempty"`
	Checked       time.Time    `json:"checked,omitempty"`
}

// BrowserCookie is one cookie imported into ego lite's default profile (hub → node browser.cookies.import, #1304). The
// client parses the file the user picked (cookies.txt or an extension's JSON) into these; the hub passes them on and
// keeps nothing. Expires is Unix seconds, 0 for a session cookie; HostOnly: Domain is the host itself (no leading dot).
type BrowserCookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path,omitempty"`
	Expires  float64 `json:"expires,omitempty"`
	Secure   bool    `json:"secure,omitempty"`
	HTTPOnly bool    `json:"http_only,omitempty"`
	SameSite string  `json:"same_site,omitempty"` // Strict | Lax | None
	HostOnly bool    `json:"host_only,omitempty"`
}

// BrowserCookieSite is a site with cookies in ego lite's profile (browser.cookies.list): never a value or a name.
type BrowserCookieSite struct {
	Domain  string    `json:"domain"` // the registrable part shown to the user: example.com for .example.com and www.example.com
	Count   int       `json:"count"`
	Expires time.Time `json:"expires,omitzero"` // the latest, zero when all are session cookies
}

// BrowserCookies answers browser.cookies.*: the sites now (after an import or a clear), and what was done.
type BrowserCookies struct {
	Sites    []BrowserCookieSite `json:"sites"`
	Imported int                 `json:"imported,omitempty"`
	Skipped  int                 `json:"skipped,omitempty"` // expired or malformed
	Removed  int                 `json:"removed,omitempty"`
}

// ComputerJob is one install or grant the node runs in the background (docs/computer-use.md「一键安装」).
type ComputerJob struct {
	Kind  string `json:"kind"`  // install | grant | open
	State string `json:"state"` // running | done | failed | cancelled
	// Step: what it does now, in Chinese for the app; Error: why it failed (the installer's last lines).
	Step    string    `json:"step,omitempty"`
	Error   string    `json:"error,omitempty"`
	Started time.Time `json:"started"`
}

// ComputerResult is what one Cua Driver tool answered (node computer.call): its JSON (Output), or Text when it was not
// JSON, and the screenshot it took, if any.
type ComputerResult struct {
	Output json.RawMessage `json:"output,omitempty"`
	Text   string          `json:"text,omitempty"`
	Image  *Image          `json:"image,omitempty"`
}

// Protocol is the app <-> hub protocol's major version: it goes up only for a change an older app cannot live with
// (features cover everything finer). MinAppBuild is the oldest app build (CFBundleVersion) this hub works with; 0:
// any. Both are in /api/v1/health and the WebSocket hello (docs/hub-update.md「兼容」).
const (
	Protocol    = 1
	MinAppBuild = 0
)

// Features are what this hub supports beyond the first protocol, listed in the WebSocket hello and /health for
// clients that need to know before they try (docs/protocol.md).
var Features = []string{
	"assistant_compact",      // POST /api/v1/assistant/conversations/{id}/compact, session.compaction
	"assistant_main_new",     // POST /api/v1/assistant/main/new: archive the main conversation, the next message starts one
	"android_update",         // authenticated Android release metadata and APK downloads
	"error_codes",            // stable HTTP error codes and optional delivery information
	"client_hello",           // client platform, version and capabilities
	"heartbeat",              // per-connection heartbeat negotiation
	"app_ping",               // application ping/pong with a string id
	"ws_notify",              // notification alerts for capable connected clients
	"elicitation",            // MCP forms and URLs for opted-in clients
	"lock_test",              // scheduled device notification tests with durable receipts
	"model_configuration",    // unified per-purpose models and provider selection
	"client_msg_id",          // session.send dedupes retries
	"client_ops",             // durable operation receipts and read-only status queries
	"send_refusals",          // 409 dialog_open / input_draft, clear_draft
	"screen_dialog",          // interaction.dialog, Resolution.option, 409 prompt_changed
	"resumable_interactions", // hook questions survive hub / node restarts
	"secret_prompts",         // interaction.secret
	"request_id",             // X-Request-Id
	"terminal_stream",        // WebSocket terminal_subscribe: the pane's live frames (docs/terminal.md)
	"terminal_control",       // terminal_subscribe with control: the phone drives the terminal at its size; terminal_input
	"terminal_scroll",        // terminal_input with scroll: the wheel over a terminal the app drives, to herdr (docs/terminal.md)
	"terminal_click",         // terminal_input with click: a tap on a terminal the app drives, a left click there (docs/terminal.md)
	"file_download",          // GET /api/v1/machines/{mid}/file/download
	"file_stream",            // file?stream=1 (binary described, bytes by Range), file/download?inline=1, sniffed MIME
	"file_thumb",             // GET /api/v1/machines/{mid}/file/thumb (docs/protocol.md「文件预览」)
	"file_convert",           // GET /api/v1/machines/{mid}/file/convert: LibreOffice → PDF on the machine
	"git_blob",               // GET /api/v1/machines/{mid}/git/blob: a file at a version, for comparing
	"file_upload",            // POST /api/v1/machines/{mid}/files (docs/protocol.md「发送文件」)
	"tls",                    // optional TLS listener and SPKI pin (hub-changes.md 1.1)
	"endpoints",              // authenticated address discovery and capability-gated updates
	"plugins",                // GET /api/v1/plugins and the relay under /api/v1/plugins/{id}/ (docs/extensions.md)
	"plugin_manager",         // a plugin's detail, form, settings, secrets and switch under /api/v1/plugins/{id}/-/ (docs/extensions.md)
	"plugin_ui_slots",        // plugins' UI slots (nav_page, overview_card, machine_section), UI version 2, plugin_ui events (docs/extensions.md「UI 插槽」)
	"plugin_auto_upgrade",    // market plugins upgrade themselves: /api/v1/plugins/-/upgrades, …/{id}/-/auto-upgrade, plugin_upgrade events (docs/extensions.md「自动升级」)
	// The same under the new names (docs/extensions.md「命名和兼容」, #1844): /api/v1/extensions/… answering with the
	// keys extensions / extension / extension_name, extension_ui and extension_upgrade events. The plugin ones above
	// stay for the apps out there (until at least 2027-05-05).
	"extensions",             // GET /api/v1/extensions and the relay under /api/v1/extensions/{id}/
	"extension_manager",      // an extension's detail, form, settings, secrets and switch under /api/v1/extensions/{id}/-/
	"extension_ui_slots",     // UI slots under /api/v1/extensions/-/ui, extension_ui events
	"extension_auto_upgrade", // /api/v1/extensions/-/upgrades, …/{id}/-/auto-upgrade, extension_upgrade events
	"extension_market_spec",  // PUT /api/v1/extensions/-/markets/{id} {spec}: a market moves, keeping its id
	"extension_ui_v3",        // UI version 3 (markdown, table, empty, loading, search, filter, more, push / pop) and extension_ui {invalidate} (docs/extensions.md「UI 版本 3」)
	"desktops",               // register_desktop, the desktops event, NotifyPrefs.mute_while_desktop (手机静音)
	"sessions_delta",         // ?features=sessions_delta on the WebSocket: a snapshot, then sessions_delta (docs/protocol.md「会话增量」)
	"connectors",             // GET /api/v1/connectors (docs/plan-connectors.md)
	"channels",               // GET /api/v1/channels and each channel's settings, binding, test (docs/channels.md)
	"runtime_cli",            // phone_runtime_cli kept out of the phone's tools, POST /api/v1/phone/runtime-cli (docs/runtime-cli.md)
}

// Files sent from the phone (POST /api/v1/machines/{mid}/files): the hub streams the body to the node in chunks.
const (
	MaxUploadBytes   = 200 << 20
	UploadChunkBytes = 1 << 20
	// CodeTooLarge: over MaxUploadBytes (413); CodeNodeOutdated: the machine's node predates the feature (409).
	CodeTooLarge     = "too_large"
	CodeNodeOutdated = "node_outdated"
)

// Codes of a failed credential.gh_token (hub → node, node/credential.go).
const (
	CodeNoGh        = "no_gh"
	CodeNotSignedIn = "not_signed_in"
)

// Codes of a failed connector.chats / connector.older (hub → node, node/connectors.go): macOS does not let the
// node read the connector's data (Full Disk Access), or no conversation has that id.
const (
	CodeNoPermission = "no_permission"
	CodeNoChat       = "no_chat"
)

// UploadedFile is where an upload landed on the machine.
type UploadedFile struct {
	Path string `json:"path"` // absolute
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type NodeInfo struct {
	NodeUpdate         bool    `json:"node_update,omitempty"`
	NodeUpdateReason   string  `json:"node_update_reason,omitempty"`
	ModelConfiguration bool    `json:"model_configuration,omitempty"`
	Hostname           string  `json:"hostname"`
	OS                 string  `json:"os"`
	Arch               string  `json:"arch"`
	Home               string  `json:"home"`
	DataDir            string  `json:"data_dir,omitempty"`
	HerdrVersion       string  `json:"herdr_version"`
	ClaudeVersion      string  `json:"claude_version"`
	Version            string  `json:"version"`
	System             *System `json:"system,omitempty"`
	// Engine is the engine the node drives now; Engines, what it found (proto.Machine.Engines). Older nodes leave
	// both out.
	Engine  string   `json:"engine,omitempty"`
	Engines []Engine `json:"engines,omitempty"`
	// MachineUID is the computer's own id (~/.jarvis/machine-uid, a UUIDv4 made on the node's first run): hubs that
	// both reach it tell it is the same computer (docs/mesh.md). Older nodes leave it out.
	MachineUID string `json:"machine_uid,omitempty"`
	// HerdrRunning and HerdrRunningKnown: whether classic herdr's server answers on this machine now, so the Agents
	// list can offer 启动 instead of waiting for a start to fail (docs/engines.md). An older node sets neither, and
	// the hub then tells "it does not run" apart from "nobody said".
	HerdrRunning      bool `json:"herdr_running,omitempty"`
	HerdrRunningKnown bool `json:"herdr_running_known,omitempty"`
}

// System is what a machine is: OS, hardware, since when it runs (sysmon; docs/machines.md). Read once when the node
// starts and sent with node.info; the hub keeps the last one of every machine in Postgres.
type System struct {
	OS       string `json:"os"`                  // runtime.GOOS: linux | darwin
	Distro   string `json:"distro"`              // os-release ID (ubuntu, debian, fedora…), "macos"
	Like     string `json:"like,omitempty"`      // os-release ID_LIKE ("ubuntu debian" for Pop!_OS): for the icon
	Name     string `json:"name"`                // "Ubuntu 24.04.1 LTS", "macOS 26.6.2"
	Version  string `json:"version"`             // "24.04", "26.6.2"
	Build    string `json:"build,omitempty"`     // macOS build ("25G83")
	Kernel   string `json:"kernel"`              // "6.8.0-45-generic", "Darwin 25.6.0"
	Arch     string `json:"arch"`                // amd64 | arm64
	Model    string `json:"model,omitempty"`     // "Mac Studio", a PC's DMI product name
	CPU      string `json:"cpu,omitempty"`       // "Apple M1 Ultra", "AMD Ryzen 9 7950X"
	Cores    int    `json:"cores"`               // logical CPUs
	Memory   int64  `json:"memory"`              // bytes
	BootTime int64  `json:"boot_time,omitempty"` // Unix seconds
}

// MetricSample is one reading of a machine, every MetricEvery (sysmon; docs/machines.md「采集口径」). A nil field
// was not measured (the first sample has no CPU or network rate: those need two readings).
type MetricSample struct {
	T         int64     `json:"t"`               // Unix seconds
	CPU       *float64  `json:"cpu,omitempty"`   // busy %, all cores (0–100)
	Cores     []float64 `json:"cores,omitempty"` // busy % per core (Linux, Windows; the latest sample only, not kept)
	Load      *float64  `json:"load,omitempty"`  // 1-minute load average
	MemUsed   int64     `json:"mem_used"`        // bytes
	MemTotal  int64     `json:"mem_total"`
	SwapUsed  int64     `json:"swap_used"`
	SwapTotal int64     `json:"swap_total"`
	NetRx     *float64  `json:"net_rx,omitempty"` // bytes/s received on physical interfaces
	NetTx     *float64  `json:"net_tx,omitempty"` // bytes/s sent
	DiskUsed  int64     `json:"disk_used"`        // the system disk (/, or the macOS data volume), like df
	DiskTotal int64     `json:"disk_total"`       // used + available
}

// MetricPoint is one step of a machine's history (GET /api/v1/machines/{id}/metrics): the average over the step,
// or a break (Gap) where the machine sent nothing, which charts must not bridge. Memory, swap and disk are bytes used.
type MetricPoint struct {
	T    int64    `json:"t"` // Unix seconds, the step's start
	Gap  bool     `json:"gap,omitempty"`
	CPU  *float64 `json:"cpu,omitempty"`
	Load *float64 `json:"load,omitempty"`
	Mem  *float64 `json:"mem,omitempty"`
	Swap *float64 `json:"swap,omitempty"`
	Rx   *float64 `json:"rx,omitempty"`
	Tx   *float64 `json:"tx,omitempty"`
	Disk *float64 `json:"disk,omitempty"`
}

// MetricEvery is how often a node samples (and the finest step of the history).
const MetricEvery = 10 * time.Second

// Claude Code's effort levels and permission modes (node/modes.go sets them; the hub checks a scheduled task's).
var (
	EffortLevels    = []string{"low", "medium", "high", "xhigh", "max"}
	PermissionModes = []string{"default", "acceptEdits", "plan", "auto", "bypassPermissions"}
)

// ModeBypass skips every permission prompt; a headless claude only takes it at start (--permission-mode).
const ModeBypass = "bypassPermissions"

type Launcher struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	// Source: "zshrc" (a function in the machine's ~/.zshrc) or "jarvis" (managed in 设置 › 模型, launchers.go). Empty
	// from an older node: zshrc.
	Source string `json:"source,omitempty"`
	// Agent is what it starts when not Claude Code: "codex" (plain codex, or an alias running it; docs/codex.md
	// 「从手机新建」). A Codex start needs its first message with it.
	Agent string `json:"agent,omitempty"`
	// Assistant: a runtime's launcher that may run the Persona assistant (docs/runtimes.md「助手」), whatever its agent.
	Assistant bool `json:"assistant,omitempty"`
}

// LauncherDetail is what a ~/.zshrc launcher really sets (the node runs it with the agent stubbed out,
// node/launcherprobe.go): every field the page shows; secrets are never in it.
type LauncherDetail struct {
	Agent         string          `json:"agent"` // claude, codex, traex
	BaseURL       string          `json:"base_url,omitempty"`
	Auth          string          `json:"auth,omitempty"` // which variable carries the key (its value is not here)
	Models        []LauncherModel `json:"models,omitempty"`
	ContextWindow int             `json:"context_window,omitempty"`
	CompactWindow int             `json:"compact_window,omitempty"` // when it differs from the window
	Effort        string          `json:"effort,omitempty"`
	TimeoutMS     int             `json:"timeout_ms,omitempty"`
	Args          []string        `json:"args,omitempty"`
	Env           []LauncherEnv   `json:"env,omitempty"` // the other settings, secrets as •••
	Error         string          `json:"error,omitempty"`
}

// LauncherCheck is one 测试可用性 run: a real one-word request through the launcher.
type LauncherCheck struct {
	OK     bool   `json:"ok"`
	Agent  string `json:"agent,omitempty"`
	Model  string `json:"model,omitempty"` // which model answered
	Answer string `json:"answer,omitempty"`
	Millis int    `json:"millis"`
	Error  string `json:"error,omitempty"`
}

type LauncherModel struct {
	Role  string `json:"role"` // 主模型, Opus, 小模型, 子 Agent…
	Env   string `json:"env"`
	Model string `json:"model"`
}

// LauncherModels is what a launcher's agent offers in its /model menu, for 新建 Agent's model picker
// (node/launcher_models.go). No models: its agent has no list to ask for (TRAEX, a runtime's).
type LauncherModels struct {
	Agent  string       `json:"agent"`
	Models []AgentModel `json:"models"`
	// Locked: why a model picked here would not reach the agent (the launcher fixes its own --model); empty: it would.
	Locked string `json:"locked,omitempty"`
}

// AgentModel is one model of the list: Claude Code's initialize, Codex's model/list.
type AgentModel struct {
	ID          string `json:"id"` // what --model takes (Claude's aliases too: opus, sonnet…)
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Resolved: the model an alias stands for with this launcher's settings (opus → its model id).
	Resolved string `json:"resolved,omitempty"`
	// Default: what the agent runs when no model is given; picking it is the same as picking none.
	Default bool `json:"default,omitempty"`
	// Efforts: the thinking efforts it takes, weakest first; none: no effort setting.
	Efforts       []string `json:"efforts,omitempty"`
	DefaultEffort string   `json:"default_effort,omitempty"`
}

type LauncherEnv struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ManagedLauncher is a launcher kept by the hub (设置 › 模型): what `jarvisd launch <name>` sets before it runs claude.
// The key only goes to nodes (in memory), never to the app.
type ManagedLauncher struct {
	Kind       string `json:"kind,omitempty"` // launcher (default) or api
	API        string `json:"api,omitempty"`  // messages or responses for direct API providers
	Name       string `json:"name"`
	BaseURL    string `json:"base_url"`
	Model      string `json:"model"`
	SmallModel string `json:"small_model,omitempty"`
	// SubagentModel is CLAUDE_CODE_SUBAGENT_MODEL; "" keeps the model (the old behaviour).
	SubagentModel string `json:"subagent_model,omitempty"`
	// CustomModel is the gateway's /model extra entry (ANTHROPIC_CUSTOM_MODEL_OPTION) and how it reads.
	CustomModel            string `json:"custom_model,omitempty"`
	CustomModelName        string `json:"custom_model_name,omitempty"`
	CustomModelDescription string `json:"custom_model_description,omitempty"`
	// Args go before the caller's (e.g. --dangerously-skip-permissions).
	Args   []string `json:"args,omitempty"`
	APIKey string   `json:"api_key,omitempty"`
	// Auth is how the key goes to the gateway: "api_key" (x-api-key, ANTHROPIC_API_KEY; the default) or "bearer"
	// (Authorization: Bearer, ANTHROPIC_AUTH_TOKEN), for gateways that take only that.
	Auth string `json:"auth,omitempty"`
}

// BearerAuth: the launcher's key goes as Authorization: Bearer (ANTHROPIC_AUTH_TOKEN) rather than x-api-key.
func (l ManagedLauncher) BearerAuth() bool { return l.Auth == "bearer" }

type DirEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsGit bool   `json:"is_git"`
}

type RecentDir struct {
	Path     string    `json:"path"`
	Name     string    `json:"name"`
	LastUsed time.Time `json:"last_used"`
}

type DirList struct {
	Path    string      `json:"path"`
	Parent  string      `json:"parent"`
	Entries []DirEntry  `json:"entries"`
	Recent  []RecentDir `json:"recent"`
}

// Session statuses mirror herdr agent states, plus offline for transcripts with no live agent.
const (
	StatusIdle    = "idle"
	StatusWorking = "working"
	StatusBlocked = "blocked"
	StatusDone    = "done"
	StatusUnknown = "unknown"
	StatusOffline = "offline"
	// StatusInterrupted: it was running in a pane when the node restarted, which took herdr's panes with it. The
	// conversation is still resumable (session.start with resume) but nothing carries it on until the user says so
	// (#1917, interrupted.go in the node).
	StatusInterrupted = "interrupted"
)

// Unified agent states (docs/agent-states.md).
const (
	StateNeedsYou = "needs_you" // waiting for the user: a question, plan, permission or any TUI dialog
	StateWorking  = "working"
	StateUnread   = "unread" // finished, result not looked at yet
	StateIdle     = "idle"   // finished and seen, ready for input
	StateOffline  = "offline"
	StateUnknown  = "unknown"
)

type Session struct {
	MachineID        string               `json:"machine_id"`
	SessionID        string               `json:"session_id"`
	SourceKind       string               `json:"source_kind,omitempty"`
	SourceThreadID   string               `json:"source_thread_id,omitempty"`
	SourceInstanceID string               `json:"source_instance_id,omitempty"`
	ParentThreadID   string               `json:"parent_thread_id,omitempty"`
	ReadOnlyReason   string               `json:"read_only_reason,omitempty"`
	Capabilities     *SessionCapabilities `json:"capabilities,omitempty"`
	Attached         bool                 `json:"attached,omitempty"`
	Title            string               `json:"title"`
	Cwd              string               `json:"cwd"`
	// WorkspaceCwd is the node-verified physical directory for Cwd. Consumers
	// matching workspace bindings prefer it and fall back to Cwd on older nodes.
	WorkspaceCwd string `json:"workspace_cwd,omitempty"`
	Status       string `json:"status"`
	PaneID       string `json:"pane_id,omitempty"`
	TabID        string `json:"tab_id,omitempty"`
	WorkspaceID  string `json:"workspace_id,omitempty"`
	AgentName    string `json:"agent_name,omitempty"`
	// CodexRoute: how Persona reaches a Codex in a terminal (docs/codex.md「守护进程上的会话」) — "app_server" (joined on
	// the shared app-server: questions, approvals and messages go through its protocol) or "terminal" (its rollout and
	// screen, keys and typing); CodexRouteNote says why when it is the terminal.
	CodexRoute     string `json:"codex_route,omitempty"`
	CodexRouteNote string `json:"codex_route_note,omitempty"`
	// Agent is the coding agent in the pane: "claude" (also when empty, from older nodes) or "codex"; for a session a
	// runtime lists (docs/runtimes.md), the agent it says it runs ("dsh").
	Agent string `json:"agent,omitempty"`
	// AgentLabel is what the apps call the agent when they do not know it themselves: a runtime's own name ("DSH").
	// Empty for the agents the apps know (Claude Code, Codex, TRAEX).
	AgentLabel string `json:"agent_label,omitempty"`
	Launcher   string `json:"launcher,omitempty"`
	Model      string `json:"model,omitempty"`
	// PermissionMode and Effort are what Claude Code runs with now: default / acceptEdits / plan / auto /
	// bypassPermissions, and low … max. Empty while unknown (before the first turn, Codex).
	PermissionMode  string `json:"permission_mode,omitempty"`
	Effort          string `json:"effort,omitempty"`
	StartedByJarvis bool   `json:"started_by_jarvis"`
	Assistant       bool   `json:"assistant"` // runs in the assistant directory (the app's 对话 tab)
	// Archived: an assistant conversation moved out of the sidebar's list (kept, listed under 已归档; hub
	// assistant_conversations.go).
	Archived bool `json:"archived,omitempty"`
	// Compaction is the last compaction the user asked for in an assistant conversation (POST …/compact), kept by the
	// node until the next one: running, then done or failed (docs/assistant.md「手动压缩」).
	Compaction *Compaction `json:"compaction,omitempty"`
	// Channels are the channels messages came into this conversation from, newest first ("wechat", "feishu"):
	// the conversation list draws the latest's icon (docs/channels.md「来源标记」). Set by the hub.
	Channels []string `json:"channels,omitempty"`
	// Space / slot generalize where an agent lives in its host tool: for herdr a workspace and a tab
	// (docs/agent-states.md). Agents sharing a space are shown together as one collaborating group.
	SpaceKind  string `json:"space_kind,omitempty"` // herdr
	SpaceID    string `json:"space_id,omitempty"`
	SpaceName  string `json:"space_name,omitempty"`
	SpaceIndex int    `json:"space_index,omitempty"`
	SlotID     string `json:"slot_id,omitempty"`
	SlotName   string `json:"slot_name,omitempty"`
	SlotIndex  int    `json:"slot_index,omitempty"`
	// State is the unified agent state the apps render (needs_you, working, unread, idle, offline, unknown),
	// computed by the hub from the host status, pending interactions and whether the user has seen the result.
	State  string    `json:"state"`
	SeenAt time.Time `json:"seen_at,omitempty"`
	// StateSince is when the agent entered its current state, as the hub saw it (the hub restarting resets it):
	// lists sort 工作中 by it, since updated_at moves on every step.
	StateSince time.Time `json:"state_since,omitempty"`
	// Queued is input typed while the agent works, waiting to be taken (shown above the composer, not in the chat).
	Queued []string `json:"queued,omitempty"`
	// Background lists commands / agents Claude started in the background that have not reported back yet
	// (the TUI's "2 shells still running").
	Background []BackgroundTask `json:"background,omitempty"`
	// Subagents the agent launched (Agent / Task tool); each one's transcript is the session "<id>@<agent id>".
	Subagents []Subagent `json:"subagents,omitempty"`
	// Loop is the /loop driving this agent, when there is one (it wakes itself up on a schedule).
	Loop *LoopInfo `json:"loop,omitempty"`
	// Ports are the TCP ports the agent's processes listen on that answer HTTP (docs/ports.md): the dev servers
	// it started, which the apps open through the hub.
	Ports     []Port    `json:"ports,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	LastText  string    `json:"last_text,omitempty"`
	// TurnError: the agent's latest turn ended in a model API error (the connection lost mid-response, overloaded,
	// rate limited, the model service down…), as Claude Code writes it ("API Error: …"); set by the hub while the agent
	// is stopped, empty while it works or when the turn ended normally. The task it works for is interrupted, not
	// done (docs/todos.md「状态计算规则」, #1516).
	TurnError string `json:"turn_error,omitempty"`
	// Work is where the session stands in its work (docs/agent-work.md): working, blocked on a named precondition, or
	// done, with a line saying what it waits for or what it concluded. Set by the hub; absent before the hub has
	// triaged a stopped session's turn.
	Work *WorkState `json:"work,omitempty"`
	// Activity says what the agent is doing when its latest step is a tool call no text came after (「在跑测试」
	// 「在读 tasks.go」「在等 CI」, transcript.Activity), and ActivityAt when that call started: while an agent only runs
	// tools its last_text stays put for minutes, and a task card showing it looked stuck. Empty once it writes text.
	Activity   string     `json:"activity,omitempty"`
	ActivityAt *time.Time `json:"activity_at,omitempty"`
	// Rev changes whenever the transcript does (its size): the app pulls new items for every session whose rev
	// differs from what it has, without opening it. Changes travel as the light `revs` event.
	Rev int64 `json:"rev,omitempty"`
	// WokeByTask: the latest turn was started by a background task reporting back, not by the user; finishing
	// it sends no push (the user did not ask for anything).
	WokeByTask          bool `json:"woke_by_task,omitempty"`
	PendingInteractions int  `json:"pending_interactions"`
	// Suggestion is the next message Claude Code proposes after a turn (grey in its empty input box), read off the
	// screen; gone once the agent works again or a message is sent.
	Suggestion string `json:"suggestion,omitempty"`
	// Starting: Persona is opening its tab and waiting for claude (node/starting.go); messages sent now go in once it is
	// ready. StartError: the start failed (shown in the chat for a while).
	Starting   bool   `json:"starting,omitempty"`
	StartError string `json:"start_error,omitempty"`
	// Todo is the active (uncompleted, unreleased) todo this session is linked to (hub/todos.go).
	Todo *TodoRef `json:"todo,omitempty"`
	// Quiet: the agent works (or worked) for a quiet task, one the main conversation opened while the user wanted
	// those told only there (#1731): set by the hub, also once the task is done. Clients leave it out of task
	// notifications and global badges; the Agents page shows it as always.
	Quiet bool `json:"quiet,omitempty"`
	// SpawnFrom is persona_from reported by node for todo derivation (#762), stripped by hub before client push.
	SpawnFrom string `json:"spawn_from,omitempty"`
	// AttentionReason is why the session waits for the user, in the host's words (a botmux attention's reason).
	AttentionReason string `json:"attention_reason,omitempty"`
	// ExternalURL opens the session where it lives outside Persona (a botmux session's Feishu topic).
	ExternalURL string `json:"external_url,omitempty"`
}

// Work phases (WorkState.Phase) and what a blocked session waits on (WorkState.On), docs/agent-work.md.
const (
	WorkWorking = "working"
	WorkBlocked = "blocked"
	WorkDone    = "done"

	WaitUser      = "user"      // a decision, an answer, a link to open: the hub makes a card for it
	WaitAgent     = "agent"     // another agent finishing: the hub wakes this one when it stops
	WaitExternal  = "external"  // a deploy, CI, a review, a time: the hub wakes it to check
	WaitTransient = "transient" // the model service failed for a moment: the hub sends 继续
)

// WorkState is a session's work state (Session.Work).
type WorkState struct {
	Phase string `json:"phase"`
	On    string `json:"on,omitempty"`
	// Line is one line for the lists: the conclusion when done, what it waits for when blocked.
	Line string `json:"line,omitempty"`
	// Wait / Then: what it waits for and what it will do then (agent, external).
	Wait string `json:"wait,omitempty"`
	Then string `json:"then,omitempty"`
	// Every is how many minutes after the turn the hub first looks again for the wait (it doubles after each wake).
	Every int `json:"every_minutes,omitempty"`
	// WakeAt is when the hub acts next on its own: sends 继续 (transient) or wakes it to check (external, agent).
	WakeAt *time.Time `json:"wake_at,omitempty"`
	// Attempts the hub made for this wait so far, of at most Max.
	Attempts int        `json:"attempts,omitempty"`
	Max      int        `json:"max,omitempty"`
	Agent    *WorkAgent `json:"agent,omitempty"` // the agent it waits for (on agent)
	Link     *WorkLink  `json:"link,omitempty"`  // a link the user must open in time (on user)
	// Artifacts are what the work produced (done): PRs, commits, documents, builds.
	Artifacts []WorkArtifact `json:"artifacts,omitempty"`
	// Questions are what the user is to decide (on user), as the card asks them.
	Questions []Question `json:"questions,omitempty"`
	// InteractionID is the card the hub made for a decision (on user), while it is pending.
	InteractionID string `json:"interaction_id,omitempty"`
	// TurnAt is the end of the turn this state is about (the session's updated_at then); At when it was found, By how:
	// "rule" or "model".
	TurnAt time.Time `json:"turn_at,omitzero"`
	At     time.Time `json:"at,omitzero"`
	By     string    `json:"by,omitempty"`
}

// WorkAgent names another agent (WorkState.Agent).
type WorkAgent struct {
	MachineID string `json:"machine_id"`
	SessionID string `json:"session_id"`
	Label     string `json:"label,omitempty"`
}

// WorkLink is a link to open before it expires (WorkState.Link).
type WorkLink struct {
	URL       string     `json:"url"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// WorkArtifact is something a session's work produced: kind pr, commit, doc, build or url, and its reference.
type WorkArtifact struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

// TodoRef is a session's link to an active todo (docs/todos.md).
type TodoRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// SourceRuntime is the source_kind of a session an agent runtime lists (docs/runtimes.md): source_instance_id names
// the runtime ("dsh"); its capabilities come from what the runtime declared (no terminal, never attach).
const SourceRuntime = "runtime"

// Port is a listening TCP port on a machine (docs/ports.md).
type Port struct {
	Port int `json:"port"`
	// Addr is what it is bound to: 127.0.0.1, 0.0.0.0, ::, ::1 or one of the machine's addresses.
	Addr    string `json:"addr,omitempty"`
	PID     int    `json:"pid,omitempty"`
	Process string `json:"process,omitempty"` // the command, as ps / lsof name it (node, python3.12…)
	Title   string `json:"title,omitempty"`   // the <title> of its page at /
}

// Item kinds.
const (
	KindText       = "text"
	KindThinking   = "thinking"
	KindToolUse    = "tool_use"
	KindToolResult = "tool_result"
	KindNotice     = "notice"
	KindImage      = "image"
	KindRecap      = "recap" // Claude Code's "while you were away" summary
	// KindFold stands for a run of the agent's work left out of a page (Item.Fold); only for callers that ask
	// (fold_work).
	KindFold = "fold"
)

// Fold is what a KindFold item stands for: a run of tool calls with their results and thoughts, from the item it is
// named after ("fold:" + its id, at its seq and time) through ThroughID. A chat shows the run folded into counts;
// opening it fetches the run (session.items with from_seq / through_seq), which replaces the fold.
type Fold struct {
	Items      int                `json:"items"`
	Tools      int                `json:"tools"`
	Thoughts   int                `json:"thoughts"` // thoughts with words (blank ones are not shown)
	Until      time.Time          `json:"until"`
	ThroughSeq int                `json:"through_seq"`
	ThroughID  string             `json:"through_id"`
	Tasks      []FoldTaskSnapshot `json:"tasks,omitempty"`
	// Images: the images the run's calls looked at, in order (FoldImage): a chat shows its turn's under the answer
	// without opening the run.
	Images []FoldImage `json:"images,omitempty"`
}

// FoldImage is an image a tool call of a folded run looked at (docs/protocol.md「这一轮看过的图片」): one its result
// carried (ItemID, fetched from GET …/items/{item id}/image), or a picture file Read opened whose result carried none
// (only Path; the file's thumbnail is GET /machines/{mid}/file/thumb).
type FoldImage struct {
	ItemID    string `json:"item_id,omitempty"`
	ToolUseID string `json:"tool_use_id"`
	Tool      string `json:"tool"`              // the call's tool as written (Read, mcp__jarvis__computer_state)
	Source    string `json:"source"`            // read, screenshot (computer_*), browser (browser_*), mcp (another MCP tool), tool
	Path      string `json:"path,omitempty"`    // the picture file Read opened (a Codex view_image too)
	Machine   string `json:"machine,omitempty"` // screenshot / browser: the machine the call named (an id or a name)
	Mime      string `json:"mime,omitempty"`
	Size      int    `json:"size,omitempty"` // bytes, about (the base64's length × 3 / 4, like Image.Placeholder)
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
}

// FoldTaskSnapshot preserves a successful task result without its raw tool payload.
// Replacement boundaries stay ordered so partial updates can inherit older loaded tasks.
type FoldTaskSnapshot struct {
	Replace         string     `json:"replace,omitempty"`
	FallbackStatus  string     `json:"fallback_status,omitempty"`
	Entries         []FoldTask `json:"entries"`
	SourceItemID    string     `json:"source_item_id"`
	SourceToolUseID string     `json:"source_tool_use_id"`
	SourceSeq       int        `json:"source_seq"`
}

type FoldTask struct {
	ID          string  `json:"id"`
	Subject     *string `json:"subject,omitempty"`
	Description *string `json:"description,omitempty"`
	Status      *string `json:"status,omitempty"`
}

type Image struct {
	Mime string `json:"mime"`
	Data string `json:"data"`
	// Lazy: the hub left Data out (clients that asked for lazy_images); fetch it from
	// GET /api/v1/sessions/{mid}/{sid}/items/{item id}/image when the image is shown. Size is the byte count.
	// Width / Height (pixels) let the app reserve the image's space before the bytes arrive.
	Lazy   bool `json:"lazy,omitempty"`
	Size   int  `json:"size,omitempty"`
	Width  int  `json:"width,omitempty"`
	Height int  `json:"height,omitempty"`
}

// Placeholder is the image without its data: what a page carries, fetched on demand when shown.
func (img *Image) Placeholder() *Image {
	if img.Data == "" {
		return img
	}
	lazy := &Image{Mime: img.Mime, Lazy: true, Size: len(img.Data) * 3 / 4}
	// Only the header is read: the decoder stops once it knows the size.
	if c, _, err := image.DecodeConfig(base64.NewDecoder(base64.StdEncoding, strings.NewReader(img.Data))); err == nil {
		lazy.Width, lazy.Height = c.Width, c.Height
	}
	return lazy
}

// WithoutImageData returns items whose images carry no data, only a placeholder to fetch on demand: chat pages
// with screenshots inline ran to megabytes.
func WithoutImageData(items []Item) []Item {
	var out []Item
	for i, it := range items {
		if it.Image == nil || it.Image.Data == "" {
			continue
		}
		if out == nil {
			out = append([]Item(nil), items...)
		}
		out[i].Image = it.Image.Placeholder()
	}
	if out == nil {
		return items
	}
	return out
}

// Item is one content block of a Claude Code transcript.
type Item struct {
	ID        string          `json:"id"`
	Seq       int             `json:"seq"`
	TS        time.Time       `json:"ts"`
	Role      string          `json:"role"`
	Kind      string          `json:"kind"`
	Text      string          `json:"text,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	ToolName  string          `json:"tool_name,omitempty"`
	ToolInput json.RawMessage `json:"tool_input,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	Image     *Image          `json:"image,omitempty"`
	Queued    bool            `json:"queued,omitempty"` // user input typed while the agent worked, absorbed mid-turn
	// ClientMsgID: this item came from the message an app sent with this client_msg_id (session.send): its words,
	// its pictures, or the notice it became (/compact). Set by the node, which ties the transcript's line to the send
	// (node/echo.go); absent for anything else and from older nodes. docs/protocol.md「待发消息的对应」.
	ClientMsgID string `json:"client_msg_id,omitempty"`
	// FullSize: Text was cut for pages and pushes (it ends in a "已截断，点开看全文" line); the whole of it, this many
	// bytes, comes from GET /api/v1/sessions/{mid}/{sid}/items/{item id}/full.
	FullSize int `json:"full_size,omitempty"`
	// Wake marks a background task's notice that started new work after the agent had finished its turn
	// (not one absorbed mid-turn): the exchange before it is over and its last words are an answer.
	Wake bool `json:"wake,omitempty"`
	// From names the Claude session that sent this message (cross-session messaging), not the user.
	From string `json:"from,omitempty"`
	Fold *Fold  `json:"fold,omitempty"`
	// Task: this notice is a background task reporting back (a command, a Monitor, a subagent).
	Task         *TaskNote     `json:"task,omitempty"`
	TurnDuration *TurnDuration `json:"turn_duration,omitempty"`
	// Compact: this notice is where the context was compacted (Claude Code's compact_boundary).
	Compact *Compact `json:"compact,omitempty"`
}

// Compact says how a compaction came about: Trigger "manual" (/compact) or "auto", and the context's size before it.
type Compact struct {
	Trigger   string `json:"trigger,omitempty"`
	PreTokens int    `json:"pre_tokens,omitempty"`
}

// Compaction states.
const (
	CompactionRunning = "running"
	CompactionDone    = "done"
	CompactionFailed  = "failed"
)

// Compaction is a compaction the user asked for: State running, done or failed (Error says why).
type Compaction struct {
	State      string     `json:"state"`
	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// TurnDuration preserves a recorded whole-turn duration and its native association.
type TurnDuration struct {
	DurationMS int64  `json:"duration_ms"`
	Source     string `json:"source"`
	TurnID     string `json:"turn_id,omitempty"`
	ParentUUID string `json:"parent_uuid,omitempty"`
	UserItemID string `json:"user_item_id,omitempty"`
}

// TaskNote is what a <task-notification> says, read from the notification alone (the call it came from is found by
// the app, through TaskID / ToolUseID): a command run in the background finishing, a Monitor's event or its end, a
// subagent finishing. Title, Detail and Text come decoded (Claude Code writes &gt; &lt; &amp; in them).
type TaskNote struct {
	Kind   string `json:"kind"`             // shell | monitor | agent | task (anything else)
	Status string `json:"status,omitempty"` // completed | failed | killed | stopped | event | ended | waiting
	// Title: the call's description, or the first line of its command when it had none.
	Title      string `json:"title"`
	TaskID     string `json:"task_id,omitempty"`
	ToolUseID  string `json:"tool_use_id,omitempty"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	OutputFile string `json:"output_file,omitempty"`
	// Detail: a Monitor's event, a subagent's result, or a command's whole text when the title is only its first line.
	Detail string `json:"detail,omitempty"`
}

// Interaction kinds.
const (
	IxQuestion    = "question"
	IxPlan        = "plan"
	IxPermission  = "permission"
	IxScreen      = "screen"
	IxElicitation = "elicitation"
)

// AnsweredByAssistant marks a resolution the assistant gave for the user (Resolution.AnsweredBy).
const AnsweredByAssistant = "assistant"

type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type Question struct {
	Question    string           `json:"question"`
	Header      string           `json:"header,omitempty"`
	MultiSelect bool             `json:"multi_select"`
	Options     []QuestionOption `json:"options"`
}

type Interaction struct {
	ID              string          `json:"id"`
	MachineID       string          `json:"machine_id"`
	SessionID       string          `json:"session_id"`
	SessionTitle    string          `json:"session_title,omitempty"`
	TaskTitle       string          `json:"task_title,omitempty"` // the task its agent works for (docs/unified-entry.md), set when sent to clients
	Cwd             string          `json:"cwd,omitempty"`
	Kind            string          `json:"kind"`
	CreatedAt       time.Time       `json:"created_at"`
	ExpiresAt       time.Time       `json:"expires_at"`
	ToolName        string          `json:"tool_name,omitempty"`
	ToolUseID       string          `json:"tool_use_id,omitempty"`
	Questions       []Question      `json:"questions,omitempty"`
	Plan            string          `json:"plan,omitempty"`
	ToolInput       json.RawMessage `json:"tool_input,omitempty"`
	Summary         string          `json:"summary,omitempty"`
	Screen          string          `json:"screen,omitempty"`
	AgentID         string          `json:"agent_id,omitempty"`
	AgentType       string          `json:"agent_type,omitempty"`
	MCPServerName   string          `json:"mcp_server_name,omitempty"`
	MCPConfigSource string          `json:"mcp_config_source,omitempty"`
	Elicitation     *Elicitation    `json:"elicitation,omitempty"`
	// Dialog is the screen's dialog read on the hub (screen interactions); answers name an option and are checked
	// against Dialog.Rev before any key is pressed. Absent when the screen shows no dialog it could read.
	Dialog *screen.Dialog `json:"dialog,omitempty"`
	// Secret: the screen asks for a password. The app takes it in a password field and keeps no copy; the hub never
	// stores what is typed into a terminal card anyway.
	Secret bool `json:"secret,omitempty"`
	// Fields are filled in on the phone before approving (a permission card that needs a password): the values come
	// back in Resolution.Values and never pass through the model. Quick approve (notification buttons, preview
	// cards, the watch, Siri) is off for such cards: they open the form.
	Fields []Field `json:"fields,omitempty"`
	// Resumable: a hook waits on it that reconnects when its node goes away (hub or node restart), so the hub keeps
	// it pending through the gap instead of cancelling it (docs/protocol.md「交互挺过重启」).
	Resumable bool `json:"resumable,omitempty"`
	// Agent is who asks when it is not Claude Code: "codex" or "traex" (a card the node read off a Codex TUI,
	// docs/codex.md「提问和审批」).
	Agent string `json:"agent,omitempty"`
	// Reason: why the agent asks for this permission, in its words (Codex's "Reason: …").
	Reason string `json:"reason,omitempty"`
	// AlwaysLabel names what "always allow" does for this request ("本会话不再问这个命令"); a permission card offers it
	// only when set or for Claude.
	AlwaysLabel string `json:"always_label,omitempty"`
	// AgentLabel is what to call Agent when the apps do not know it (a runtime's own name, "DSH"; docs/runtimes.md),
	// as proto.Session.AgentLabel.
	AgentLabel string `json:"agent_label,omitempty"`
}

// Field is one input of a card (Interaction.Fields).
type Field struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Placeholder string `json:"placeholder,omitempty"`
	Secret      bool   `json:"secret,omitempty"` // a password: a secure field, redacted in the history
	Optional    bool   `json:"optional,omitempty"`
}

// Elicitation carries the hook's original schema or URL without interpreting its fields.
type Elicitation struct {
	Mode            string          `json:"mode"`
	Message         string          `json:"message"`
	RequestedSchema json.RawMessage `json:"requested_schema,omitempty"`
	URL             string          `json:"url,omitempty"`
	ID              string          `json:"elicitation_id,omitempty"`
}

type Resolution struct {
	Action   string            `json:"action,omitempty"`
	Content  map[string]any    `json:"content,omitempty"`
	Answers  map[string]string `json:"answers,omitempty"`
	Approve  *bool             `json:"approve,omitempty"`
	Mode     string            `json:"mode,omitempty"`
	Feedback string            `json:"feedback,omitempty"`
	Allow    *bool             `json:"allow,omitempty"`
	Always   bool              `json:"always,omitempty"`
	Message  string            `json:"message,omitempty"`
	Keys     []string          `json:"keys,omitempty"`
	Text     string            `json:"text,omitempty"`
	// AnsweredBy is who answered when it was not the user in the app: AnsweredByAssistant for a question the
	// assistant answered on their behalf (docs/assistant.md). Absent when the app answered.
	AnsweredBy string `json:"answered_by,omitempty"`
	// PreKeys go in before Text (Keys after it): pick "Type something." by its number, type the answer, enter.
	PreKeys []string `json:"pre_keys,omitempty"`
	Dismiss bool     `json:"dismiss,omitempty"`
	// Option answers a screen dialog by its option's key (Interaction.Dialog): the keys are worked out where they are
	// pressed, from the screen as it is then (Text goes with a type_something option).
	Option string `json:"option,omitempty"`
	// Options answer a multi-select screen dialog (Dialog.Multi): the keys of the options to have checked when it is
	// sent, the rest unchecked; Text is its free-text field (empty: left unchecked).
	Options []string `json:"options,omitempty"`
	// Values are the card's Fields as filled in on the phone, by key.
	Values map[string]string `json:"values,omitempty"`
}

// Tool is an MCP tool definition.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// Content is an MCP tool result content block.
type Content struct {
	Type     string `json:"type"` // text | image
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
}

type ToolResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"is_error"`
}

func TextResult(text string, isErr bool) ToolResult {
	return ToolResult{Content: []Content{{Type: "text", Text: text}}, IsError: isErr}
}

// BackgroundTask is a Bash command or subagent Claude runs in the background (run_in_background).
type BackgroundTask struct {
	ID          string    `json:"id"`   // task id from the tool result (e.g. "b85polnoj")
	Kind        string    `json:"kind"` // "shell" | "agent"
	Description string    `json:"description,omitempty"`
	Command     string    `json:"command,omitempty"`
	OutputFile  string    `json:"output_file,omitempty"`
	StartedAt   time.Time `json:"started_at"`
}

// LoopInfo describes a /loop: Claude re-runs Prompt on its own, either by scheduling its next wakeup each tick
// (ScheduleWakeup, kind "wakeup") or with a cron job (CronCreate, kind "cron").
type LoopInfo struct {
	Kind            string    `json:"kind"`
	Prompt          string    `json:"prompt,omitempty"`
	Reason          string    `json:"reason,omitempty"`
	IntervalSeconds int       `json:"interval_seconds,omitempty"`
	NextAt          time.Time `json:"next_at,omitempty"`
	Cron            string    `json:"cron,omitempty"`
	Ticks           int       `json:"ticks,omitempty"` // wakeups scheduled so far
}

// Subagent is an agent the session launched (the Agent tool), with its own transcript.
type Subagent struct {
	ID          string    `json:"id"`         // agent id (agent-<id>.jsonl)
	SessionID   string    `json:"session_id"` // "<parent>@<id>": readable like any session
	Type        string    `json:"type,omitempty"`
	Description string    `json:"description,omitempty"`
	ToolUseID   string    `json:"tool_use_id,omitempty"`
	ParentID    string    `json:"parent_id,omitempty"` // the launching subagent's id; empty: the session (an unlisted one: show at the top)
	Fork        bool      `json:"fork,omitempty"`      // a fork of its launcher's conversation (agentType "fork")
	Running     bool      `json:"running"`
	Background  bool      `json:"background,omitempty"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
	LastText    string    `json:"last_text,omitempty"`
}

// UsageBucket is one minute of one model's token usage on a machine, summed over every transcript there (node
// usage.go). A node reports a bucket's whole total each time it changes, so reporting it again replaces it.
type UsageBucket struct {
	Minute     int64  `json:"minute"` // Unix seconds of the minute's start
	Model      string `json:"model"`
	Input      int64  `json:"in,omitempty"` // not from the cache
	CacheWrite int64  `json:"cache_write,omitempty"`
	CacheRead  int64  `json:"cache_read,omitempty"`
	Output     int64  `json:"out,omitempty"`
	Requests   int    `json:"req,omitempty"` // model replies
}

// LarkRule is one rule of the assistant's Feishu write gate (node/larkgate.go): an lark-connector agent API call matching
// Method ("*" for any), Path (segments; "*" one segment, "**" the rest) and, when set, the top-level fields of its
// JSON body, gets Action: "allow" (run it), "confirm" (the user approves it on the phone first) or "deny".
// hub.json's lark_gate rules come first, then the built-in ones; the first match decides.
type LarkRule struct {
	Method string         `json:"method"`
	Path   string         `json:"path"`
	Body   map[string]any `json:"body,omitempty"`
	Action string         `json:"action"`
}

// AssistantLark is what the assistant process needs to drive lark-connector itself (the lark-connector skill's ol.sh): the
// agent API's base (without /agent/v1), its token, and the write gate's extra rules. Sent by the hub to the assistant's
// node only; never written to disk or logs.
type AssistantLark struct {
	Account int        `json:"account,omitempty"`
	Base    string     `json:"base"`
	Token   string     `json:"token"`
	Rules   []LarkRule `json:"rules,omitempty"`
	CLI     *LarkCLI   `json:"cli,omitempty"`    // lark-cli is configured (hub.json lark.cli, docs/plan-lark-cli.md)
	Prefer  string     `json:"prefer,omitempty"` // "lark-connector" | "cli": the primary source when both are there
	// Gates are the command gates of the assistant's skills (plugins' assistant.gate, docs/extensions.md「助手」):
	// a command calling an HTTP API as the user, its credentials and the rules its calls go by.
	Gates []CommandGate `json:"gates,omitempty"`
}

// CommandGate is one skill command that calls an HTTP API as the user (`<command> METHOD PATH [BODY] [curl flags]`),
// as the assistant's machine gates it (node/larkgate.go): the command on the assistant's PATH runs the skill's
// script with Env (the credentials, only in the assistant's process); every Bash call of it is read, and its calls
// go by Rules (the first match; none matching is confirmed on the phone).
type CommandGate struct {
	Plugin  string `json:"plugin,omitempty"`
	Command string `json:"command"` // the name on the assistant's PATH
	Script  string `json:"script"`  // the script it runs, relative to the skills directory ("<skill>/scripts/x.sh")
	Title   string `json:"title"`   // what the phone card says it acts on ("在<Title>上…")
	// Env is put into the assistant's process; BaseEnv names the one holding the API's base URL (curl / wget at its
	// host is refused); Secret are names the assistant may never mention (the token); AllowedEnv may be set before
	// the command (VAR=x cmd …).
	Env        map[string]string `json:"env,omitempty"`
	BaseEnv    string            `json:"base_env,omitempty"`
	Secret     []string          `json:"secret,omitempty"`
	AllowedEnv []string          `json:"allowed_env,omitempty"`
	Headers    []string          `json:"headers,omitempty"`     // -H headers the command may be given (lowercase)
	PathPrefix string            `json:"path_prefix,omitempty"` // dropped from a call's path ("/agent/v1")
	Rules      []LarkRule        `json:"rules,omitempty"`
	// Actions name what a call does on the phone card: the first whose pattern (a regexp over "METHOD /path")
	// matches.
	Actions []GateAction `json:"actions,omitempty"`
	// Tool is the command as a tool for an assistant on a runtime without Bash ({args: [words after the command]}).
	Tool *GateTool `json:"tool,omitempty"`
}

// GateAction names a kind of call for the phone card.
type GateAction struct {
	Pattern string `json:"pattern"`
	What    string `json:"what"`
}

// GateTool is a command gate's tool for runtimes.
type GateTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// LarkCLI is how the assistant's machine runs lark-cli.
type LarkCLI struct {
	Bin     string `json:"bin,omitempty"`
	Profile string `json:"profile,omitempty"`
}
