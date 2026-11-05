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

// Hello is manager.hello's answer, and manager.updated's params (the answer again when it changed): hub.json's
// assistant section, which the assistant takes as its defaults (its own settings are its own: Profile), and what the
// hub knows that it needs.
type Hello struct {
	Owner string `json:"owner"`
	// OwnerNotes is hub.json's assistant.notes: what the assistant knows about the user until 关于我 is edited.
	OwnerNotes string `json:"owner_notes"`
	// Assistant is where the hub would run the assistant's conversations by default.
	Assistant Placement `json:"assistant"`
	// DoneDelayMS is how long a finished turn stays still before it counts as the end (hub.json notify): a task's
	// agents are followed with the same patience as the hub's own pushes.
	DoneDelayMS int64 `json:"done_delay_ms"`
	// PersonaView is hub.json's assistant.persona_view: teach ```persona-view rather than ```persona-card (#1850).
	PersonaView bool `json:"persona_view,omitempty"`
	// KB is hub.json's kb section: the knowledge base the assistant keeps (kb.go).
	KB *KBSettings `json:"kb,omitempty"`
}

// Placement is the hub's defaults for where the assistant runs.
type Placement struct {
	// MachineID is hub.json's assistant.machine when the hub has it, else the hub's own machine, else the first one;
	// "" when the hub has no machine.
	MachineID string `json:"machine_id"`
	// Launcher is hub.json's assistant.launcher ("" is claude).
	Launcher string `json:"launcher,omitempty"`
	// Machines are the machines the hub has: a machine the assistant saved that is none of them is not used.
	Machines []string `json:"machines,omitempty"`
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
)

// DeviceAll is the device of the calls about every machine.
const DeviceAll = "*"

// Device is one of the user's machines as devices.list has it.
type Device struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind,omitempty"`     // local (the hub's own) | ssh | link …
	SSHHost string `json:"ssh_host,omitempty"` // the hub machine's ssh alias for it
	Home    string `json:"home,omitempty"`     // "" while it has not reported one
	DataDir string `json:"data_dir,omitempty"` // its node's data directory ("" while not reported, or the default)
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
	// TimeoutMS is how long the caller waits for the answer (its context's deadline); the hub stops there too. 0: none.
	TimeoutMS int64 `json:"timeout_ms,omitempty"`
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
	// ModelsJSON: {purpose, system, input, max_tokens?} → the JSON object the model answered (max_tokens: the room
	// for the answer, asked once more with twice that when it is cut off; the knowledge base's calls give it).
	ModelsJSON = "models.json"
	// ModelsTabName: {prompt} → {name}, a herdr tab's name for an agent started with prompt ("" when none).
	ModelsTabName = "models.tab_name"

	// AppConnectors is the hub's connectors (docs/connectors.md).
	AppConnectors = "connectors"
	// ConnectorsSnapshots: {tz} → {snapshots: [connector.SnapshotData]}, the live states of the signed-in connectors
	// that keep one (the chats waiting on the user, today's meetings), those that answered within a few seconds.
	ConnectorsSnapshots = "connectors.snapshots"

	// AppInteractions is the hub's interaction queue (InteractionsGet, InteractionsAnswer).
	AppInteractions = "interactions"

	// AppInbox is the hub's inbox (docs/inbox.md); AppSettings is Persona's own settings (jarvis_settings).
	AppInbox    = "inbox"
	AppSettings = "settings"
	// InboxReplied: {ids} → {replied: {id: time}}, when those of the items the user replied to were replied to.
	InboxReplied = "inbox.replied"

	// AppTool: {name, arguments, machine_id, session_id} → proto.ToolResult, one of the assistant's tools that is the
	// app's to run (jarvis_settings).
	AppTool = "app.tool"
)

// What the interaction queue and the inbox offer the assistant: data in, data out. The tools on them
// (interaction_answer, inbox_draft) are the assistant's, with their rules and wording.
const (
	// InteractionsGet: {interaction_id} → {interaction, ok}, the interaction while it is pending; when it is not (ok
	// false), status says how it ended (resolved, dismissed, expired, cancelled; "" when there is no such interaction).
	InteractionsGet = "interactions.get"
	// InteractionsAnswer: {machine_id, session_id, interaction_id, answers: {question: answer}} → {ok, status}: the
	// pending question of that session resolved with answers, answered by the assistant; ok false (with status as
	// InteractionsGet's) when it is no longer pending. An interaction that is not a question, or not that session's,
	// is an error.
	InteractionsAnswer = "interactions.answer"

	// InboxDraft: {session_id, text, note, changes: [{ref, field, value}]} → {item_id, refused?}: the draft (and the
	// moves of a document's rows it proposes) on the item whose AI 分身 runs in that conversation. item_id is "" when
	// there is none; refused is the first change the item does not offer (a row it lists, one of the column's values),
	// and then nothing is kept.
	InboxDraft = "inbox.draft"
)

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
	// Feishu bot naming a task's conversation); null while they could not be read.
	Tasks []TaskView `json:"tasks"`
	// Todos is which todo each agent works for (TodoReport); null while the todos could not be read.
	Todos *TodoReport `json:"todos"`
	// Profile is its settings as the hub's own features need them; null while they could not be read.
	Profile *Profile `json:"profile"`
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
	MachineID string `json:"machine_id"`
	SessionID string `json:"session_id"`
	// Title is 「MMDD | 类型 | 主题」 or the user's own (titles.go); the main conversation's is always 「Persona」.
	Title    string   `json:"title,omitempty"`
	Archived bool     `json:"archived,omitempty"`
	Queued   []string `json:"queued,omitempty"` // messages from other conversations waiting for its turn to end
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

// DeviceSessions is sessions.changed's params: the list as the node sent it, and when the node's connection to the hub
// began (a new one: the node may have restarted and forgotten what it was told, e.g. the herdr todo marks).
type DeviceSessions struct {
	MachineID string          `json:"machine_id"`
	Sessions  []proto.Session `json:"sessions"`
	Connected time.Time       `json:"connected,omitzero"`
}

// ---- the todos (the assistant's, docs/todos.md) ----

// TodoReport is what the hub shows of the todos: the todo each agent works for now (the open todos' active links: the
// session lists' todo, the task named on its agents' cards) and the agents kept quiet (#1731: no pushes, badges or Live
// Activity; those of quiet tasks open or done in the last week). Small, whole every time.
type TodoReport struct {
	Links []TodoLink   `json:"links"`
	Quiet []SessionRef `json:"quiet,omitempty"`
}

// TodoLink is one agent that works for an open todo.
type TodoLink struct {
	MachineID string `json:"machine_id"`
	SessionID string `json:"session_id"`
	TodoID    string `json:"todo_id"`
	Title     string `json:"title"` // the todo's
	// Task: the todo is a task an assistant conversation opened (its agents' cards name it: proto.Interaction.TaskTitle).
	Task bool `json:"task,omitempty"`
}

// TodoHandoff is assistant.todo_handoff's params: an agent is handed work for a todo (POST /api/v1/sessions or
// …/messages with todo_id); Text is what it is handed ("" when only the todo is wanted).
type TodoHandoff struct {
	TodoID string `json:"todo_id"`
	Text   string `json:"text,omitempty"`
}

// TodoHandoffResult is assistant.todo_handoff's answer: the todo (Found false: no such todo) and Text with the todo's
// decisions in effect before it (as it was without any, or when they could not be read).
type TodoHandoffResult struct {
	Found  bool       `json:"found"`
	Title  string     `json:"title,omitempty"`
	DoneAt *time.Time `json:"done_at,omitempty"`
	Text   string     `json:"text,omitempty"`
}

// TodoLinkParams is assistant.todo_link's params: the agent handed work for a todo works for it from now on (a link
// by hand; title, cwd and agent as the session has them).
type TodoLinkParams struct {
	TodoID    string `json:"todo_id"`
	MachineID string `json:"machine_id"`
	SessionID string `json:"session_id"`
	Title     string `json:"title,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
	Agent     string `json:"agent,omitempty"`
}

// TodoSpaceRenamed is todos.space_renamed's params: the hub renamed a workspace (PATCH …/spaces/{wid}); a todo linked
// to it by its name follows.
type TodoSpaceRenamed struct {
	MachineID string `json:"machine_id"`
	Old       string `json:"old"`
	New       string `json:"new"`
}

const (
	// MethodTodoHandoff: TodoHandoff → TodoHandoffResult (the hub calls it before an agent is handed work for a todo).
	MethodTodoHandoff = "assistant.todo_handoff"
	// MethodTodoLink: TodoLinkParams → {} (after the hand-off went through); an error says why it was not linked
	// (db.ErrTodoNotFound / ErrTodoCompleted's words).
	MethodTodoLink = "assistant.todo_link"
	// EventTodoSpaceRenamed: TodoSpaceRenamed, one way.
	EventTodoSpaceRenamed = "todos.space_renamed"
)

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
	EventUserSpoke           = "session.user_spoke"
	EventTurnStarted         = "session.turn_started"
	EventTurnEnded           = "session.turn_ended"
	EventSessionsChanged     = "sessions.changed"
	EventInteractionResolved = "interaction.resolved"
	EventInteractionRaised   = "interaction.raised"
	EventInboxMessage        = "inbox.message"
	EventUpdated             = "manager.updated" // Hello again: hub.json's assistant section or the machines changed
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
	// The todos (the assistant's todos.go, docs/todos.md).
	"GET /api/v1/todos",
	"POST /api/v1/todos",
	"PATCH /api/v1/todos/{id}",
	"DELETE /api/v1/todos/{id}",
	"PUT /api/v1/todos/{id}/spaces/{mid}/{space...}",
	"DELETE /api/v1/todos/{id}/spaces/{mid}/{space...}",
	"PUT /api/v1/todos/{id}/agents/{mid}/{sid}",
	"DELETE /api/v1/todos/{id}/agents/{mid}/{sid}",
	"GET /api/v1/todos/{id}/decisions",
	"POST /api/v1/todos/{id}/decisions",
	"PATCH /api/v1/todos/{id}/decisions/{did}",
	"POST /api/v1/todos/{id}/done-check",
	"POST /api/v1/todos/{id}/retry",
	// Its settings and its main conversation (profile.go, start.go).
	"GET /api/v1/assistant",
	"PUT /api/v1/assistant",
	"POST /api/v1/assistant/warm",
	"GET /api/v1/assistant/memory",
	"PUT /api/v1/assistant/memory/{name}",
	"DELETE /api/v1/assistant/memory/{name}",
	"POST /api/v1/assistant/conversations",
	"POST /api/v1/assistant/conversations/{id}/compact",
	"POST /api/v1/assistant/main/new",
	// The knowledge base (the assistant's kb_routes.go, docs/knowledge.md「App 接口」).
	"GET /api/v1/kb/{kind}",
	"GET /api/v1/kb/{kind}/{id}",
	"PUT /api/v1/kb/{kind}/{id}",
	"GET /api/v1/kb/moments/{id}",
	"GET /api/v1/kb/chats/{id}/context",
	"GET /api/v1/kb/chats/{id}/days/{day}",
	"GET /api/v1/kb-search",
}

// ---- the assistant's settings, and the conversations the hub's features start (docs/assistant.md「和 hub 的边界」) ----

// Profile is the assistant's settings (settings `assistant`, its own) as the hub's own features need them, in every
// snapshot: the Feishu bot and the channels talk to the main conversation on its machine, the model configuration's
// 主对话 runs there, the hub reads times in the user's zone, and the nodes keep listing Keep. The hub keeps the last
// one; before one came in it goes by hub.json's assistant section, its own time zone and no main conversation.
type Profile struct {
	MachineID string `json:"machine_id"` // where its conversations run
	Main      string `json:"main,omitempty"`
	// Launcher is the one it runs on when 模型配置 › 主对话 names none (its own setting, else hub.json's, else claude):
	// that purpose's default.
	Launcher string `json:"launcher"`
	Timezone string `json:"timezone,omitempty"` // the user's (the phone reports it), IANA; "" while not known
	// Keep are the conversations their machines list however long they are quiet: the main one, the main ones it
	// replaced, scheduled tasks' own (node.configure keep_sessions: a busy machine's newest transcripts crowd them out).
	Keep []SessionRef `json:"keep"`
}

// The hub asks the assistant (Handler) for what its own features need of the assistant's conversations.
const (
	// MethodStart: StartParams → {session}: a conversation the hub's features start (the Feishu bot, a channel, the
	// inbox's AI 分身).
	MethodStart = "assistant.start"
	// MethodSendParams: SendParams → {params}: a message for one of its conversations (the phone's, the Feishu bot's
	// and a channel's to the main one, one the node sends by itself) with the launcher and model it runs on, and what
	// the old main conversation left before the text of the first one to a new main conversation started empty.
	MethodSendParams = "assistant.send_params"
	// MethodRename: RenameParams → the node's answer: the user renames one of its conversations in the app (the main
	// one has no title to change, a Codex one only its tab); the new title is the user's from now on.
	MethodRename = "assistant.rename"
)

// The node's methods the assistant calls for its directory, its conversations and its settings
// (docs/protocol.md「hub ↔ node」), passed through by the hub's device.call like the others.
const (
	// DeviceAssistantPrepare: {dir, claude_md, connectors, clis, plugin_skills} → {cwd, plugin_skills_need}.
	DeviceAssistantPrepare = "assistant.prepare"
	// DeviceAssistantWarm: {cwd, launcher, …model} → {}: a process ready for the next new conversation.
	DeviceAssistantWarm = "assistant.warm"
	// DeviceAssistantMemory: {op, cwd, name?, content?} → the memory files (op list) or {}.
	DeviceAssistantMemory = "assistant.memory"
	// DeviceAssistantCompact: {session_id} → {}.
	DeviceAssistantCompact = "assistant.compact"
	// DeviceAssistantHandoff: {session_id} → {summary, recent}: what a main conversation leaves for the next one.
	DeviceAssistantHandoff = "assistant.handoff"
	// DeviceLaunchersList: {} → {launchers}, the node's own.
	DeviceLaunchersList = "launchers.list"
	// DeviceLaunchersProbe: {name} → {detail}.
	DeviceLaunchersProbe = "launchers.probe"
)

// StartParams is assistant.start's params.
type StartParams struct {
	Text           string        `json:"text"`
	Images         []proto.Image `json:"images,omitempty"`
	Launcher       string        `json:"launcher,omitempty"`
	PermissionMode string        `json:"permission_mode,omitempty"`
	Effort         string        `json:"effort,omitempty"`
	Purpose        string        `json:"purpose,omitempty"`    // the model configuration's purpose; "" is 主对话's
	MachineID      string        `json:"machine_id,omitempty"` // "" is the assistant's machine
	// Main: it becomes the main conversation (what the old one left goes before Text).
	Main bool `json:"main,omitempty"`
	// Untitled: not one of the conversations it names and lists as its own (the inbox's AI 分身: its result is the
	// draft in the inbox).
	Untitled bool `json:"untitled,omitempty"`
}

// SendParams is assistant.send_params's params: Params are the node's assistant.send / session.send params.
type SendParams struct {
	MachineID string         `json:"machine_id"`
	SessionID string         `json:"session_id"`
	Params    map[string]any `json:"params"`
}

// RenameParams is assistant.rename's params: Params are the app's ({title?, tab_name?}).
type RenameParams struct {
	MachineID string         `json:"machine_id"`
	SessionID string         `json:"session_id"`
	Params    map[string]any `json:"params"`
}

// CodeBadRequest is what the assistant refuses a request of the hub's with when the app's request itself is wrong
// (the hub answers it 400 with the message).
const CodeBadRequest = "bad_request"

// What the hub's apps tell the assistant for its directory and its settings.
const (
	// ModelsRecord: {purpose} → {binding, revision}: what the purpose itself is set to (an empty binding inherits).
	ModelsRecord = "models.record"
	// ModelsSave: {purpose, revision, binding} → {}: an error when it changed since revision.
	ModelsSave = "models.save"
	// ModelsApplyPicked: {params} → {params}: a launcher (and model) picked for one message, checked and fingerprinted
	// like a configured one; with no model the launcher's own default stays.
	ModelsApplyPicked = "models.apply_picked"
	// ModelsLaunchers: {machine_id} → {launchers}: the machine's launchers as the apps list them (its own but the
	// hidden ones, and the hub's).
	ModelsLaunchers = "models.launchers"
	// ModelsLauncherCheck: {machine_id, launcher, model?} → {available, model_ok}: whether a message can still run on
	// them (not a renamed launcher, not a model its gateway dropped); true when it can't be told.
	ModelsLauncherCheck = "models.launcher_check"

	// ConnectorsSignedIn: {} → {connectors, clis, lark_cli}: the connectors switched on and signed in (rate limited
	// counts), their command lines (name → connector id), and whether hub.json sets up lark-cli.
	ConnectorsSignedIn = "connectors.signed_in"

	// AppPlugins is the hub's plugins (docs/plugins.md「助手」).
	AppPlugins = "plugins"
	// PluginsAssistant: {machine_id} → {instructions, skills}: the plugins' parts of the assistant's instructions, and
	// their skills for that machine (files only with those it was not sent yet).
	PluginsAssistant = "plugins.assistant"
	// PluginsSkillsNeed: {machine_id, names} → {}: the machine's node lacks these skills (its assistant.prepare said
	// plugin_skills_need): their files go next time.
	PluginsSkillsNeed = "plugins.skills_need"

	// SettingsList: {} → {settings: [Setting]}: the hub's own settings jarvis_settings offers; app.tool jarvis_settings
	// runs get / set / add / remove on them (a change waits for the user's approval on the phone).
	SettingsList = "settings.list"
)

// Setting is one of the hub's settings jarvis_settings offers.
type Setting struct {
	Key     string   `json:"key"`
	Title   string   `json:"title"`
	About   string   `json:"about"`
	Value   string   `json:"value,omitempty"` // what set / add take, for the model
	Actions []string `json:"actions"`
}
