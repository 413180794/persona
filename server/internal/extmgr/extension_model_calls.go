package extmgr

import (
	"context"
	"encoding/json"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Model calls by extensions (#6102, docs/extensions/extensions.md "Model calls"): an extension the user granted the models
// scope calls model.complete; it goes to the assistant, which holds the keys, with the extension's id (which the
// extension cannot set), and runs on the extension's purpose extension.<id> in Settings › Models. The legacy
// full-hub grant does not include it. No budget and no cap (the user's decision).

// extensionModelCall is model.complete from extension id.
func (ps *extensions) extensionModelCall(ctx context.Context, id string, params json.RawMessage) (any, error) {
	if ps.reg == nil || !ps.reg.HubScopeGranted(id, extension.ScopeModels) {
		data, _ := json.Marshal(map[string]string{"scope": extension.ScopeModels})
		return nil, &rpc.Error{Code: extension.CodePermissionDenied, Message: assistantwire.MethodModelComplete + ": requires " + extension.ScopeModels, Data: data}
	}
	if ps.h == nil {
		return nil, &rpc.Error{Code: assistantwire.CodeAssistantOffline, Message: errAssistantOffline.Error()}
	}
	var out json.RawMessage
	if err := ps.h.kbAsk(ctx, assistantwire.MethodModelsExtensionComplete, assistantwire.ExtensionComplete{Extension: id, Params: params}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// modelExtensions are the extensions switched on and granted models: each has its own purpose in Settings › Models
// (plugins.models).
func (ps *extensions) modelExtensions() []assistantwire.ModelExtension {
	out := []assistantwire.ModelExtension{}
	if ps == nil || ps.reg == nil {
		return out
	}
	for _, p := range ps.reg.List() {
		if p.Full == nil || !ps.reg.HubScopeGranted(p.Record.ID, extension.ScopeModels) {
			continue
		}
		out = append(out, assistantwire.ModelExtension{ID: p.Record.ID, Name: p.Full.Name})
	}
	return out
}
