package hub

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/bus"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/rpc"
)

// The model configuration is the assistant's (docs/model-config.md「谁管模型」): its records, the providers' keys, every
// model call, the routes of 设置 › 模型供应商 / 模型配置 and 新建 Agent's pickers (assistantwire.Routes, relayed). The
// hub hands over what hub.json says about models (the hello) and what its plugins have (their providers, their voice
// models, which of them need 信息筛选), and asks the assistant only what its plugins need: whether 信息筛选 can be
// called, which launchers have a gateway, and requests to a launcher's gateway sent with its key (models.usable,
// models.launchers, models.launcher_http).

// hubModels is what hub.json says about models, for the hello.
func (h *Hub) hubModels() assistantwire.HubModels {
	out := assistantwire.HubModels{AI: h.cfg.AI, TriageAI: h.cfg.Inbox.TriageAI, JudgeAI: h.cfg.Inbox.JudgeAI, KBAI: h.cfg.KB.AI}
	if m := h.cfg.Inbox.Agent.Machine; h.localMachine(m) != nil {
		out.InboxAgentMachine = m
	}
	for _, m := range h.machinesNow() {
		if len(m.cfg.Launchers) > 0 {
			if out.Launchers == nil {
				out.Launchers = map[string][]assistantwire.ConfiguredLauncher{}
			}
			out.Launchers[m.cfg.ID] = m.cfg.Launchers
		}
	}
	return out
}

// modelsAppCall answers the manager's plugins.presets / plugins.voice / plugins.quota / plugins.need_triage; false for
// the others.
func (h *Hub) modelsAppCall(ctx context.Context, p assistantwire.AppCall) (json.RawMessage, bool, error) {
	if p.AppID != assistantwire.AppPlugins {
		return nil, false, nil
	}
	var out any
	switch p.Method {
	case assistantwire.PluginsPresets:
		out = map[string]any{"presets": h.extensions.launcherPresets(ctx)}
	case assistantwire.PluginsVoice:
		list := []assistantwire.VoicePlugin{}
		if h.extensions != nil {
			for _, x := range h.extensions.list() {
				if x.Kind != "voice" {
					continue
				}
				models, err := h.extensionModelChoices(ctx, "plugin."+x.ID)
				v := assistantwire.VoicePlugin{ID: x.ID, Name: x.Name, Models: models.Models, Default: models.Default}
				if v.Models == nil {
					v.Models = []assistantwire.ModelChoice{}
				}
				if err != nil {
					v.Error = i18n.Localize(err, h.contextLang(ctx))
				}
				list = append(list, v)
			}
		}
		out = map[string]any{"plugins": list}
	case assistantwire.PluginsQuota:
		var a struct {
			Name    string `json:"name"`
			Machine string `json:"machine"`
			BaseURL string `json:"base_url"`
			TZ      string `json:"tz"`
		}
		if err := json.Unmarshal(p.Params, &a); err != nil {
			return nil, true, err
		}
		report, err := h.extensions.quota(ctx, a.BaseURL, a.TZ, h.launcherSender(a.Name, a.Machine))
		if err != nil {
			return nil, true, err
		}
		out = map[string]any{"report": report}
	case assistantwire.PluginsNeedTriage:
		names := h.extensionsNeedingTriage()
		if names == nil {
			names = []string{}
		}
		out = map[string]any{"names": names}
	default:
		return nil, false, nil
	}
	b, err := json.Marshal(out)
	return b, true, err
}

// triageUsable says whether inbox.triage (信息筛选) can be called now, as the assistant resolves it; an error while it
// can't be asked (not connected: assistant_offline).
func (h *Hub) triageUsable(ctx context.Context) (bool, error) {
	var res struct {
		Usable bool `json:"usable"`
	}
	err := h.asst().call(ctx, assistantwire.MethodModelsUsable, map[string]any{"purpose": "inbox.triage"}, &res)
	return res.Usable, err
}

// launcherCall is models.launcher_http: the assistant has the launchers and their keys. Its coded errors come back as
// the hub's (errNoLauncher, errOffline, errNoGateway).
func (h *Hub) launcherCall(ctx context.Context, params map[string]any, out any) error {
	err := h.asst().call(ctx, assistantwire.MethodModelsLauncherHTTP, params, out)
	var re *rpc.Error
	if errors.As(err, &re) {
		switch re.Code {
		case assistantwire.CodeNoLauncher:
			return errNoLauncher
		case assistantwire.CodeMachineOffline:
			return errOffline
		case assistantwire.CodeNoGateway:
			return errNoGateway
		}
	}
	return err
}

// gatewayLaunchers is models.launchers: the launchers with a gateway address, as the assistant has them (names and
// addresses, no key), for the plugins' gateway.launchers. While the assistant is not connected the error is
// assistant_offline; a manager too old to list them is unsupported, not the plugin's unknown method.
func (h *Hub) gatewayLaunchers(ctx context.Context) ([]assistantwire.GatewayLauncher, error) {
	var res struct {
		Launchers []assistantwire.GatewayLauncher `json:"launchers"`
	}
	err := h.asst().call(ctx, assistantwire.MethodModelsLaunchers, map[string]any{}, &res)
	var re *rpc.Error
	if errors.As(err, &re) && re.Code == bus.CodeUnknownMethod {
		return nil, &rpc.Error{Code: extension.CodeUnsupported, Message: "the manager is too old to list the launchers; update it"}
	}
	return res.Launchers, err
}

// isAssistantOffline: err is the assistant not connected (assistantRemote.call's assistant_offline).
func isAssistantOffline(err error) bool {
	var re *rpc.Error
	return errors.As(err, &re) && re.Code == assistantwire.CodeAssistantOffline
}
