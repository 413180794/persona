package prompts

import "time"

// AssistantMachine is one computer in the assistant's CLAUDE.md.
type AssistantMachine struct {
	Name, ID string
	Local    bool   // the machine the assistant runs on
	SSHHost  string // "" uses ID
}

// AssistantClaudeMD is the assistant directory's CLAUDE.md (assistant/claude_md).
type AssistantClaudeMD struct {
	Owner     string // "" is 用户
	Machines  []AssistantMachine
	Notes     string // 设置 › 助手 › 关于我, trimmed
	ExtraDirs []string
}

func RenderAssistantClaudeMD(d AssistantClaudeMD) string { return render("assistant/claude_md", d) }

// SnapshotMachine, SnapshotAgent… are the parts of the live snapshot (assistant/snapshot).
type SnapshotMachine struct {
	Name, ID string
	Online   bool
}

type SnapshotAgent struct {
	Label, Machine, Session string // Session: the short id
}

type SnapshotChat struct {
	Name    string
	Unread  int
	Preview string // @me chats: the clipped preview
}

type SnapshotChats struct {
	AtMe, Other []SnapshotChat
}

func (c SnapshotChats) Total() int { return len(c.AtMe) + len(c.Other) }

type SnapshotEvent struct {
	Start, End time.Time // in the user's zone
	Title      string
}

type SnapshotCalendar struct {
	Events []SnapshotEvent
}

type SnapshotFollowup struct {
	At    time.Time // in the user's zone
	Label string
	Every string // "30 分钟" for a repeating one
}

// Snapshot is the live context a starting assistant gets in its system prompt.
type Snapshot struct {
	Now           time.Time // in the user's zone
	Zone          string
	Machines      []SnapshotMachine
	Needs, Unread []SnapshotAgent
	Working       []string
	PhoneOnline   bool
	KB            string            // RenderKBDigest, "" leaves it out
	Chats         *SnapshotChats    // nil: Feishu did not answer in time
	Calendar      *SnapshotCalendar // nil: the calendar did not answer in time
	Followups     []SnapshotFollowup
}

func (s Snapshot) Up() []SnapshotMachine   { return s.machines(true) }
func (s Snapshot) Down() []SnapshotMachine { return s.machines(false) }

func (s Snapshot) machines(online bool) []SnapshotMachine {
	var out []SnapshotMachine
	for _, m := range s.Machines {
		if m.Online == online {
			out = append(out, m)
		}
	}
	return out
}

func RenderSnapshot(s Snapshot) string { return render("assistant/snapshot", s) }

// AssistantContext is what node passes with --append-system-prompt: the snapshot, for the main conversation with a
// line saying so first (assistant/context).
type AssistantContext struct {
	Main     bool
	Owner    string
	Snapshot string
}

func RenderAssistantContext(c AssistantContext) string { return render("assistant/context", c) }

// TaskRun is the message a scheduled task's run starts with (assistant/task_run).
type TaskRun struct {
	Label          string
	Once, Interval bool   // neither: daily
	Every          string // interval: "30 分钟"
	Late           string // "" when on time; else how late ("25 分钟")
	Due            time.Time
	Prompt         string
	KBHint         bool // daily runs with the knowledge base on
	// Continued: it goes into the task's own conversation, which got the background on the first run.
	Continued bool
}

func RenderTaskRun(t TaskRun) string { return render("assistant/task_run", t) }

// RenderMorningRoutine and RenderEveningRoutine are the default daily tasks' prompts.
func RenderMorningRoutine() string { return render("assistant/routine_morning", nil) }
func RenderEveningRoutine() string { return render("assistant/routine_evening", nil) }

// RenderLostTasks tells an assistant conversation that a restart stopped its background tasks (assistant/lost_tasks).
func RenderLostTasks(tasks []string) string {
	return render("assistant/lost_tasks", struct{ Tasks []string }{tasks})
}

// RenderRefused is passed into a running Codex turn when a tool call was refused (assistant/refused).
func RenderRefused(reason string) string { return render("assistant/refused", reason) }

// CodexMemory tells the Codex assistant about the memory it shares with Claude (assistant/codex_memory).
type CodexMemory struct {
	Dir       string
	Index     string // MEMORY.md, trimmed
	Truncated bool   // Index is the start of a longer one
}

func RenderCodexMemory(m CodexMemory) string { return render("assistant/codex_memory", m) }

// RenderCodexInstructions is the Codex assistant's developer instructions: the live context, then the memory
// (assistant/codex_instructions).
func RenderCodexInstructions(snapshot, memory string) string {
	return render("assistant/codex_instructions", struct{ Snapshot, Memory string }{snapshot, memory})
}
