package prompts

import "time"

// AssistantMachine is one computer in the assistant's CLAUDE.md, as seen from where the assistant runs.
type AssistantMachine struct {
	Name, ID string
	Local    bool   // the machine the assistant runs on
	SSHHost  string // "" uses ID
	// AgentsOnly: no command reaches it from where the assistant runs (the ssh aliases are the hub machine's, and a
	// link or tailcat machine has none), only the Agents tools do.
	AgentsOnly bool
}

// AssistantClaudeMD is the assistant directory's CLAUDE.md (assistant/claude_md).
type AssistantClaudeMD struct {
	Owner        string // "" is 用户
	Machines     []AssistantMachine
	Notes        string // 设置 › 助手 › 关于我, trimmed
	ExtraDirs    []string
	StateDir     string
	KnowledgeDir string
	OnHub        bool
	// LarkCLI: hub.json configures lark-cli (docs/plan-lark-cli.md), on the assistant's PATH.
	LarkCLI bool
	// PersonaView: every app draws ```persona-view (hub.json assistant.persona_view); else the assistant writes
	// ```persona-card, which they all draw.
	PersonaView bool
}

func RenderAssistantClaudeMD(d AssistantClaudeMD) string { return render("assistant/claude_md", d) }

// SnapshotMachine, SnapshotAgent… are the parts of the live snapshot (assistant/snapshot).
type SnapshotMachine struct {
	Name, ID string
	Online   bool
}

type SnapshotAgent struct {
	Label, Machine, Session string // Session: the short id
	Line                    string // what it waits for, or what it concluded ("" when not known)
	Link                    string // its page in the hub's web app ("" when the hub has no public_url)
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

// SnapshotTask is a task opened from an assistant conversation: a todo and its agents (docs/unified-entry.md).
type SnapshotTask struct {
	ID, Title, State string
	Machine, Session string // its latest agent, when it has one
	Last             string // what that agent said last, in a line
	// Doing: the step that agent is on (its latest tool call, transcript.Activity) while it works or has said nothing
	// yet, and DoingFor how long that step has gone on ("3 分钟"), "" when unknown or not working.
	Doing, DoingFor string
	Subagents       []string // that agent's running subagents, what each is for: on top of its own step, never instead
	Link            string   // the todo's page in the hub's web app ("" when the hub has no public_url)
}

// Snapshot is the live context a starting assistant gets in its system prompt.
type Snapshot struct {
	Now           time.Time // in the user's zone
	Zone          string
	Machines      []SnapshotMachine
	Needs, Unread []SnapshotAgent
	// Waiting: stopped on something the hub waits out for them (a retry, a deploy, another agent); Done: finished
	// within a day, with the conclusion (docs/agent-work.md「Persona 知道」).
	Waiting, Done []SnapshotAgent
	Working       []string
	PhoneOnline   bool
	KB            string            // RenderKBDigest, "" leaves it out
	Chats         *SnapshotChats    // nil: Feishu did not answer in time
	Calendar      *SnapshotCalendar // nil: the calendar did not answer in time
	Followups     []SnapshotFollowup
	Tasks         []SnapshotTask // tasks opened from assistant conversations (task_plan), newest first
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

// Upcoming is the calendar without the meetings over by Now (the snapshot's sources are read less often than it is
// rendered).
func (s Snapshot) Upcoming() *SnapshotCalendar {
	if s.Calendar == nil {
		return nil
	}
	out := SnapshotCalendar{}
	for _, e := range s.Calendar.Events {
		if e.End.After(s.Now) {
			out.Events = append(out.Events, e)
		}
	}
	return &out
}

func RenderSnapshot(s Snapshot) string { return render("assistant/snapshot", s) }

// AssistantContext is the hub's answer to assistant.context: the snapshot, for the main conversation with a line
// saying so first (assistant/context). Node puts what changed of it at the start of each message (TurnContext).
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

// TaskNotice tells the main conversation about one of its quiet tasks (#1731): it was done, waits for the user, was
// cut off by the model service or did not start. The assistant decides what to tell the user.
type TaskNotice struct {
	Title, Machine string
	State          string   // done | needs | interrupted | failed
	Earlier        []string // what else it went through since the last notice ("在等用户", "做完了"), oldest first
	Ask            string   // needs: what it asks
	Text           string   // what its agent said last, or why it did not start
}

func RenderTaskNotice(n TaskNotice) string { return render("assistant/task_notice", n) }

// Relay is a message one assistant conversation sends another (conversation_send / conversation_new): what was
// written, behind a note saying where it comes from and how many hops it is from the user's own message.
type Relay struct {
	FromID, FromTitle string
	Tool              string // conversation_send | conversation_new
	Hop, MaxHops      int
	Text              string
}

// RenderRelay is the message the other conversation gets (assistant/relay).
func RenderRelay(r Relay) string { return render("assistant/relay", r) }

// RenderRefused is passed into a running Codex turn when a tool call was refused (assistant/refused).
func RenderRefused(reason string) string { return render("assistant/refused", reason) }

// CodexMemory tells the Codex assistant about the memory it shares with Claude (assistant/codex_memory).
type CodexMemory struct {
	Dir       string
	Index     string // MEMORY.md, trimmed
	Truncated bool   // Index is the start of a longer one
}

func RenderCodexMemory(m CodexMemory) string { return render("assistant/codex_memory", m) }

// RuntimeInstructions is what the assistant on an agent runtime (DSH…) gets with a new conversation's first message
// (assistant/runtime_instructions): the shared memory, and the skills' gated commands it runs as tools.
type RuntimeInstructions struct {
	Dir       string
	Index     string // MEMORY.md, trimmed
	Truncated bool   // Index is the start of a longer one
	// Gates are the skills' gated commands this machine runs as tools (a plugin's assistant.gate with a tool).
	Gates []RuntimeGate
}

// RuntimeGate is one gated command as a runtime's tool: what the card calls it, the command, the tool.
type RuntimeGate struct {
	Title, Command, Tool string
}

func RenderRuntimeInstructions(r RuntimeInstructions) string {
	return render("assistant/runtime_instructions", r)
}

// RenderCodexInstructions is the Codex assistant's developer instructions (assistant/codex_instructions): the memory.
// The live snapshot is not in them but in each message (TurnContext), like Claude's.
func RenderCodexInstructions(memory string) string {
	return render("assistant/codex_instructions", struct{ Memory string }{memory})
}

// TurnContext is the <jarvis-context> block a message to the assistant starts with (assistant/turn_context): the
// sections of the live snapshot (AssistantContext) the conversation hasn't seen yet. Full: all of them (the
// conversation is new, was compacted, or node lost track); otherwise only those that changed, the time always.
type TurnContext struct {
	Full     bool
	Sections []string
}

func RenderTurnContext(c TurnContext) string { return render("assistant/turn_context", c) }

// TaskDone is the system prompt deciding whether a task opened from an assistant conversation is finished, so the
// hub can mark its todo done (assistant/task_done, docs/todos.md「自动完成」).
type TaskDone struct {
	Owner string
}

func RenderTaskDone(d TaskDone) string { return render("assistant/task_done", d) }

// TaskDoneInput is the task as it was handed out and what its agents said when they stopped (assistant/task_done_input).
type TaskDoneInput struct {
	Title, Prompt string
	Replies       []TaskDoneReply
	// Hints the hub found in the replies: a merged pull request, a TestFlight build number.
	MergedPR, Build bool
}

// TaskDoneReply is one agent's last reply, and what the user last asked it in that turn when it was not the task's
// own prompt (a follow-up the user sent the agent later).
type TaskDoneReply struct {
	Machine, Ask, Text string
}

func RenderTaskDoneInput(in TaskDoneInput) string { return render("assistant/task_done_input", in) }

// MainHandoff is the first message of a new main conversation: what the old one left (its last compaction summary,
// the messages after it), behind a note, then what the user wrote (docs/assistant.md「新建主对话」).
type MainHandoff struct {
	FromID  string
	Until   string // when the old one was archived, in the user's words
	Summary string
	Recent  []HandoffTurn
	Text    string
}

// HandoffTurn is one message of the old conversation: Role user or assistant.
type HandoffTurn struct {
	Role string
	Text string
}

// RenderMainHandoff is assistant/main_handoff.
func RenderMainHandoff(h MainHandoff) string { return render("assistant/main_handoff", h) }
