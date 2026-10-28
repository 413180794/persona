// Package transcript turns Claude Code session JSONL files into normalized proto.Items.
package transcript

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/proto"
)

// Limits keep huge tool outputs and images from flooding the phone.
const (
	maxTextLen  = 24 * 1024
	maxImageLen = 1536 * 1024
)

type entry struct {
	Type             string          `json:"type"`
	Subtype          string          `json:"subtype"`
	UUID             string          `json:"uuid"`
	Timestamp        time.Time       `json:"timestamp"`
	IsSidechain      bool            `json:"isSidechain"`
	IsMeta           bool            `json:"isMeta"`
	IsCompactSummary bool            `json:"isCompactSummary"`
	Content          json.RawMessage `json:"content"`
	Message          *struct {
		Role    string          `json:"role"`
		Model   string          `json:"model"`
		ID      string          `json:"id"`
		Content json.RawMessage `json:"content"`
		Usage   *struct {
			InputTokens              int `json:"input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			OutputTokens             int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
	AITitle        string `json:"aiTitle"`
	CustomTitle    string `json:"customTitle"`
	GitBranch      string `json:"gitBranch"`
	Version        string `json:"version"`
	PermissionMode string `json:"permissionMode"`
	Effort         string `json:"effort"` // assistant lines: the effort level the reply was generated with
	Operation      string `json:"operation"`
	Attachment     *struct {
		Type        string          `json:"type"`
		Prompt      json.RawMessage `json:"prompt"`
		CommandMode string          `json:"commandMode"`
		Origin      *struct {
			Kind string `json:"kind"`
			Name string `json:"name"` // kind "peer": the Claude session that sent it
			Body string `json:"body"`
		} `json:"origin"`
	} `json:"attachment"`
	Cwd           string          `json:"cwd"`
	ToolUseResult json.RawMessage `json:"toolUseResult"`
	// system/api_error: {message, formatted} (raw: other lines may carry a plain string here), and which retry this is.
	Error        json.RawMessage `json:"error"`
	RetryAttempt int             `json:"retryAttempt"`
	MaxRetries   int             `json:"maxRetries"`
	// system/scheduled_task_fire: the /loop prompt that is about to run.
	Prompt string `json:"prompt"`
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
	Source    *struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	} `json:"source"`
}

// Meta is session-level information collected while parsing.
type Meta struct {
	// Developer info for the chat header (all from fields Claude Code writes on each line).
	GitBranch      string
	Version        string
	PermissionMode string
	// ModeAt / Effort / EffortAt: when the permission mode was last seen, and the effort level of the latest reply.
	ModeAt        time.Time
	Effort        string
	EffortAt      time.Time
	ContextTokens int // input + cache read + cache creation of the latest reply: what the model last saw
	OutputTokens  int // total generated, counted once per message
	Compactions   int
	Turns         int // user prompts
	StartedAt     time.Time
	lastMessageID string
	// Queue holds input typed while Claude works and not yet taken (queue-operation lines:
	// enqueue / dequeue / remove / popAll — popAll is "up arrow pulls them back into the input").
	Queue []string
	// Background holds background shells / agents that have not reported back (see trackBackground).
	Background []proto.BackgroundTask
	bgPending  map[string]proto.BackgroundTask // tool_use_id → started, waiting for the tool result with its id
	// Loop is the /loop this session runs, if any (see trackBackground).
	Loop        *proto.LoopInfo
	loopPending map[string]proto.LoopInfo
	// LastActivity is the time of the last real exchange (user input, assistant output, tool calls). Claude Code
	// also appends recaps, titles and mode lines to idle sessions, so the file mtime is not "last used".
	LastActivity time.Time
	FirstUser    string // first thing the user typed; titles untitled (headless) sessions
	Title        string
	customTitle  bool // Title came from /rename; later ai-titles do not replace it
	// Sidechain keeps isSidechain lines: a subagent's own transcript is all sidechain.
	Sidechain bool
	// Codex: the file is a Codex rollout (codex.go), not a Claude transcript.
	Codex bool
	// TurnOpen: a rollout's latest turn has started and not finished (task_started without task_complete).
	TurnOpen bool
	// codexSeen: newer rollouts record an item both as event_msg/item_completed and in history_mutation.
	codexSeen map[string]bool
	// LastTurnWake: the latest turn was started by a background task reporting back, not by a message (no
	// "finished" push for it).
	LastTurnWake bool
	// bashInput is the last "! cmd" typed in Claude Code, for the Bash call its output line turns into.
	bashInput string
	// notified holds the background-task notifications already shown: one is often recorded twice (as a user line and
	// as a queued_command attachment). The same task reporting again with other content is a new event and shows.
	notified map[string]bool
	Cwd       string
	Model     string
	LastText  string
	Updated   time.Time
}

// pastedRe matches the wrapper Claude Code puts around long pasted input (also what herdr's bracketed paste from
// the phone, e.g. a WeChat voice-input transcript, turns into): <pasted_content id="ae9b">…</pasted_content id="ae9b">.
var pastedRe = regexp.MustCompile(`<[\\/]*pasted_content(?:\s+id="[^"]*")?\s*>`) // also <\pasted…> / <\/pasted…> from nested pastes

var tagRe = regexp.MustCompile(`(?s)<(command-name|command-message|command-args|local-command-stdout|local-command-stderr|task-notification|system-reminder)>(.*?)</(command-name|command-message|command-args|local-command-stdout|local-command-stderr|task-notification|system-reminder)>`)

// ParseLine converts one JSONL line (1-based seq) into items, updating meta.
func ParseLine(seq int, line []byte, meta *Meta) []proto.Item {
	var items []proto.Item
	if meta.Codex || isCodexLine(line) {
		meta.Codex = true
		items = parseCodexLine(seq, line, meta)
	} else {
		items = parseLine(seq, line, meta)
	}
	if len(items) > 0 && items[0].Role == "user" && items[0].Kind == proto.KindText {
		meta.Turns++
	}
	for _, it := range items {
		if it.Role != "system" && !it.TS.IsZero() && it.TS.After(meta.LastActivity) {
			meta.LastActivity = it.TS
		}
		switch {
		case it.Wake:
			meta.LastTurnWake = true
		case it.Role == "user" && it.Kind == proto.KindText && !it.Queued:
			meta.LastTurnWake = false
		}
	}
	return items
}

func parseLine(seq int, line []byte, meta *Meta) []proto.Item {
	var e entry
	if err := json.Unmarshal(line, &e); err != nil {
		return nil
	}
	if e.Cwd != "" {
		meta.Cwd = e.Cwd
	}
	if !e.Timestamp.IsZero() {
		meta.Updated = e.Timestamp
	}
	if e.GitBranch != "" {
		meta.GitBranch = e.GitBranch
	}
	if e.Version != "" {
		meta.Version = e.Version
	}
	if meta.StartedAt.IsZero() && !e.Timestamp.IsZero() {
		meta.StartedAt = e.Timestamp
	}
	// The mode is written on every prompt (user lines) and as its own line when it changes; the effort on replies.
	if e.PermissionMode != "" && !e.IsSidechain && (e.Type == "permission-mode" || e.Type == "user") {
		meta.PermissionMode, meta.ModeAt = e.PermissionMode, meta.Updated
	}
	if e.Effort != "" && e.Type == "assistant" && !e.IsSidechain {
		meta.Effort, meta.EffortAt = e.Effort, meta.Updated
	}
	switch {
	case e.Type == "system" && e.Subtype == "compact_boundary":
		meta.Compactions++
	case e.Type == "assistant" && e.Message != nil && e.Message.Usage != nil && !e.IsSidechain:
		u := e.Message.Usage
		meta.ContextTokens = u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
		if e.Message.ID != meta.lastMessageID { // every content block repeats the message's usage
			meta.OutputTokens += u.OutputTokens
			meta.lastMessageID = e.Message.ID
		}
	}
	if e.Type == "queue-operation" {
		content := rawString(e.Content)
		if strings.Contains(content, "<task-notification>") {
			// A background task reporting back is queued like input, but it is not something the user typed.
			meta.taskFinished(content)
			return nil
		}
		meta.applyQueue(e.Operation, content)
		return nil
	}
	if e.Type == "custom-title" && e.CustomTitle != "" { // /rename: the user's name beats Claude's generated one
		meta.Title, meta.customTitle = e.CustomTitle, true
		return nil
	}
	if e.Type == "ai-title" && e.AITitle != "" {
		if !meta.customTitle {
			meta.Title = e.AITitle
		}
		return nil
	}
	if e.IsSidechain && !meta.Sidechain {
		return nil
	}
	mk := func(idx int, role, kind string) proto.Item {
		id := e.UUID
		if id == "" {
			id = fmt.Sprintf("line%d", seq)
		}
		return proto.Item{ID: fmt.Sprintf("%s#%d", id, idx), Seq: seq, TS: e.Timestamp, Role: role, Kind: kind}
	}
	switch e.Type {
	case "attachment":
		// Input typed while Claude was working is queued, then absorbed into the running turn; it is stored as a
		// queued_command attachment instead of a user message, so without this it never shows up in the chat.
		a := e.Attachment
		if a == nil || a.Type != "queued_command" {
			return nil
		}
		notification := a.CommandMode == "task-notification"
		if a.Origin != nil && a.Origin.Kind == "peer" { // another Claude session's message, absorbed mid-turn
			var prompt string
			_ = json.Unmarshal(a.Prompt, &prompt)
			from, body, ok := CrossSession(prompt)
			if !ok {
				from, body = a.Origin.Name, strings.TrimSpace(a.Origin.Body)
			}
			if a.Origin.Name != "" {
				from = a.Origin.Name
			}
			if body == "" {
				return nil
			}
			it := mk(0, "user", proto.KindText)
			it.Text, it.From, it.Queued = clip(body), from, true
			return []proto.Item{it}
		}
		if !notification && ((a.CommandMode != "" && a.CommandMode != "prompt") || (a.Origin != nil && a.Origin.Kind != "human")) {
			return nil
		}
		var text string
		if json.Unmarshal(a.Prompt, &text) != nil {
			var blocks []block
			if json.Unmarshal(a.Prompt, &blocks) == nil {
				var parts []string
				for _, b := range blocks {
					if b.Type == "text" {
						parts = append(parts, b.Text)
					}
				}
				text = strings.Join(parts, "\n")
			}
		}
		if strings.Contains(text, "<task-notification>") {
			meta.taskFinished(text)
			return userText(mk, text, meta) // a "background task finished" notice
		}
		if notification {
			return nil // other harness notifications are not user input
		}
		items := userText(mk, text, meta)
		for i := range items {
			items[i].Queued = true
		}
		if meta.FirstUser == "" && len(items) > 0 && items[0].Kind == proto.KindText {
			meta.FirstUser = items[0].Text
		}
		return items
	case "system":
		var text string
		switch e.Subtype {
		case "compact_boundary":
			text = "上下文已压缩"
		case "api_error":
			// Claude Code writes one line per retry (retryAttempt 1..maxRetries): show the first and, if it never
			// got through, the last.
			if e.RetryAttempt > 1 && (e.MaxRetries == 0 || e.RetryAttempt < e.MaxRetries) {
				return nil
			}
			text = "API 错误：" + apiErrorText(e)
			switch {
			case e.MaxRetries > 0 && e.RetryAttempt >= e.MaxRetries:
				text += fmt.Sprintf("（重试 %d 次后放弃）", e.RetryAttempt)
			case e.RetryAttempt == 1:
				text += "（正在重试）"
			}
		case "local_command":
			// Newer Claude Code writes slash commands and their output as system lines (older ones as user lines):
			// "<command-name>/model</command-name>…" is what the user typed, "<local-command-stdout>…" the answer.
			return userText(mk, rawString(e.Content), meta)
		case "scheduled_task_fire":
			// A /loop or cron job firing: the turn that follows was started by the schedule, not by a message.
			text = "/loop 到点，开始新一轮"
			if p := snippetLine(e.Prompt, 80); p != "" {
				text += "：" + p
			}
			it := mk(0, "system", proto.KindNotice)
			it.Text, it.Wake = text, true
			return []proto.Item{it}
		case "away_summary":
			recap := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rawString(e.Content)), "(disable recaps in /config)"))
			if recap == "" {
				return nil
			}
			it := mk(0, "system", proto.KindRecap)
			it.Text = clip(recap)
			return []proto.Item{it}
		case "informational":
			text = rawString(e.Content)
		default:
			return nil
		}
		if strings.TrimSpace(text) == "" {
			return nil
		}
		it := mk(0, "system", proto.KindNotice)
		it.Text = clip(text)
		return []proto.Item{it}
	case "user":
		if e.Message == nil {
			return nil
		}
		if e.IsCompactSummary {
			it := mk(0, "system", proto.KindNotice)
			it.Text = "已从压缩的上下文继续"
			return []proto.Item{it}
		}
		var s string
		if json.Unmarshal(e.Message.Content, &s) == nil {
			if strings.Contains(s, "<task-notification>") {
				meta.taskFinished(s)
			}
			if e.IsMeta {
				return nil
			}
			items := userText(mk, s, meta)
			if strings.Contains(s, "<task-notification>") {
				markWake(items)
			}
			if meta.FirstUser == "" && len(items) > 0 && items[0].Kind == proto.KindText {
				meta.FirstUser = items[0].Text
			}
			return items
		}
		var blocks []block
		if json.Unmarshal(e.Message.Content, &blocks) != nil {
			return nil
		}
		var out []proto.Item
		for i, b := range blocks {
			switch b.Type {
			case "text":
				if strings.Contains(b.Text, "<task-notification>") {
					meta.taskFinished(b.Text)
				}
				if e.IsMeta {
					continue
				}
				items := userText(func(_ int, role, kind string) proto.Item { return mk(i, role, kind) }, b.Text, meta)
				if strings.Contains(b.Text, "<task-notification>") {
					markWake(items)
				}
				if meta.FirstUser == "" && len(items) > 0 && items[0].Kind == proto.KindText {
					meta.FirstUser = items[0].Text
				}
				out = append(out, items...)
			case "image":
				if e.IsMeta {
					continue
				}
				if img := imageOf(b); img != nil {
					it := mk(i, "user", proto.KindImage)
					it.Image = img
					out = append(out, it)
				}
			case "tool_result":
				text, imgs := toolResultContent(b.Content)
				meta.backgroundStarted(b.ToolUseID, text, b.IsError, e.Timestamp)
				it := mk(i, "user", proto.KindToolResult)
				it.ToolUseID = b.ToolUseID
				it.IsError = b.IsError
				it.Text = clip(text)
				out = append(out, it)
				for j, img := range imgs {
					im := mk(i*100+j+1, "user", proto.KindImage)
					im.ToolUseID = b.ToolUseID
					im.Image = img
					out = append(out, im)
				}
			}
		}
		return out
	case "assistant":
		if e.Message == nil {
			return nil
		}
		if e.Message.Model != "" && e.Message.Model != "<synthetic>" {
			meta.Model = e.Message.Model
		}
		var blocks []block
		if json.Unmarshal(e.Message.Content, &blocks) != nil {
			return nil
		}
		var out []proto.Item
		for i, b := range blocks {
			switch b.Type {
			case "text":
				if strings.TrimSpace(b.Text) == "" {
					continue
				}
				it := mk(i, "assistant", proto.KindText)
				it.Text = clip(b.Text)
				meta.LastText = b.Text
				out = append(out, it)
			case "thinking":
				if strings.TrimSpace(b.Thinking) == "" {
					continue
				}
				it := mk(i, "assistant", proto.KindThinking)
				it.Text = clip(b.Thinking)
				out = append(out, it)
			case "tool_use", "server_tool_use":
				it := mk(i, "assistant", proto.KindToolUse)
				it.ToolUseID = b.ID
				it.ToolName = b.Name
				it.ToolInput = clipJSON(b.Input)
				out = append(out, it)
				meta.trackBackground(b.ID, b.Name, b.Input, e.Timestamp)
			}
		}
		return out
	}
	return nil
}

// Background tasks: Bash with run_in_background and subagents launched async answer with a task id right away
// ("Command running in background with ID: b85polnoj. Output is being written to: …" / "Async agent launched …
// agentId: …") and report back later with a <task-notification> carrying the same id. TaskStop / KillShell end them too.
var (
	bgIDRe     = regexp.MustCompile(`(?:running in background with ID|agentId):\s*([A-Za-z0-9_-]+)`)
	wakeInRe   = regexp.MustCompile(`Next wakeup scheduled for [^(]*\(in (\d+)s\)`)
	bgMovedRe  = regexp.MustCompile(`moved to the background \(ID: ([A-Za-z0-9_-]+)\)`) // a foreground command that timed out
	bgOutputRe = regexp.MustCompile(`Output is being written to:\s*(\S+?)\.?(?:\s|$)`)
)

func (m *Meta) trackBackground(toolUseID, name string, raw json.RawMessage, ts time.Time) {
	var in struct {
		Command     string `json:"command"`
		Description string `json:"description"`
		Background  bool   `json:"run_in_background"`
		TaskID      string `json:"task_id"`
		ShellID     string `json:"shell_id"`
		// ScheduleWakeup / CronCreate (the /loop skill)
		Delay     float64 `json:"delaySeconds"`
		Prompt    string  `json:"prompt"`
		Reason    string  `json:"reason"`
		Stop      bool    `json:"stop"`
		Cron      string  `json:"cron"`
		Recurring *bool   `json:"recurring"`
	}
	_ = json.Unmarshal(raw, &in)
	switch name {
	case "ScheduleWakeup":
		if in.Stop {
			m.Loop = nil
			return
		}
		if m.loopPending == nil {
			m.loopPending = map[string]proto.LoopInfo{}
		}
		m.loopPending[toolUseID] = proto.LoopInfo{Kind: "wakeup", Prompt: in.Prompt, Reason: in.Reason,
			IntervalSeconds: int(in.Delay), NextAt: ts.Add(time.Duration(in.Delay) * time.Second)}
	case "CronCreate":
		if (in.Recurring != nil && !*in.Recurring) || onceCron(in.Cron) {
			return // a one-off reminder, not a loop
		}
		m.Loop = &proto.LoopInfo{Kind: "cron", Prompt: in.Prompt, Cron: in.Cron}
	case "CronDelete":
		if m.Loop != nil && m.Loop.Kind == "cron" {
			m.Loop = nil
		}
	case "Bash":
		if !in.Background {
			return
		}
		m.pend(toolUseID, proto.BackgroundTask{Kind: "shell", Command: in.Command, Description: in.Description, StartedAt: ts})
	case "Agent", "Task":
		// Whether it went to the background shows in the result ("Async agent launched").
		m.pend(toolUseID, proto.BackgroundTask{Kind: "agent", Description: in.Description, StartedAt: ts})
	case "TaskStop", "KillShell", "KillBash":
		m.removeBackground(in.TaskID + in.ShellID)
	}
}

// onceCron reports a cron expression pinned to one date ("33 17 21 10 *": day of month and month both fixed).
func onceCron(expr string) bool {
	f := strings.Fields(expr)
	if len(f) < 5 {
		return false
	}
	_, errDay := strconv.Atoi(f[2])
	_, errMonth := strconv.Atoi(f[3])
	return errDay == nil && errMonth == nil
}

func (m *Meta) pend(toolUseID string, t proto.BackgroundTask) {
	if m.bgPending == nil {
		m.bgPending = map[string]proto.BackgroundTask{}
	}
	m.bgPending[toolUseID] = t
}

func (m *Meta) backgroundStarted(toolUseID, result string, isError bool, ts time.Time) {
	if l, ok := m.loopPending[toolUseID]; ok {
		delete(m.loopPending, toolUseID)
		if !isError {
			if mm := wakeInRe.FindStringSubmatch(result); mm != nil { // "Next wakeup scheduled for 11:57:00 (in 1859s)"
				if n, err := strconv.Atoi(mm[1]); err == nil {
					l.NextAt = ts.Add(time.Duration(n) * time.Second)
				}
			}
			if m.Loop != nil && m.Loop.Kind == "wakeup" {
				l.Ticks = m.Loop.Ticks
			}
			l.Ticks++
			m.Loop = &l
		}
		return
	}
	t, ok := m.bgPending[toolUseID]
	if !ok {
		if mv := bgMovedRe.FindStringSubmatch(result); mv != nil && !isError {
			m.removeBackground(mv[1])
			m.Background = append(m.Background, proto.BackgroundTask{ID: mv[1], Kind: "shell", Description: "超时转到后台的命令",
				OutputFile: outputFile(result), StartedAt: ts})
		}
		return
	}
	delete(m.bgPending, toolUseID)
	match := bgIDRe.FindStringSubmatch(result)
	if isError || match == nil {
		return // ran in the foreground after all, or failed to start
	}
	t.ID = match[1]
	t.OutputFile = outputFile(result)
	if t.StartedAt.IsZero() {
		t.StartedAt = ts
	}
	m.removeBackground(t.ID)
	m.Background = append(m.Background, t)
}

func outputFile(result string) string {
	if o := bgOutputRe.FindStringSubmatch(result); o != nil {
		return o[1]
	}
	return ""
}

func (m *Meta) taskFinished(notification string) {
	if id := firstTag(notification, "task-id"); id != "" {
		m.removeBackground(id)
	}
}

func (m *Meta) removeBackground(id string) {
	if id == "" {
		return
	}
	for i, t := range m.Background {
		if t.ID == id {
			m.Background = append(m.Background[:i:i], m.Background[i+1:]...)
			return
		}
	}
}

func (m *Meta) applyQueue(op, content string) {
	// Queued text keeps the <pasted_content> wrapper of long / voice input; show what the user wrote.
	content = strings.TrimSpace(pastedRe.ReplaceAllString(content, ""))
	if from, body, ok := CrossSession(content); ok {
		content = "【来自 " + from + "】" + body
	}
	removeOne := func() bool {
		for i, q := range m.Queue {
			if q == content {
				m.Queue = append(m.Queue[:i:i], m.Queue[i+1:]...)
				return true
			}
		}
		return false
	}
	switch op {
	case "enqueue":
		if content != "" {
			m.Queue = append(m.Queue, content)
		}
	case "dequeue":
		if len(m.Queue) > 0 {
			m.Queue = m.Queue[1:]
		}
	case "remove", "popAll":
		if content == "" || !removeOne() {
			if op == "popAll" {
				m.Queue = nil
			} else if len(m.Queue) > 0 {
				m.Queue = m.Queue[1:]
			}
		}
	}
}

// A message another Claude session sent this one ("Another Claude session sent a message:" + the tagged body + an
// English note on how to treat it).
// caveatRe drops Claude Code's note that the lines around it came from a local command (context for the model).
var caveatRe = regexp.MustCompile(`(?s)<local-command-caveat>.*?</local-command-caveat>`)

var teammateRe = regexp.MustCompile(`(?s)<teammate-message([^>]*)>\s*(.*?)\s*</teammate-message>`)
var teammateIDRe = regexp.MustCompile(`teammate_id="([^"]*)"`)

var crossRe = regexp.MustCompile(`(?s)<cross-session-message([^>]*)>\s*(.*?)\s*</cross-session-message>`)
var crossNameRe = regexp.MustCompile(`from-name="([^"]*)"`)

// CrossSession returns the sender and body of a cross-session message, ok false for anything else.
func CrossSession(s string) (from, body string, ok bool) {
	m := crossRe.FindStringSubmatch(s)
	if m == nil {
		return "", "", false
	}
	from = "另一个会话"
	if n := crossNameRe.FindStringSubmatch(m[1]); n != nil && n[1] != "" {
		from = n[1]
	}
	return from, strings.TrimSpace(m[2]), true
}

// markWake flags a background task's notice that arrives as a user message: the agent was idle and the notice
// started a new turn (ones absorbed mid-turn come as queued_command attachments instead).
func markWake(items []proto.Item) {
	for i := range items {
		if items[i].Kind == proto.KindNotice {
			items[i].Wake = true
		}
	}
}

func userText(mk func(int, string, string) proto.Item, s string, meta *Meta) []proto.Item {
	trimmed := strings.TrimSpace(caveatRe.ReplaceAllString(pastedRe.ReplaceAllString(s, ""), ""))
	if trimmed == "" {
		return nil
	}
	if from, body, ok := CrossSession(trimmed); ok {
		it := mk(0, "user", proto.KindText)
		it.Text, it.From = clip(body), from
		return []proto.Item{it}
	}
	if m := teammateRe.FindStringSubmatch(trimmed); m != nil { // agent teams: a teammate reporting to the lead
		from := "队友"
		if n := teammateIDRe.FindStringSubmatch(m[1]); n != nil && n[1] != "" {
			from = n[1]
		}
		it := mk(0, "user", proto.KindText)
		it.Text, it.From = clip(strings.TrimSpace(m[2])), from
		return []proto.Item{it}
	}
	// "! cmd" typed in Claude Code runs the command directly: the input reads as the user's own line, its output
	// as a Bash call under it (the input line comes first; its command is kept for the output's call).
	if strings.HasPrefix(trimmed, "<bash-input>") {
		cmd := strings.TrimSpace(firstTag(trimmed, "bash-input"))
		meta.bashInput = cmd
		it := mk(0, "user", proto.KindText)
		it.Text = clip("! " + cmd)
		return []proto.Item{it}
	}
	if strings.HasPrefix(trimmed, "<bash-stdout>") || strings.HasPrefix(trimmed, "<bash-stderr>") {
		out := strings.TrimSpace(strings.TrimSpace(firstTag(trimmed, "bash-stdout")) + "\n" + strings.TrimSpace(firstTag(trimmed, "bash-stderr")))
		input, _ := json.Marshal(map[string]string{"command": meta.bashInput})
		meta.bashInput = ""
		use := mk(0, "assistant", proto.KindToolUse)
		id := strings.TrimSuffix(use.ID, "#0") + "-bash"
		use.ToolUseID, use.ToolName, use.ToolInput = id, "Bash", input
		res := mk(1, "user", proto.KindToolResult)
		res.ToolUseID, res.Text, res.IsError = id, clip(out), strings.TrimSpace(firstTag(trimmed, "bash-stderr")) != ""
		return []proto.Item{use, res}
	}
	if strings.HasPrefix(trimmed, "<command-name>") || strings.HasPrefix(trimmed, "<command-message>") {
		name := firstTag(trimmed, "command-name")
		args := firstTag(trimmed, "command-args")
		it := mk(0, "user", proto.KindText)
		it.Text = strings.TrimSpace(name + " " + args)
		return []proto.Item{it}
	}
	if strings.HasPrefix(trimmed, "<local-command-stdout>") || strings.HasPrefix(trimmed, "<local-command-stderr>") {
		it := mk(0, "system", proto.KindNotice)
		it.Text = clip(strings.TrimSpace(ansiRe.ReplaceAllString(tagRe.ReplaceAllString(trimmed, "$2"), "")))
		if it.Text == "" {
			return nil
		}
		return []proto.Item{it}
	}
	if strings.HasPrefix(trimmed, "<task-notification>") {
		if meta.notified[trimmed] {
			return nil
		}
		if meta.notified == nil {
			meta.notified = map[string]bool{}
		}
		meta.notified[trimmed] = true
		it := mk(0, "system", proto.KindNotice)
		summary := firstTag(trimmed, "summary")
		if summary == "" {
			summary = "后台任务有更新"
		}
		it.Text = clip(summary)
		return []proto.Item{it}
	}
	if trimmed == "[Request interrupted by user]" || trimmed == "[Request interrupted by user for tool use]" {
		it := mk(0, "system", proto.KindNotice)
		it.Text = "已中断"
		return []proto.Item{it}
	}
	text := strings.TrimSpace(tagRe.ReplaceAllStringFunc(trimmed, func(m string) string {
		if strings.HasPrefix(m, "<system-reminder>") {
			return ""
		}
		return m
	}))
	if text == "" {
		return nil
	}
	it := mk(0, "user", proto.KindText)
	it.Text = clip(text)
	return []proto.Item{it}
}

// ansiRe matches terminal escape sequences (colors, bold) that local command output keeps.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// apiErrorText is what an api_error line says went wrong: error.formatted / error.message, or older lines' content.
func apiErrorText(e entry) string {
	var s string
	if json.Unmarshal(e.Error, &s) == nil && s != "" {
		return s
	}
	var obj struct {
		Message   string `json:"message"`
		Formatted string `json:"formatted"`
	}
	if json.Unmarshal(e.Error, &obj) == nil {
		if obj.Formatted != "" {
			return obj.Formatted
		}
		if obj.Message != "" {
			return obj.Message
		}
	}
	if s := strings.TrimSpace(rawString(e.Content)); s != "" {
		return s
	}
	return "请求失败"
}

// snippetLine is the first line of s, at most n runes.
func snippetLine(s string, n int) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return string(r)
}

func firstTag(s, tag string) string {
	open, close := "<"+tag+">", "</"+tag+">"
	i := strings.Index(s, open)
	if i < 0 {
		return ""
	}
	rest := s[i+len(open):]
	j := strings.Index(rest, close)
	if j < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:j])
}

func toolResultContent(raw json.RawMessage) (string, []*proto.Image) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, nil
	}
	var blocks []block
	if json.Unmarshal(raw, &blocks) != nil {
		return "", nil
	}
	var parts []string
	var imgs []*proto.Image
	for _, b := range blocks {
		switch b.Type {
		case "text":
			parts = append(parts, b.Text)
		case "image":
			if img := imageOf(b); img != nil {
				imgs = append(imgs, img)
			}
		case "tool_reference":
			parts = append(parts, "已加载工具 "+b.Name)
		}
	}
	return strings.Join(parts, "\n"), imgs
}

func imageOf(b block) *proto.Image {
	if b.Source == nil || b.Source.Type != "base64" || len(b.Source.Data) > maxImageLen {
		return nil
	}
	return &proto.Image{Mime: b.Source.MediaType, Data: b.Source.Data}
}

func rawString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

func clip(s string) string { return clipTo(s, maxTextLen) }

// clipTo cuts s to at most n bytes on a UTF-8 boundary and notes the original size.
func clipTo(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut] + fmt.Sprintf("\n…（已截断，共 %d 字节）", len(s))
}

// clipJSON shortens very long string fields (e.g. Write content) inside tool inputs.
func clipJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) <= maxTextLen {
		return raw
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return raw
	}
	for k, v := range m {
		if s, ok := v.(string); ok && len(s) > maxTextLen/2 {
			m[k] = clipTo(s, maxTextLen/2)
		}
	}
	b, _ := json.Marshal(m)
	return b
}

// File incrementally parses a transcript file. Safe for concurrent use.
type File struct {
	Path string

	mu      sync.Mutex
	offset  int64
	lines   int
	partial []byte
	items   []proto.Item
	meta    Meta
}

func NewFile(path string) *File { return &File{Path: path} }

// NewSubagentFile reads a subagent's transcript (<session>/subagents/agent-<id>.jsonl, every line a sidechain).
func NewSubagentFile(path string) *File { return &File{Path: path, meta: Meta{Sidechain: true}} }

// Refresh reads newly appended complete lines and returns the new items.
func (f *File) Refresh() ([]proto.Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fh, err := os.Open(f.Path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	st, err := fh.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() < f.offset { // truncated or replaced: start over
		f.offset, f.lines, f.partial, f.items, f.meta = 0, 0, nil, nil, Meta{}
	}
	if st.Size() == f.offset {
		return nil, nil
	}
	if _, err := fh.Seek(f.offset, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(fh)
	if err != nil {
		return nil, err
	}
	f.offset += int64(len(data))
	data = append(f.partial, data...)
	var fresh []proto.Item
	for {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			break
		}
		line := data[:i]
		data = data[i+1:]
		f.lines++
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		fresh = append(fresh, ParseLine(f.lines, line, &f.meta)...)
	}
	f.partial = append([]byte(nil), data...)
	f.items = append(f.items, fresh...)
	return fresh, nil
}

// Page returns items with seq < beforeSeq (0 = from the end), newest `limit`, in ascending order.
func (f *File) Page(beforeSeq, limit int) ([]proto.Item, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	end := len(f.items)
	if beforeSeq > 0 {
		end = 0
		for end < len(f.items) && f.items[end].Seq < beforeSeq {
			end++
		}
	}
	start := max(end-limit, 0)
	// Never split a transcript line across pages.
	for start > 0 && f.items[start].Seq == f.items[start-1].Seq {
		start--
	}
	out := append([]proto.Item(nil), f.items[start:end]...)
	return out, start > 0
}

// PageTurns returns the items before beforeSeq (0 = the end) going back far enough to hold `turns` whole exchanges:
// it starts at a user prompt, so a page never opens in the middle of a turn. maxItems caps one enormous turn.
func (f *File) PageTurns(beforeSeq, turns, maxItems int) ([]proto.Item, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	end := len(f.items)
	if beforeSeq > 0 {
		end = 0
		for end < len(f.items) && f.items[end].Seq < beforeSeq {
			end++
		}
	}
	start, seen := end, 0
	for start > 0 && seen < turns && end-start < maxItems {
		start--
		if it := f.items[start]; it.Role == "user" && it.Kind == proto.KindText && !it.Queued {
			seen++
		}
	}
	for start > 0 && f.items[start].Seq == f.items[start-1].Seq { // never split a transcript line across pages
		start--
	}
	out := append([]proto.Item(nil), f.items[start:end]...)
	return out, start > 0
}

// Item finds one item by id (an image a client fetches on demand).
func (f *File) Item(id string) (proto.Item, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.items) - 1; i >= 0; i-- {
		if f.items[i].ID == id {
			return f.items[i], true
		}
	}
	return proto.Item{}, false
}

// After returns items with seq > afterSeq, oldest first: at least limit of them unless fewer exist, and never part of
// a line (a Codex tool call is its call and its result on one line; a client resuming from the last seq it got would
// never see the rest). more says items are left after the last one returned.
func (f *File) After(afterSeq, limit int) (items []proto.Item, more bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := 0
	for i < len(f.items) && f.items[i].Seq <= afterSeq {
		i++
	}
	end := min(i+limit, len(f.items))
	for end > i && end < len(f.items) && f.items[end].Seq == f.items[end-1].Seq {
		end++
	}
	if end == i {
		return nil, false
	}
	return append([]proto.Item(nil), f.items[i:end]...), end < len(f.items)
}

func (f *File) Meta() Meta {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.meta
}

func (f *File) LastSeq() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lines
}
