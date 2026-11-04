// Package assistant is the Persona tab's assistant (docs/assistant.md), a process of its own (`personad assistant`):
// its conversations (titles, archive, messages between them), what it does on its own (scheduled tasks), the tasks
// it hands out (plans, the conversation's feed, receipts, the 今天 card), its MCP tools and the live context each
// message starts with. Its runtime (the headless Claude / Codex) is a node's (node/assistant*.go); the hub is only
// the channel between it and the apps.
//
// The package never imports the hub. It reaches the hub through Hub alone, whose methods are bus methods
// (docs/protocol.md「总线」, the shapes in internal/assistantwire), and the hub reaches it through Handler
// (handler.go). BusHub (bus.go) is both on the bus connection; Main (main.go) is the process.

package assistant

import (
	"context"
	"encoding/json"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

// Hub is everything the assistant asks of the hub.
type Hub interface {
	// Hello is manager.hello's answer: the owner's profile and, for now, where the hub runs the assistant's
	// conversations. BusHub keeps the last one and takes manager.updated.
	Hello(ctx context.Context) (Hello, error)
	// DeviceCall is device.call {device, method, params}: a call to one of the user's machines (DeviceMethod*).
	DeviceCall(ctx context.Context, p DeviceCall) (json.RawMessage, error)
	// AppCall is app.call {app_id, method, params}: a call to an app the hub runs (App*).
	AppCall(ctx context.Context, p AppCall) (json.RawMessage, error)
	// NotifyRequest is notify.request {notification} → {ok}: a push for the user (nil error is ok).
	NotifyRequest(ctx context.Context, p NotifyRequest) error
	// Broadcast is ws.broadcast {message}: a live update the apps get as it is (assistant_schedule, assistant_feed,
	// task_plan). One way: nothing comes back.
	Broadcast(ctx context.Context, b Broadcast)
	// Snapshot is manager.snapshot: what the assistant reports for the apps. The hub caches the latest and shows it.
	Snapshot(ctx context.Context, s Snapshot) (SnapshotResult, error)
}

// What crosses the bus is assistantwire's; these names keep the package's code as it was.
type (
	Hello               = assistantwire.Hello
	Placement           = assistantwire.Placement
	DeviceCall          = assistantwire.DeviceCall
	Device              = assistantwire.Device
	AppCall             = assistantwire.AppCall
	Todo                = assistantwire.Todo
	TodoAgent           = assistantwire.TodoAgent
	ModelBinding        = assistantwire.ModelBinding
	NotifyRequest       = assistantwire.NotifyRequest
	Notification        = assistantwire.Notification
	Broadcast           = assistantwire.Broadcast
	Snapshot            = assistantwire.Snapshot
	TaskView            = assistantwire.TaskView
	SessionView         = assistantwire.SessionView
	SnapshotResult      = assistantwire.SnapshotResult
	ToolCall            = assistantwire.ToolCall
	SessionRef          = assistantwire.SessionRef
	ConversationStarted = assistantwire.ConversationStarted
	ConversationRenamed = assistantwire.ConversationRenamed
	ContextParams       = assistantwire.ContextParams
	ContextResult       = assistantwire.ContextResult
	Action              = assistantwire.Action
	ActionResult        = assistantwire.ActionResult
	InteractionResolved = assistantwire.InteractionResolved
	InboxMessage        = assistantwire.InboxMessage
	MessageCard         = assistantwire.MessageCard
	CardSentParams      = assistantwire.CardSentParams
	CardsSentParams     = assistantwire.CardsSentParams
	DeviceSessions      = assistantwire.DeviceSessions
	HTTPRequest         = assistantwire.HTTPRequest
	HTTPResponse        = assistantwire.HTTPResponse
)

const (
	DeviceSessionItems       = assistantwire.DeviceSessionItems
	DeviceSessionRename      = assistantwire.DeviceSessionRename
	DeviceAssistantSend      = assistantwire.DeviceAssistantSend
	DeviceNodeConfigure      = assistantwire.DeviceNodeConfigure
	DeviceSessionSend        = assistantwire.DeviceSessionSend
	DeviceSessionStart       = assistantwire.DeviceSessionStart
	DeviceDevicesList        = assistantwire.DeviceDevicesList
	DeviceInteractionsList   = assistantwire.DeviceInteractionsList
	DeviceSessionsList       = assistantwire.DeviceSessionsList
	DeviceSessionGet         = assistantwire.DeviceSessionGet
	DeviceAssistantStart     = assistantwire.DeviceAssistantStart
	AppModels                = assistantwire.AppModels
	ModelsComplete           = assistantwire.ModelsComplete
	ModelsApply              = assistantwire.ModelsApply
	ModelsResolve            = assistantwire.ModelsResolve
	ModelsJSON               = assistantwire.ModelsJSON
	ModelsTabName            = assistantwire.ModelsTabName
	AppKB                    = assistantwire.AppKB
	KBStatus                 = assistantwire.KBStatus
	KBPrompt                 = assistantwire.KBPrompt
	ModelsAssistantSend      = assistantwire.ModelsAssistantSend
	AppConnectors            = assistantwire.AppConnectors
	ConnectorsSnapshots      = assistantwire.ConnectorsSnapshots
	AppInbox                 = assistantwire.AppInbox
	AppInteractions          = assistantwire.AppInteractions
	InboxReplied             = assistantwire.InboxReplied
	AppSettings              = assistantwire.AppSettings
	AppTool                  = assistantwire.AppTool
	AppTodos                 = assistantwire.AppTodos
	TodosLoad                = assistantwire.TodosLoad
	TodosCreate              = assistantwire.TodosCreate
	TodosLinkAgent           = assistantwire.TodosLinkAgent
	TodosFail                = assistantwire.TodosFail
	TodosAutoDone            = assistantwire.TodosAutoDone
	TodosGet                 = assistantwire.TodosGet
	TodosList                = assistantwire.TodosList
	TodosOfSession           = assistantwire.TodosOfSession
	TodosRetry               = assistantwire.TodosRetry
	TodosView                = assistantwire.TodosView
	MethodHello              = assistantwire.MethodHello
	MethodDevice             = assistantwire.MethodDevice
	MethodApp                = assistantwire.MethodApp
	MethodNotify             = assistantwire.MethodNotify
	MethodSnapshot           = assistantwire.MethodSnapshot
	EventBroadcast           = assistantwire.EventBroadcast
	MethodHTTP               = assistantwire.MethodHTTP
	MethodTools              = assistantwire.MethodTools
	MethodTool               = assistantwire.MethodTool
	MethodContext            = assistantwire.MethodContext
	MethodAction             = assistantwire.MethodAction
	MethodTasks              = assistantwire.MethodTasks
	EventConversationStarted = assistantwire.EventConversationStarted
	EventConversationRenamed = assistantwire.EventConversationRenamed
	EventUserSpoke           = assistantwire.EventUserSpoke
	EventTurnStarted         = assistantwire.EventTurnStarted
	EventTurnEnded           = assistantwire.EventTurnEnded
	EventSessionsChanged     = assistantwire.EventSessionsChanged
	EventInteractionResolved = assistantwire.EventInteractionResolved
	EventInboxMessage        = assistantwire.EventInboxMessage
	EventUpdated             = assistantwire.EventUpdated
	EventResync              = assistantwire.EventResync
	CodeNoDevice             = assistantwire.CodeNoDevice
	CodeMachineOffline       = assistantwire.CodeMachineOffline
	CodeStaleSnapshot        = assistantwire.CodeStaleSnapshot
	CodeModelPaused          = assistantwire.CodeModelPaused
	CodeAssistantOffline     = assistantwire.CodeAssistantOffline
	DeviceAll                = assistantwire.DeviceAll
	NotifyPush               = assistantwire.NotifyPush
	RoleHeader               = assistantwire.RoleHeader
	AssistantID              = assistantwire.AssistantID
)

var (
	ErrNoDevice      = assistantwire.ErrNoDevice
	ErrStaleSnapshot = assistantwire.ErrStaleSnapshot
	Routes           = assistantwire.Routes
)
