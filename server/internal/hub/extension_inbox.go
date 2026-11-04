package hub

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/extension"
)

// The inbox's common rules for plugins (#1730, docs/plugins.md「收件箱的通用规则」). A plugin connector with an inbox and
// no fallback of its own (capability fallback) leaves sorting to the hub's model (inbox.triage, 信息筛选): its new
// items without a tier are not let in while the model cannot sort them (inbox_admit.go). So such a plugin is not
// switched on while that model cannot be called, and the model is not cleared while one is on. A hub.json plugin
// cannot be switched: it runs, but its inbox is not collected until the model is there.

// errNeedsTriageModel refuses to switch on (or install, or upgrade while on) a plugin that needs the triage model.
var errNeedsTriageModel = errors.New("先在 设置 › 模型 里配好『信息筛选』，再启用这个插件")

// needsTriageModelSetup is what 设置 › 插件 says of a hub.json plugin waiting for the model (needs_setup).
const needsTriageModelSetup = "需要先配分拣模型"

// modelUsable says whether a binding of an API use can be called: it resolves to an address, a model and a key.
func (h *Hub) modelUsable(ctx context.Context, b ModelBinding) bool {
	c, err := h.apiConfig(ctx, b)
	return err == nil && c.Configured()
}

// triageUsable says whether inbox.triage (信息筛选) can be called now: what it resolves to, set here, hub.json's
// "ai" / inbox.triage_ai, or the managed connection picked for it when neither is set.
func (h *Hub) triageUsable(ctx context.Context) bool {
	b, err := h.resolveModel(ctx, "inbox.triage")
	return err == nil && h.modelUsable(ctx, b)
}

// canEnablePlugin is the registry's CanEnable: a plugin with an inbox connector without a fallback needs the triage
// model. Built-in plugins are the hub's own sources (#1690's line) and keep their way.
func (h *Hub) canEnableExtension(ctx context.Context, source string, m *extension.Manifest) error {
	if source == extension.SourceBuiltin {
		return nil
	}
	if _, needs := extension.InboxConnectors(m); len(needs) == 0 || h.triageUsable(ctx) {
		return nil
	}
	return errNeedsTriageModel
}

// pluginsNeedingTriage are the names of the plugins that are on and need the triage model: installed ones whose
// inbox connector has no fallback (a hub.json one just waits for the model; a built-in one has its fallbacks).
func (h *Hub) extensionsNeedingTriage() []string {
	if h.extensions == nil {
		return nil
	}
	var names []string
	for _, p := range h.extensions.reg.List() {
		if !p.Record.Enabled || p.Record.Source != extension.SourceInstalled {
			continue
		}
		if _, needs := extension.InboxConnectors(p.Manifest); len(needs) > 0 {
			name := p.Record.ID
			if p.Manifest != nil && p.Manifest.Name != "" {
				name = p.Manifest.Name
			}
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// waitsForTriageModel says whether a plugin connector's inbox is not collected now: a hub.json plugin's without a
// fallback while the triage model cannot be called.
func (h *Hub) waitsForTriageModel(ctx context.Context, c connector.Connector) bool {
	return extensionSource(c.ID()) == extension.SourceHubJSON && c.Manifest().Has(connector.CapInbox) &&
		!c.Manifest().Has(connector.CapFallback) && !h.triageUsable(ctx)
}

// hubJSONWaitsForTriageModel is waitsForTriageModel for a plugin (its status in 设置 › 插件).
func (h *Hub) hubJSONWaitsForTriageModel(ctx context.Context, p *extension.Extension) bool {
	if p.Record.Source != extension.SourceHubJSON {
		return false
	}
	_, needs := extension.InboxConnectors(p.Manifest)
	return len(needs) > 0 && !h.triageUsable(ctx)
}

// writePluginErr answers a refused plugin change: 409 needs_triage_model when the triage model is missing, else
// status with code.
func writeExtensionErr(w http.ResponseWriter, status int, code string, err error) {
	if errors.Is(err, errNeedsTriageModel) {
		writeErrCode(w, http.StatusConflict, "needs_triage_model", err)
		return
	}
	writeErrCode(w, status, code, err)
}

// triageInUse refuses a change that leaves inbox.triage uncallable while plugins that need it are on: 422
// triage_model_in_use, naming them (not 409: the apps read a 409 from model-config as an edit conflict and would hide
// the message; a 422 they show as it is). nil when the change may go.
func (h *Hub) triageInUse(ctx context.Context, id string, effective ModelBinding) map[string]any {
	if id != "inbox.triage" || h.modelUsable(ctx, effective) {
		return nil
	}
	names := h.extensionsNeedingTriage()
	if len(names) == 0 {
		return nil
	}
	return map[string]any{"code": "triage_model_in_use", "plugins": names,
		"error": "这些插件靠『信息筛选』分拣收件箱：" + strings.Join(names, "、") + "。先在 设置 › 插件 里停用它们，再改掉这个模型"}
}
