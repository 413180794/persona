package hub

import (
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/nwverse/persona/server/internal/extension"
)

// An extension's token (docs/extensions.md「权限」): it opens the routes of the scopes the user granted the extension
// (permissions.hub) and no other. The hub makes one each time it starts the extension and drops the one before; it
// drops them all when the extension is switched off, upgraded, uninstalled or reconfigured (apply). They are kept in
// memory only: an extension's process is the hub's child and starts again with a new hub (docs/hub-restart.md), so a
// token never has to outlive the hub that made it.

// roleExtension is an extension's token.
const roleExtension = "extension"

// extTokenPrefix starts every extension's token, so authenticate knows it without a query.
const extTokenPrefix = "pext_"

type extToken struct {
	extension string
	name      string
	scopes    []string
}

// extTokens are the extensions' live tokens.
type extTokens struct {
	mu     sync.Mutex
	byHash map[string]extToken // token hash → whose
	byExt  map[string]string   // extension id → its token's hash
}

// issue makes id a new token of scopes, dropping the one it had.
func (t *extTokens) issue(id, name string, scopes []string) string {
	tok := extTokenPrefix + randomToken()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.byHash == nil {
		t.byHash, t.byExt = map[string]extToken{}, map[string]string{}
	}
	if old, ok := t.byExt[id]; ok {
		delete(t.byHash, old)
	}
	hash := hashToken(tok)
	t.byHash[hash] = extToken{extension: id, name: name, scopes: slices.Clone(scopes)}
	t.byExt[id] = hash
	return tok
}

// revoke drops id's token.
func (t *extTokens) revoke(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if hash, ok := t.byExt[id]; ok {
		delete(t.byHash, hash)
		delete(t.byExt, id)
	}
}

// lookup is the principal of tok, an extension's token.
func (t *extTokens) lookup(tok string) (principal, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.byHash[hashToken(tok)]
	if !ok {
		return principal{}, false
	}
	return principal{ID: "extension:" + e.extension, Name: e.name, Role: roleExtension, Extension: e.extension,
		Scopes: e.scopes}, true
}

// routeScopes are the routes an extension's token may call, by the pattern they are registered with, and the scope
// each needs. A route not here is for full credentials only: deciding that a new route is open to extensions means
// adding it here (TestRouteScopes keeps the list of every route and what it was decided).
var routeScopes = map[string]string{
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
	"POST /api/v1/inbox/{id}/done":                       extension.ScopeInboxWrite,
	"POST /api/v1/inbox/{id}/draft":                      extension.ScopeInboxWrite,
	"POST /api/v1/inbox/{id}/reply":                      extension.ScopeInboxWrite,
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
	"POST /api/v1/agents/sessions/{id}/messages":         extension.ScopeAgentsControl,
	"POST /api/v1/agents/sessions/{id}/read":             extension.ScopeAgentsControl,
	"GET /api/v1/machines":                               extension.ScopeMachinesRead,
	"GET /api/v1/machines/{id}/metrics":                  extension.ScopeMachinesRead,
	"GET /api/v1/machines/{mid}/stat":                    extension.ScopeMachinesRead,
	"POST /api/v1/notify":                                extension.ScopeNotify,
	"GET /api/v1/assistant":                              extension.ScopeAssistantAsk,
	"POST /api/v1/assistant/conversations":               extension.ScopeAssistantAsk,
	"GET /api/v1/assistant/conversations/{id}/feed":      extension.ScopeAssistantAsk,
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
	// Another extension's own HTTP: the scope names which (extensionCallScope).
	"/api/v1/extensions/{id}/": extension.ScopeCallPrefix,
}

// extensionCallScope is the scope a request to an extension's own HTTP needs (extensions.call:<id>); "" for the hub's
// routes under /api/v1/extensions/<id>/-/, which are no extension's to open.
func extensionCallScope(path string) string {
	rest, _ := strings.CutPrefix(path, "/api/v1/extensions/")
	id, after, _ := strings.Cut(rest, "/")
	if id == "" || id == "-" || after == "-" || strings.HasPrefix(after, "-/") {
		return ""
	}
	return extension.ScopeCallPrefix + id
}

// extensionMay says whether p, an extension's token, may make request r, and the scope it lacks when not ("" for a
// route open to full credentials only).
func (h *Hub) extensionMay(p principal, r *http.Request) (bool, string) {
	mux := h.apiMux
	if mux == nil {
		return false, ""
	}
	_, pattern := mux.Handler(r)
	scope, ok := routeScopes[pattern]
	if !ok {
		return false, ""
	}
	if scope == extension.ScopeCallPrefix {
		if scope = extensionCallScope(r.URL.Path); scope == "" {
			return false, ""
		}
	}
	return slices.Contains(p.Scopes, scope), scope
}

// answerOnly says whether the request comes from an answer-only device (a watch): what such a device sends is held to
// a card's own choices and plain text. A full credential and an extension granted the route's scope are not.
func answerOnly(r *http.Request) bool { return principalOf(r.Context()).Role == roleAnswer }

// startToken is an extension's token for the run of its program: made when the hub starts the program (its
// environment), handed on in initialize.
type startToken struct {
	mu  sync.Mutex
	tok string
}

// renew makes p a new token of the scopes it was granted, dropping the one before.
func (t *startToken) renew(ps *extensions, p *extension.Extension) string {
	tok := ps.h.extTokens.issue(p.Record.ID, p.Manifest.Name, p.Record.Granted.Hub)
	t.mu.Lock()
	t.tok = tok
	t.mu.Unlock()
	return tok
}

func (t *startToken) current() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.tok
}
