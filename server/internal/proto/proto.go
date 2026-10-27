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
}

type NodeInfo struct {
	Hostname      string `json:"hostname"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	Home          string `json:"home"`
	HerdrVersion  string `json:"herdr_version"`
	ClaudeVersion string `json:"claude_version"`
	Version       string `json:"version"`
}

type Launcher struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
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
	Agent           string `json:"agent,omitempty"`
	Launcher        string `json:"launcher,omitempty"`
	Model           string `json:"model,omitempty"`
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
)

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
		lazy := &Image{Mime: it.Image.Mime, Lazy: true, Size: len(it.Image.Data) * 3 / 4}
		// Only the header is read: the decoder stops once it knows the size.
		if c, _, err := image.DecodeConfig(base64.NewDecoder(base64.StdEncoding, strings.NewReader(it.Image.Data))); err == nil {
			lazy.Width, lazy.Height = c.Width, c.Height
		}
		out[i].Image = lazy
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
	// Wake marks a background task's notice that started new work after the agent had finished its turn
	// (not one absorbed mid-turn): the exchange before it is over and its last words are an answer.
	Wake bool `json:"wake,omitempty"`
	// From names the Claude session that sent this message (cross-session messaging), not the user.
	From string `json:"from,omitempty"`
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
	Dismiss  bool              `json:"dismiss,omitempty"`
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
