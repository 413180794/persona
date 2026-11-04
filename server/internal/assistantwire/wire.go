// Package assistantwire is what the hub and the assistant (internal/assistant, its own process) say to each other
// over the bus (docs/protocol.md「总线」, docs/assistant.md「和 hub 的边界」): each method's and event's name, params
// and result, the error codes that cross it, and the routes the hub relays. Both sides import it; neither imports
// the other.
package assistantwire

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// Hello is manager.hello's answer, and manager.updated's params (the answer again when it changed).
type Hello struct {
	Timezone   string `json:"timezone"` // IANA name; "" is the hub's own zone
	Owner      string `json:"owner"`
	OwnerNotes string `json:"owner_notes"`
	// Assistant is where the hub's settings run the assistant (settings `assistant`, still shared with features
	// outside this package, docs/assistant.md「和 hub 的边界」).
	Assistant Placement `json:"assistant"`
	// DoneDelayMS is how long a finished turn stays still before it counts as the end (hub.json notify): a task's
	// agents are followed with the same patience as the hub's own pushes.
	DoneDelayMS int64 `json:"done_delay_ms"`
}

// Placement is the machine the assistant's conversations live on and its main conversation.
type Placement struct {
	MachineID string `json:"machine_id"` // "" when the hub has no machine
	Main      string `json:"main,omitempty"`
	// Standby: this hub is a member of a mesh group, whose center runs the scheduled tasks (docs/mesh.md).
	Standby bool `json:"standby,omitempty"`
}

// DeviceCall is device.call's params. Device is a machine id, or DeviceAll for DeviceDevicesList.
type DeviceCall struct {
	Device string          `json:"device"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// The machine methods the assistant calls. The node's own methods go to the machine's node as they are; the hub
// answers the others from what it holds about the machine.
const (
	// The node's (docs/protocol.md「hub ↔ node」).
	DeviceSessionItems  = "session.items"  // {session_id, turns} → {items}
	DeviceSessionRename = "session.rename" // {session_id, title} → {ok}
	DeviceAssistantSend = "assistant.send" // {session_id, text, resume_only?, permission_mode?, …model} → {session_id}
	DeviceNodeConfigure = "node.configure" // {keep_sessions} → {}
	DeviceSessionSend   = "session.send"   // {session_id, text} → {}
	DeviceSessionStart  = "session.start"  // {cwd, prompt, launcher?, model?, permission_mode?, tab_name?} → {session}

	// The hub's.
	// DeviceDevicesList (on DeviceAll): {} → {devices: [Device], phone_online}, the user's machines.
	DeviceDevicesList = "devices.list"
	// DeviceInteractionsList (on DeviceAll): {} → {interactions}, what the machines' agents wait on the user for.
	DeviceInteractionsList = "interactions.list"
	// DeviceSessionsList: {} → {sessions}, the machine's sessions as the apps get them (state, titles).
	DeviceSessionsList = "sessions.list"
	// DeviceSessionGet: {session_id} → {session, ok}, one session as the node listed it.
	DeviceSessionGet = "session.get"
	// DeviceAssistantStart: {text, images?, launcher?, permission_mode?, effort?, purpose?} → {session}. A new
	// conversation: the hub prepares the assistant's directory on the machine (its CLAUDE.md is still made from the
	// hub's state) and applies the model configuration of purpose ("" = the assistant's) before the node's
	// assistant.send.
	DeviceAssistantStart = "assistant.start"
)

// DeviceAll is the device of the calls about every machine.
const DeviceAll = "*"

// Device is one of the user's machines as devices.list has it.
type Device struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	SSHHost string `json:"ssh_host,omitempty"` // the hub machine's ssh alias for it
	Home    string `json:"home,omitempty"`     // "" while it has not reported one
	Online  bool   `json:"online"`
	// Reachable: a call to it would go out now (its link is up; Online can lag a reconnect).
	Reachable bool `json:"reachable"`
}

// ErrNoDevice: the hub has no machine by the id a DeviceCall gave.
var ErrNoDevice = errors.New("no such machine")

// AppCall is app.call's params (appkit.RelayParams on the bus).
type AppCall struct {
	AppID  string          `json:"app_id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// The apps the hub runs for the assistant and their methods.
const (
	// AppModels is the hub's model configuration (docs/model-config.md), outside this package for now: these calls go
	// away when the assistant has its own model configuration (v9's manager.models).
	AppModels = "models"
	// ModelsComplete: {purpose, system, input, max_tokens} → {text}; an error the model's (ai.IsPaused tells a pause).
	ModelsComplete = "models.complete"
	// ModelsApply: {purpose, params} → {params}: a node call's params with the purpose's launcher / model filled in.
	ModelsApply = "models.apply"
	// ModelsResolve: {purpose} → ModelBinding, what the purpose runs on.
	ModelsResolve = "models.resolve"
	// ModelsJSON: {purpose, system, input} → the JSON object the model answered.
	ModelsJSON = "models.json"
	// ModelsTabName: {prompt} → {name}, a herdr tab's name for an agent started with prompt ("" when none).
	ModelsTabName = "models.tab_name"
	// ModelsAssistantSend: {machine_id, session_id, params} → {params}: a message for one of the assistant's
	// conversations with the launcher and model it runs on filled in, as the phone's messages get them (the hub's
	// assistantSendModel: the main conversation and a task's own follow 模型配置 › 主对话, the others keep theirs).
	ModelsAssistantSend = "models.assistant_send"

	// AppKB is the hub's knowledge base (docs/knowledge.md).
	AppKB = "kb"
	// KBStatus: {} → {ready}.
	KBStatus = "kb.status"
	// KBPrompt: {projects, chats} → {text}, what the live context says about it ("" without one).
	KBPrompt = "kb.prompt"

	// AppConnectors is the hub's connectors (docs/connectors.md).
	AppConnectors = "connectors"
	// ConnectorsSnapshots: {tz} → {snapshots: [connector.SnapshotData]}, the live states of the signed-in connectors
	// that keep one (the chats waiting on the user, today's meetings), those that answered within a few seconds.
	ConnectorsSnapshots = "connectors.snapshots"

	// AppInteractions is the hub's interaction queue: its tool interaction_answer answers another agent's question for
	// the user (app.tool).
	AppInteractions = "interactions"

	// AppInbox is the hub's inbox (docs/inbox.md); AppSettings is Persona's own settings (jarvis_settings).
	AppInbox    = "inbox"
	AppSettings = "settings"
	// InboxReplied: {ids} → {replied: {id: time}}, when those of the items the user replied to were replied to.
	InboxReplied = "inbox.replied"

	// AppTool: {name, arguments, machine_id, session_id} → proto.ToolResult, one of the assistant's tools that is the
	// app's to run (kb_*, interaction_answer, inbox_draft, jarvis_settings).
	AppTool = "app.tool"

	// AppTodos is the hub's todos (docs/todos.md): a task the assistant hands out is a todo whose origin is the
	// conversation.
	AppTodos = "todos"
	// TodosLoad: {} → {}, or why the todos can't be read.
	TodosLoad = "todos.load"
	// TodosCreate: {title, notes, origin, plan_id, if_quiet} → {todo}; if_quiet: only a todo that would be quiet
	// (#1731), else {todo: null}.
	TodosCreate = "todos.create"
	// TodosLinkAgent: {todo_id, machine_id, session_id, title, cwd} → {}, the agent works for the todo from now on.
	TodosLinkAgent = "todos.link_agent"
	// TodosFail: {todo_id, reason} → {}, a todo whose agent did not start ends, with why.
	TodosFail = "todos.fail"
	// TodosAutoDone: {todo_id, reason, work} → {ok}, done by the assistant's judge unless the user changed it since
	// work began.
	TodosAutoDone = "todos.auto_done"
	// TodosGet: {todo_id} → {todo, ok}.
	TodosGet = "todos.get"
	// TodosList: {origin} → {todos}, the todos opened from that conversation ("*": from any).
	TodosList = "todos.list"
	// TodosOfSession: {machine_id, session_id} → {todo, ok}, the open task the agent works for.
	TodosOfSession = "todos.of_session"
	// TodosRetry: {todo_id} → {todo, ok}, a task whose agent did not start reopened for another start (its error
	// cleared); not ok when another hub sharing the store reopened it first (todo as it is then). todo is as the
	// apps get it (all its fields, formatted).
	TodosRetry = "todos.retry"
	// TodosView: {todo_id} → {todo, ok}, the todo as the apps get it (all its fields, formatted).
	TodosView = "todos.view"
)

// Todo is a todo as the hub's todos app has it (the fields the assistant reads).
type Todo struct {
	ID         string      `json:"id"`
	Title      string      `json:"title"`
	Notes      string      `json:"notes"`
	DoneAt     *time.Time  `json:"done_at"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
	Origin     string      `json:"origin,omitempty"`
	PlanID     string      `json:"plan_id,omitempty"`
	StartError string      `json:"start_error,omitempty"`
	DoneBy     string      `json:"done_by,omitempty"`
	DoneReason string      `json:"done_reason,omitempty"`
	ReopenedAt *time.Time  `json:"reopened_at,omitempty"`
	Agents     []TodoAgent `json:"agents"`
	// Quiet: the main conversation opened it while the user wanted those told only there (#1731): the hub pushes
	// nothing about it; the assistant tells the main conversation (quiet.go).
	Quiet bool `json:"quiet,omitempty"`
}

// TodoAgent is one agent that works or worked for a todo.
type TodoAgent struct {
	MachineID   string     `json:"machine_id"`
	SessionID   string     `json:"session_id"`
	ReleasedAt  *time.Time `json:"released_at,omitempty"`
	DismissedAt *time.Time `json:"dismissed_at,omitempty"`
}

// ModelBinding is what a purpose runs on: a provider (launcher.<name> for a launcher) with a model and effort.
type ModelBinding struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Effort   string `json:"effort,omitempty"`
}

// NotifyRequest is notify.request's params.
type NotifyRequest struct {
	Notification Notification `json:"notification"`
}

// Notification is one notification intent: a push (kind assistant) to every device whose settings take it, like an
// agent's. The hub delivers it as it is: it does not derive what it means.
type Notification struct {
	ID        string `json:"id"` // stable, for deduplication
	Kind      string `json:"kind"`
	Title     string `json:"title,omitempty"`
	Body      string `json:"body,omitempty"`
	Subtitle  string `json:"subtitle,omitempty"`  // the line under the title: where it comes from
	DeviceID  string `json:"device_id,omitempty"` // the machine of the session it opens
	SessionID string `json:"session_id,omitempty"`
	// Asked: the conversation asked for this push itself (notify_user). It goes to the Feishu bot too (when its
	// pushes are on), and the conversation's own finished push is left out.
	Asked bool `json:"asked,omitempty"`
}

// NotifyPush is the kind of the assistant's pushes.
const NotifyPush = "assistant"

// Broadcast is ws.broadcast's params: a JSON object with a string "type", sent to the apps unchanged.
type Broadcast struct {
	Message json.RawMessage `json:"message"`
}

// Snapshot is manager.snapshot's params: the whole of what the assistant shows, every time (the hub keeps the
// newest by Seq and shows nothing it did not report).
type Snapshot struct {
	Epoch string    `json:"epoch"` // this run of the assistant
	Seq   uint64    `json:"seq"`   // increasing within Epoch
	At    time.Time `json:"at"`
	// Partial: some of the state could not be read (the database did not answer); the hub asks again (Resync) the
	// next time it shows it.
	Partial bool `json:"partial,omitempty"`
	// Sessions are the assistant's conversations it has something to say about.
	Sessions []SessionView `json:"sessions"`
	// Interactions are the questions it asks on the phone (ask_user: headless Claude has no AskUserQuestion). The hub
	// shows each like any agent's and sends the answer back (Handler.Action); one that leaves the list is withdrawn.
	Interactions []proto.Interaction `json:"interactions"`
	// Tasks are its scheduled tasks, as the hub's other parts need them (the model configuration's 「定时任务」, the
	// Feishu bot naming a task's conversation, the conversations a node keeps listed); null while they could not be read.
	Tasks []TaskView `json:"tasks"`
}

// TaskView is one scheduled task in the snapshot.
type TaskView struct {
	ID           string    `json:"id"`
	Label        string    `json:"label"`  // its title, or what stands for one
	Repeat       string    `json:"repeat"` // once / daily / interval
	Enabled      bool      `json:"enabled"`
	At           time.Time `json:"at,omitempty"` // once: when; interval: the next run
	EveryMinutes int       `json:"every_minutes,omitempty"`
	MachineID    string    `json:"machine_id,omitempty"` // of the conversation it came from
	RunMachineID string    `json:"run_machine_id,omitempty"`
	RunSessionID string    `json:"run_session_id,omitempty"` // its own conversation
}

// SessionView is what the assistant reports about one of its conversations, put on the session lists the apps get.
type SessionView struct {
	MachineID string   `json:"machine_id"`
	SessionID string   `json:"session_id"`
	Title     string   `json:"title,omitempty"` // 「MMDD | 类型 | 主题」 or the user's own (titles.go)
	Archived  bool     `json:"archived,omitempty"`
	Queued    []string `json:"queued,omitempty"` // messages from other conversations waiting for its turn to end
}

// ErrStaleSnapshot is manager.snapshot's stale_snapshot: a newer one came in first (two changes reported at once).
var ErrStaleSnapshot = errors.New("stale_snapshot: a newer snapshot came in")

// SnapshotResult is manager.snapshot's answer.
type SnapshotResult struct {
	OK  bool   `json:"ok"`
	Seq uint64 `json:"seq"`
}

// ---- what the hub sends the assistant (Handler) ----

// RoleHeader carries the role of the device a relayed request came from (the hub's auth decides it and sets the
// header, replacing any the client sent). Absent (a test's own request) means a full device.
const RoleHeader = "X-Persona-Role"

// ToolCall is assistant.tool's params.
type ToolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	MachineID string          `json:"machine_id"` // the machine of the conversation that called it
	SessionID string          `json:"session_id"` // the conversation; "" when it is not known
}

// SessionRef names one session on one machine.
type SessionRef struct {
	MachineID string `json:"machine_id"`
	SessionID string `json:"session_id"`
}

// ConversationStarted is conversation.started's params.
type ConversationStarted struct {
	MachineID string    `json:"machine_id"`
	SessionID string    `json:"session_id"`
	Started   time.Time `json:"started"`
	// Task is the scheduled task's name when one started it: its title is 「MMDD | 定时 | Task」 from now on.
	Task string `json:"task,omitempty"`
}

// ConversationRenamed is conversation.renamed's params.
type ConversationRenamed struct {
	MachineID string `json:"machine_id"`
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
}

// ContextParams is assistant.context's params.
type ContextParams struct {
	SessionID string `json:"session_id"`
}

// ContextResult is assistant.context's answer: the <jarvis-context> text.
type ContextResult struct {
	Text string `json:"text"`
}

// Action is manager.action's params (appkit.Action): op answer, with the user's answer (a proto.Resolution).
type Action struct {
	ActionID           string          `json:"action_id"`
	SessionID          string          `json:"session_id,omitempty"`
	Op                 string          `json:"op"` // answer
	TurnID             string          `json:"turn_id,omitempty"`
	InteractionID      string          `json:"interaction_id"`
	InteractionVersion uint64          `json:"interaction_version,omitempty"`
	Answer             json.RawMessage `json:"answer,omitempty"`
}

// ActionResult is manager.action's answer.
type ActionResult struct {
	OK  bool   `json:"ok"`
	Seq uint64 `json:"seq"`
}

// InteractionResolved is interaction.resolved's params: the card, the answer, and resolved / dismissed / failed.
type InteractionResolved struct {
	Interaction proto.Interaction `json:"interaction"`
	Resolution  proto.Resolution  `json:"resolution"`
	Status      string            `json:"status"`
}

// InteractionRaised is interaction.raised's params: an agent of a quiet task (#1731) asks the user something; the
// hub sends no push for it, the main conversation is told instead. The card carries its task's title.
type InteractionRaised struct {
	Interaction proto.Interaction `json:"interaction"`
}

// InboxMessage is inbox.message's params: the inbox item and its card in the task's conversation.
type InboxMessage struct {
	ItemID  string      `json:"item_id"`
	Message MessageCard `json:"message"`
}

// MessageCard is a message the inbox's AI 分身 drafted a reply to, about one of the user's tasks: what the card in the
// task's conversation shows until the item is sent or done (the app sends the draft through the inbox, by the item's
// id).
type MessageCard struct {
	TodoID  string `json:"todo_id"`
	Source  string `json:"source"`            // lark | mail | …
	Account string `json:"account,omitempty"` // 飞书 / the mail address
	Chat    string `json:"chat"`              // the chat's name / the mail's subject
	Sender  string `json:"sender,omitempty"`
	Text    string `json:"text"` // the message, clipped
	Draft   string `json:"draft"`
	// SentAt: the reply to the item went out (from this card or from the inbox, on any device) and Sent is what it
	// said; set by the assistant when the hub says so (assistant.cards_sent), never cleared, so the card stays sent
	// when the item opens again. A card sent before that was recorded has no Sent (the draft is what went out).
	SentAt *time.Time `json:"sent_at,omitempty"`
	Sent   string     `json:"sent,omitempty"`
}

// CardSentParams is assistant.card_sent's params.
type CardSentParams struct {
	CardID string `json:"card_id"`
	ItemID string `json:"item_id"`
}

// CardsSentParams is assistant.cards_sent's params: the reply to inbox item ItemID went out at At, saying Text ("":
// not known).
type CardsSentParams struct {
	ItemID string    `json:"item_id"`
	At     time.Time `json:"at"`
	Text   string    `json:"text,omitempty"`
}

// APIErrorMarkers start what Claude Code answers when the model call itself failed (an error card in the app). Both
// sides read a turn by them: the hub's session lists say an agent's turn was cut off (proto.Session.TurnError), the
// assistant's scheduled runs and tasks act on it.
var APIErrorMarkers = []string{"API Error:", "There's an issue with the selected model", "Failed to authenticate",
	"OAuth token has expired", "Invalid API key"}

// InterruptedSummary is what is said of a turn the model service cut off, instead of its last words (the error is no
// conclusion, #1516): an interrupted task's receipt, the push.
const InterruptedSummary = "模型服务出错，中断了"

// CutOff: an agent's answer is the model API error that ended its turn (it starts with one).
func CutOff(answer string) bool {
	answer = strings.TrimSpace(answer)
	for _, marker := range APIErrorMarkers {
		if strings.HasPrefix(answer, marker) {
			return true
		}
	}
	return false
}

// DeviceSessions is sessions.changed's params: the list as the node sent it.
type DeviceSessions struct {
	MachineID string          `json:"machine_id"`
	Sessions  []proto.Session `json:"sessions"`
}

// ---- the bus ----

// AssistantID is the bus identity the assistant connects as (a manager's token, hub/bustokens.go).
const AssistantID = "assistant"

// The methods and events, by direction. The assistant's handshake is manager.hello {id, host, os, arch, deploy,
// version}; its answer is Hello (with the hub's name, role and managers besides).
const (
	// The assistant calls the hub (Hub).
	MethodHello    = "manager.hello"
	MethodDevice   = "device.call"
	MethodApp      = "app.call"
	MethodNotify   = "notify.request"
	MethodSnapshot = "manager.snapshot"
	EventBroadcast = "ws.broadcast" // one way

	// The hub calls the assistant (Handler).
	MethodHTTP    = "http.request" // HTTPRequest → HTTPResponse
	MethodTools   = "assistant.tools"
	MethodTool    = "assistant.tool"
	MethodContext = "assistant.context"
	MethodAction  = "manager.action"
	MethodTasks   = "assistant.tasks" // {} → {tasks: [prompts.InboxTask]}
	// MethodCardSent: CardSentParams → {sent}: the message card says its reply went out (发送 on it sends once).
	MethodCardSent = "assistant.card_sent"
	// MethodCardsSent: CardsSentParams → {}: the reply to an inbox item went out; its cards are marked sent.
	MethodCardsSent = "assistant.cards_sent"

	// The hub tells the assistant (one way).
	EventConversationStarted = "conversation.started"
	EventConversationRenamed = "conversation.renamed"
	EventUserSpoke           = "session.user_spoke"
	EventTurnStarted         = "session.turn_started"
	EventTurnEnded           = "session.turn_ended"
	EventSessionsChanged     = "sessions.changed"
	EventInteractionResolved = "interaction.resolved"
	EventInteractionRaised   = "interaction.raised"
	EventInboxMessage        = "inbox.message"
	EventUpdated             = "manager.updated" // Hello again: the profile or where the assistant runs changed
	EventResync              = "manager.resync"  // {}: report the whole snapshot again
)

// Error codes that cross the bus where a Go error would lose its kind (rpc.Error.Code).
const (
	CodeNoDevice         = "no_device"         // ErrNoDevice
	CodeMachineOffline   = "machine_offline"   // the machine's node is not connected (apierr.ErrOffline)
	CodeNodeClosed       = "node_closed"       // the link to the machine's node dropped during the call (rpc.ErrClosed)
	CodeStaleSnapshot    = "stale_snapshot"    // ErrStaleSnapshot
	CodeModelPaused      = "model_paused"      // the model's connection is held (ai.IsPaused)
	CodeAssistantOffline = "assistant_offline" // the hub has no connection from the assistant
)

// HTTPRequest is http.request's params: an app's request on one of Routes, as it came (header without the hub's
// credentials and hop-by-hop headers, the asking device's role in RoleHeader; body as base64).
type HTTPRequest struct {
	Method string              `json:"method"`
	Path   string              `json:"path"`
	Query  string              `json:"query,omitempty"`
	Header map[string][]string `json:"header,omitempty"`
	Body   []byte              `json:"body,omitempty"`
}

// HTTPResponse is http.request's answer, written back to the app unchanged.
type HTTPResponse struct {
	Status int                 `json:"status"`
	Header map[string][]string `json:"header,omitempty"`
	Body   []byte              `json:"body,omitempty"`
}

// Routes are the routes the assistant answers (net/http patterns): the hub relays the apps' requests on them as
// http.request. The assistant registers exactly these (assistant.TestRoutesAreTheWireOnes).
var Routes = []string{
	"POST /api/v1/assistant/conversations/{id}/archive",
	"POST /api/v1/assistant/conversations/{id}/unarchive",
	"GET /api/v1/assistant/conversations/{id}/feed",
	"POST /api/v1/assistant/conversations/{id}/route",
	"POST /api/v1/assistant/conversations/{id}/today",
	"GET /api/v1/assistant/tasks",
	"POST /api/v1/assistant/tasks",
	"PUT /api/v1/assistant/tasks/{id}",
	"DELETE /api/v1/assistant/tasks/{id}",
	"POST /api/v1/assistant/tasks/{id}/run",
	"POST /api/v1/assistant/tasks/{id}/fresh",
	"GET /api/v1/assistant/runs",
	"GET /api/v1/assistant/routines",
	"PUT /api/v1/assistant/routines",
	"POST /api/v1/assistant/routines/{id}/run",
	"GET /api/v1/assistant/followups",
	"POST /api/v1/assistant/followups/{id}/run",
	"DELETE /api/v1/assistant/followups/{id}",
	"GET /api/v1/task-plans/{id}",
	"POST /api/v1/task-plans/{id}/start",
	"POST /api/v1/task-plans/{id}/dismiss",
	"GET /api/v1/task-trust",
	"DELETE /api/v1/task-trust",
	"POST /api/v1/todos/{id}/done-check",
	"POST /api/v1/todos/{id}/retry",
}
