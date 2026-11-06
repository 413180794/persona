package extmgr

import (
	"net/http"
	"slices"
	"sync"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/extwire"
)

// An extension's token (docs/extensions.md "Permissions"): it opens the routes of the scopes the user granted the
// extension (permissions.hub) and no other. The hub makes one each time it starts the extension and drops the one
// before; it drops them all when the extension is switched off, upgraded, uninstalled or reconfigured (apply). They are
// kept in memory only: an extension's process is the hub's child and starts again with a new hub (docs/hub-restart.md),
// so a token never has to outlive the hub that made it.

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

// scopeOpens says whether scopes open the route of pattern (the hub's mux pattern, or the manager's for a relayed one)
// at path, and the scope it needs ("" for a route open to full credentials only). The table is shared with the hub
// (extwire.RouteScopes: TestRouteScopes keeps the list of every route and what it was decided).
func scopeOpens(scopes []string, pattern, path string) (bool, string) {
	scope, ok := extwire.RouteScopes[pattern]
	if !ok {
		return false, ""
	}
	if scope == extension.ScopeCallPrefix {
		if scope = extwire.ExtensionCallScope(path); scope == "" {
			return false, ""
		}
	}
	return slices.Contains(scopes, scope), scope
}

// lookupHash is lookup by the token's hash (the hub asks with it: extensions.authorize).
func (t *extTokens) lookupHash(hash string) (principal, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.byHash[hash]
	if !ok {
		return principal{}, false
	}
	return principal{ID: "extension:" + e.extension, Name: e.name, Role: roleExtension, Extension: e.extension,
		Scopes: e.scopes}, true
}

// answerOnly says whether the request comes from an answer-only device (a watch): what such a device sends is held to
// a card's own choices and plain text. A full credential and an extension granted the route's scope are not.
func answerOnly(r *http.Request) bool { return principalOf(r.Context()).Role == roleAnswer }

var _ = http.MethodGet

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
