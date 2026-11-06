package extwire

import (
	"net/url"
	"strings"

	"github.com/nwverse/persona/server/internal/extension"
)

// An extension's token (docs/extensions.md "Permissions") opens the routes of the scopes the user granted the extension and
// no other; the manager made the token and checks it (extensions.authorize), with this table of the hub's routes.

// RouteScopes are the routes an extension's token may call, by the pattern they are registered with, and the scope
// each needs. A route not here is for full credentials only: deciding that a new route is open to extensions means
// adding it here (TestRouteScopes keeps the list of every route and what it was decided).
var RouteScopes = map[string]string{
	"GET /api/v1/kb/{kind}":                              extension.ScopeKBRead,
	"GET /api/v1/kb/{kind}/{id}":                         extension.ScopeKBRead,
	"GET /api/v1/kb/chats/{id}/context":                  extension.ScopeKBRead,
	"GET /api/v1/kb/chats/{id}/days/{day}":               extension.ScopeKBRead,
	"GET /api/v1/kb/moments/{id}":                        extension.ScopeKBRead,
	"GET /api/v1/kb-search":                              extension.ScopeKBRead,
	"PUT /api/v1/kb/{kind}/{id}":                         extension.ScopeKBWrite,
	"GET /api/v1/inbox":                                  extension.ScopeInboxRead,
	"GET /api/v1/inbox/{id}":                             extension.ScopeInboxRead,
	"GET /api/v1/inbox/mutes":                            extension.ScopeInboxRead,
	"POST /api/v1/inbox/done":                            extension.ScopeInboxWrite,
	"POST /api/v1/inbox/bulk":                            extension.ScopeInboxWrite,
	"POST /api/v1/inbox/{id}/done":                       extension.ScopeInboxWrite,
	"POST /api/v1/inbox/{id}/draft":                      extension.ScopeInboxWrite,
	"POST /api/v1/inbox/{id}/reply":                      extension.ScopeInboxWrite,
	"POST /api/v1/inbox/{id}/suggestion":                 extension.ScopeInboxWrite,
	"POST /api/v1/inbox/{id}/doc/status":                 extension.ScopeInboxWrite,
	"PUT /api/v1/inbox/{id}/parent":                      extension.ScopeInboxWrite,
	"GET /api/v1/inbox/tree/{kind}/{id...}":              extension.ScopeInboxRead,
	"POST /api/v1/inbox/mutes":                           extension.ScopeInboxWrite,
	"DELETE /api/v1/inbox/mutes/{id}":                    extension.ScopeInboxWrite,
	"POST /api/v1/inbox/refresh":                         extension.ScopeInboxWrite,
	"GET /api/v1/sessions":                               extension.ScopeAgentsRead,
	"GET /api/v1/sessions/{mid}/{sid}/info":              extension.ScopeAgentsRead,
	"GET /api/v1/sessions/{mid}/{sid}/items":             extension.ScopeAgentsRead,
	"GET /api/v1/sessions/{mid}/{sid}/items/{iid}/full":  extension.ScopeAgentsRead,
	"GET /api/v1/sessions/{mid}/{sid}/items/{iid}/image": extension.ScopeAgentsRead,
	"GET /api/v1/sessions/{mid}/{sid}/screen":            extension.ScopeAgentsRead,
	"GET /api/v1/interactions":                           extension.ScopeAgentsRead,
	"GET /api/v1/interactions/history":                   extension.ScopeAgentsRead,
	"GET /api/v1/agents":                                 extension.ScopeAgentsRead,
	"GET /api/v1/agents/sessions":                        extension.ScopeAgentsRead,
	"GET /api/v1/agents/sessions/{id}":                   extension.ScopeAgentsRead,
	"GET /api/v1/agents/sessions/{id}/messages":          extension.ScopeAgentsRead,
	"GET /api/v1/agents/sessions/{id}/avatar":            extension.ScopeAgentsRead,
	"GET /api/v1/workspaces":                             extension.ScopeAgentsRead,
	"GET /api/v1/workspace-board":                        extension.ScopeAgentsRead,
	"POST /api/v1/sessions":                              extension.ScopeAgentsControl,
	"POST /api/v1/sessions/{mid}/{sid}/messages":         extension.ScopeAgentsControl,
	"POST /api/v1/sessions/{mid}/{sid}/interrupt":        extension.ScopeAgentsControl,
	"POST /api/v1/sessions/{mid}/{sid}/queue":            extension.ScopeAgentsControl,
	"POST /api/v1/sessions/{mid}/{sid}/seen":             extension.ScopeAgentsControl,
	"POST /api/v1/interactions/{id}/resolve":             extension.ScopeAgentsControl,
	"POST /api/v1/interactions/{id}/reanswer":            extension.ScopeAgentsControl, // "Change answer": a message to the agent
	"POST /api/v1/agents/sessions/{id}/messages":         extension.ScopeAgentsControl,
	"POST /api/v1/agents/sessions/{id}/read":             extension.ScopeAgentsControl,
	"GET /api/v1/machines":                               extension.ScopeMachinesRead,
	"GET /api/v1/machines/{id}/metrics":                  extension.ScopeMachinesRead,
	"GET /api/v1/machines/{mid}/stat":                    extension.ScopeMachinesRead,
	"POST /api/v1/notify":                                extension.ScopeNotify,
	"GET /api/v1/assistant":                              extension.ScopeAssistantAsk,
	"POST /api/v1/assistant/conversations":               extension.ScopeAssistantAsk,
	"GET /api/v1/assistant/conversations/{id}/feed":      extension.ScopeAssistantAsk,
	"POST /api/v1/assistant/conversations/{id}/stop":     extension.ScopeAssistantAsk, // /agent's /stop (#3308)
	"GET /api/v1/assistant/reporters":                    extension.ScopeAssistantAsk,
	// The todos (the assistant's, relayed): reading them, and adding, editing and attaching agents and spaces. Deleting
	// a todo, its decisions, the done check and a retry stay with full credentials.
	"GET /api/v1/todos":                                 extension.ScopeTodosRead,
	"GET /api/v1/todos/{id}/decisions":                  extension.ScopeTodosRead,
	"POST /api/v1/todos":                                extension.ScopeTodosWrite,
	"PATCH /api/v1/todos/{id}":                          extension.ScopeTodosWrite,
	"PUT /api/v1/todos/{id}/agents/{mid}/{sid}":         extension.ScopeTodosWrite,
	"DELETE /api/v1/todos/{id}/agents/{mid}/{sid}":      extension.ScopeTodosWrite,
	"PUT /api/v1/todos/{id}/spaces/{mid}/{space...}":    extension.ScopeTodosWrite,
	"DELETE /api/v1/todos/{id}/spaces/{mid}/{space...}": extension.ScopeTodosWrite,
	// Another extension's own HTTP: the scope names which (ExtensionCallScope).
	"/api/v1/extensions/{id}/": extension.ScopeCallPrefix,
}

// ExtensionCallScope is the scope a request to an extension's own HTTP needs (extensions.call:<id>); "" for the hub's
// routes under /api/v1/extensions/<id>/-/, which are no extension's to open. path is the escaped path, the one the
// manager's mux routes the request by: its segments are read as the mux reads them (each one unescaped on its own), so
// an escaped "-" is the hub's "-" and an escaped "/" stays inside the id.
func ExtensionCallScope(path string) string {
	rest, ok := strings.CutPrefix(path, "/api/v1/extensions/")
	if !ok {
		return ""
	}
	rawID, after, _ := strings.Cut(rest, "/")
	rawNext, _, _ := strings.Cut(after, "/")
	id, err := url.PathUnescape(rawID)
	if err != nil {
		return ""
	}
	next, err := url.PathUnescape(rawNext)
	if err != nil || id == "" || id == "-" || next == "-" {
		return ""
	}
	return extension.ScopeCallPrefix + id
}
