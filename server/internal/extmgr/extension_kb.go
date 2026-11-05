package extmgr

import (
	"context"
	"encoding/json"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/rpc"
)

// extensionKBCall exposes only the public knowledge-base methods. Internal kb.* calls (notably kb.forget and
// kb.posts_*) never pass this allowlist, even with the old full-hub grant.
func (ps *extensions) extensionKBCall(ctx context.Context, id, method string, params json.RawMessage) (any, error) {
	var scope string
	switch method {
	case assistantwire.MethodKBList, assistantwire.MethodKBGet, assistantwire.MethodKBDay, assistantwire.MethodKBSearch, assistantwire.MethodKBOwner:
		scope = extension.ScopeKBRead
	case assistantwire.MethodKBUpdate:
		scope = extension.ScopeKBWrite
	default:
		return nil, &rpc.Error{Code: extension.CodeUnknownMethod, Message: "unknown method " + method}
	}
	if ps.reg == nil || !ps.reg.HubScopeAllowed(id, scope) {
		data, _ := json.Marshal(map[string]string{"scope": scope})
		return nil, &rpc.Error{Code: extension.CodePermissionDenied, Message: method + ": requires " + scope, Data: data}
	}
	if ps.h == nil {
		return nil, &rpc.Error{Code: assistantwire.CodeAssistantOffline, Message: errAssistantOffline.Error()}
	}
	var out json.RawMessage
	if err := ps.h.kbAsk(ctx, method, params, &out); err != nil {
		return nil, err
	}
	return out, nil
}
