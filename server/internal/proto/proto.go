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

	"github.com/chyroc/jarvis/server/internal/screen"
)

type Machine struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Kind          string    `json:"kind"` // local | ssh
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
	// System: OS and hardware, from the node's last connection (kept across hub restarts).
	System *System `json:"system,omitempty"`
}

// Features are what this hub supports beyond the first protocol, listed in the WebSocket hello and /health for
// clients that need to know before they try (docs/protocol.md).
var Features = []string{
	"client_msg_id",          // session.send dedupes retries
	"send_refusals",          // 409 dialog_open / input_draft, clear_draft
	"screen_dialog",          // interaction.dialog, Resolution.option, 409 prompt_changed
	"resumable_interactions", // hook questions survive hub / node restarts
	"secret_prompts",         // interaction.secret
	"request_id",             // X-Request-Id
	"terminal_stream",        // WebSocket terminal_subscribe: the pane's live frames (docs/terminal.md)
	"terminal_control",       // terminal_subscribe with control: the phone drives the terminal at its size; terminal_input
	"terminal_scroll",        // terminal_input with scroll: the wheel over a terminal the app drives, to herdr (docs/terminal.md)
	"terminal_click",         // terminal_input with click: a tap on a terminal the app drives, a left click there (docs/terminal.md)
	"file_upload",            // POST /api/v1/machines/{mid}/files (docs/protocol.md「发送文件」)
	"plugins",                // GET /api/v1/plugins and the relay under /api/v1/plugins/{id}/ (docs/plugins.md)
}

// Files sent from the phone (POST /api/v1/machines/{mid}/files): the hub streams the body to the node in chunks.
const (
	MaxUploadBytes   = 200 << 20
	UploadChunkBytes = 1 << 20
	// CodeTooLarge: over MaxUploadBytes (413); CodeNodeOutdated: the machine's node predates the feature (409).
	CodeTooLarge     = "too_large"
	CodeNodeOutdated = "node_outdated"
)

// UploadedFile is where an upload landed on the machine.
type UploadedFile struct {
	Path string `json:"path"` // absolute
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type NodeInfo struct {
	Hostname      string  `json:"hostname"`
	OS            string  `json:"os"`
	Arch          string  `json:"arch"`
	Home          string  `json:"home"`
	HerdrVersion  string  `json:"herdr_version"`
	ClaudeVersion string  `json:"claude_version"`
	Version       string  `json:"version"`
	System        *System `json:"system,omitempty"`
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
	Cores     []float64 `json:"cores,omitempty"` // busy % per core (Linux; the latest sample only, not kept)
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

type LauncherEnv struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ManagedLauncher is a launcher kept by the hub (设置 › 模型): what `jarvisd launch <name>` sets before it runs claude.
// The key only goes to nodes (in memory), never to the app.
type ManagedLauncher struct {
	Name       string `json:"name"`
	BaseURL    string `json:"base_url"`
	Model      string `json:"model"`
	SmallModel string `json:"small_model,omitempty"`
	// Args go before the caller's (e.g. --dangerously-skip-permissions).
	Args   []string `json:"args,omitempty"`
	APIKey string   `json:"api_key,omitempty"`
}

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
	MachineID   string `json:"machine_id"`
	SessionID   string `json:"session_id"`
	Title       string `json:"title"`
	Cwd         string `json:"cwd"`
	Status      string `json:"status"`
	PaneID      string `json:"pane_id,omitempty"`
	TabID       string `json:"tab_id,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	AgentName   string `json:"agent_name,omitempty"`
	// Agent is the coding agent in the pane: "claude" (also when empty, from older nodes) or "codex".
	Agent    string `json:"agent,omitempty"`
	Launcher string `json:"launcher,omitempty"`
	Model    string `json:"model,omitempty"`
	// PermissionMode and Effort are what Claude Code runs with now: default / acceptEdits / plan / auto /
	// bypassPermissions, and low … max. Empty while unknown (before the first turn, Codex).
	PermissionMode  string `json:"permission_mode,omitempty"`
	Effort          string `json:"effort,omitempty"`
	StartedByJarvis bool   `json:"started_by_jarvis"`
	Assistant       bool   `json:"assistant"` // runs in the assistant directory (the app's 对话 tab)
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
	Loop      *LoopInfo `json:"loop,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	LastText  string    `json:"last_text,omitempty"`
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
	Items      int       `json:"items"`
	Tools      int       `json:"tools"`
	Thoughts   int       `json:"thoughts"` // thoughts with words (blank ones are not shown)
	Until      time.Time `json:"until"`
	ThroughSeq int       `json:"through_seq"`
	ThroughID  string    `json:"through_id"`
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
	Task *TaskNote `json:"task,omitempty"`
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
	IxQuestion   = "question"
	IxPlan       = "plan"
	IxPermission = "permission"
	IxScreen     = "screen"
)

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
	ID           string          `json:"id"`
	MachineID    string          `json:"machine_id"`
	SessionID    string          `json:"session_id"`
	SessionTitle string          `json:"session_title,omitempty"`
	Cwd          string          `json:"cwd,omitempty"`
	Kind         string          `json:"kind"`
	CreatedAt    time.Time       `json:"created_at"`
	ExpiresAt    time.Time       `json:"expires_at"`
	ToolName     string          `json:"tool_name,omitempty"`
	ToolUseID    string          `json:"tool_use_id,omitempty"`
	Questions    []Question      `json:"questions,omitempty"`
	Plan         string          `json:"plan,omitempty"`
	ToolInput    json.RawMessage `json:"tool_input,omitempty"`
	Summary      string          `json:"summary,omitempty"`
	Screen       string          `json:"screen,omitempty"`
	// Dialog is the screen's dialog read on the hub (screen interactions); answers name an option and are checked
	// against Dialog.Rev before any key is pressed. Absent when the screen shows no dialog it could read.
	Dialog *screen.Dialog `json:"dialog,omitempty"`
	// Secret: the screen asks for a password. The app takes it in a password field and keeps no copy; the hub never
	// stores what is typed into a terminal card anyway.
	Secret bool `json:"secret,omitempty"`
	// Resumable: a hook waits on it that reconnects when its node goes away (hub or node restart), so the hub keeps
	// it pending through the gap instead of cancelling it (docs/protocol.md「交互挺过重启」).
	Resumable bool `json:"resumable,omitempty"`
}

type Resolution struct {
	Answers  map[string]string `json:"answers,omitempty"`
	Approve  *bool             `json:"approve,omitempty"`
	Mode     string            `json:"mode,omitempty"`
	Feedback string            `json:"feedback,omitempty"`
	Allow    *bool             `json:"allow,omitempty"`
	Always   bool              `json:"always,omitempty"`
	Message  string            `json:"message,omitempty"`
	Keys     []string          `json:"keys,omitempty"`
	Text     string            `json:"text,omitempty"`
	// PreKeys go in before Text (Keys after it): pick "Type something." by its number, type the answer, enter.
	PreKeys []string `json:"pre_keys,omitempty"`
	Dismiss bool     `json:"dismiss,omitempty"`
	// Option answers a screen dialog by its option's key (Interaction.Dialog): the keys are worked out where they are
	// pressed, from the screen as it is then (Text goes with a type_something option).
	Option string `json:"option,omitempty"`
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
}
