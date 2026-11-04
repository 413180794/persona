package assistant

import (
	"context"
	"net/http"

	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// Handler is what the hub asks of the assistant, the bus's other direction: the apps' requests it relays as they
// came, the tool calls of the assistant's conversations, and what it sees happen to sessions. Service implements
// it; each method is one message, named in its comment, that BusHub takes off the bus (bus.go).
type Handler interface {
	// ServeHTTP answers the apps' requests on Routes(), relayed unchanged (the caller's role in RoleHeader).
	http.Handler
	// Routes are the HTTP routes (net/http patterns) the hub relays to ServeHTTP: assistantwire.Routes, the list the
	// hub has (TestRoutesAreTheWireOnes).
	Routes() []string
	// Tools is assistant.tools: the MCP tools only the assistant's conversations get (the hub lists them with its
	// own, and sends the calls of these to Tool).
	Tools(ctx context.Context) []proto.Tool
	// Tool is assistant.tool: a conversation called one of the assistant's MCP tools.
	Tool(ctx context.Context, call ToolCall) proto.ToolResult
	// Context is assistant.context: the live context a message to conversation p.SessionID starts with (the node
	// asks for it on every message).
	Context(ctx context.Context, p ContextParams) ContextResult
	// Action is manager.action: the user answered one of the questions the assistant asked (Snapshot.Interactions).
	Action(ctx context.Context, a Action) (ActionResult, error)
	// ActiveTasks is assistant.tasks: the tasks going on, for the inbox's judge and AI 分身 to link messages to.
	ActiveTasks(ctx context.Context) []prompts.InboxTask
	// CardSent is assistant.card_sent: a message card says its reply went out (the inbox's 发送 on a card sends once).
	CardSent(ctx context.Context, p CardSentParams) (bool, error)
	// CardsSent is assistant.cards_sent: the reply to an inbox item went out; the cards about it say so from now on.
	CardsSent(ctx context.Context, p CardsSentParams)
	// ConversationStarted is conversation.started: the hub started a conversation (the app's new conversation, the
	// Feishu bot); it is named after its first turn.
	ConversationStarted(ctx context.Context, c ConversationStarted)
	// ConversationRenamed is conversation.renamed: the user renamed a conversation in the app.
	ConversationRenamed(ctx context.Context, c ConversationRenamed)
	// UserSpoke is session.user_spoke: the user sent a message to a conversation.
	UserSpoke(ctx context.Context, s SessionRef)
	// TurnStarted is session.turn_started: a session on a machine started a turn.
	TurnStarted(ctx context.Context, s SessionRef)
	// TurnEnded is session.turn_ended: a session on a machine finished a turn.
	TurnEnded(ctx context.Context, s SessionRef)
	// InteractionResolved is interaction.resolved: the user dealt with an agent's card (any agent's).
	InteractionResolved(ctx context.Context, r InteractionResolved)
	// InboxMessage is inbox.message: the inbox's AI 分身 drafted a reply to a message about one of the user's tasks.
	InboxMessage(ctx context.Context, m InboxMessage)
	// ProfileChanged is manager.updated: the owner's profile changed (the time zone): what was read in the old one goes.
	ProfileChanged(ctx context.Context)
	// SessionsChanged is sessions.changed: a machine's node sent its session list.
	SessionsChanged(ctx context.Context, d DeviceSessions)
	// Resync is manager.resync: report the whole snapshot again (the hub has none, or a partial one).
	Resync(ctx context.Context)
}

// roleFull is a device that may do everything (an answer-only watch may not).
const roleFull = "full"
