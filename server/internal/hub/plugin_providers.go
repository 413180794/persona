package hub

import (
	"cmp"
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/plugin"
	"github.com/nwverse/persona/server/internal/proto"
)

// The providers extension point (docs/plugins.md「模型供应商」): a plugin declares model providers — address, kind,
// models — and 设置 › 模型供应商 offers them when a launcher is added: the user picks one and fills in only the name and
// the key (POST /api/v1/launchers with preset). A provider whose plugin lists its models itself (dynamic_models) is
// asked provider.models.

// LauncherPreset is one plugin's provider as the app offers it.
type LauncherPreset struct {
	ID          string                 `json:"id"` // <plugin>/<provider>
	Plugin      string                 `json:"plugin"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Kind        string                 `json:"kind"`
	API         string                 `json:"api,omitempty"`
	BaseURL     string                 `json:"base_url"`
	Model       string                 `json:"model,omitempty"`
	SmallModel  string                 `json:"small_model,omitempty"`
	Models      []plugin.ProviderModel `json:"models,omitempty"`
	KeyHint     string                 `json:"key_hint,omitempty"`
	// ModelsError says why the plugin's own list could not be had (the declared models are still there).
	ModelsError string `json:"models_error,omitempty"`
}

// launcherPresets are the providers of every plugin switched on and able to run.
func (ps *plugins) launcherPresets(ctx context.Context) []LauncherPreset {
	out := []LauncherPreset{}
	if ps == nil {
		return out
	}
	for _, p := range ps.reg.List() {
		if !p.Record.Enabled || p.Problem != "" {
			continue
		}
		for _, it := range p.Manifest.Providers() {
			kind := it.Kind
			if kind == "" {
				kind = "launcher"
			}
			pr := LauncherPreset{ID: p.Record.ID + "/" + it.ID, Plugin: p.Record.ID, Name: it.Name, Description: it.Description, Kind: kind,
				API: it.API, BaseURL: it.BaseURL, Model: it.Model, SmallModel: it.SmallModel, Models: it.Models, KeyHint: it.KeyHint}
			if it.DynamicModels {
				if hp := ps.get(p.Record.ID); hp != nil && hp.proc != nil {
					cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
					params, _ := plugin.Targeted(struct{}{}, it.ID)
					var res struct {
						Models []plugin.ProviderModel `json:"models"`
					}
					if err := hp.proc.Call(cctx, "provider.models", params, &res); err != nil {
						pr.ModelsError = err.Error()
					} else {
						pr.Models = mergeModels(pr.Models, res.Models)
					}
					cancel()
				}
			}
			out = append(out, pr)
		}
	}
	return out
}

// mergeModels is declared models first, then the plugin's own that are not among them.
func mergeModels(declared, listed []plugin.ProviderModel) []plugin.ProviderModel {
	out := append([]plugin.ProviderModel(nil), declared...)
	seen := map[string]bool{}
	for _, m := range declared {
		seen[m.ID] = true
	}
	for _, m := range listed {
		if m.ID != "" && !seen[m.ID] {
			seen[m.ID] = true
			out = append(out, m)
		}
	}
	return out
}

// presetFill fills in what a launcher added from a preset leaves empty.
func (h *Hub) presetFill(ctx context.Context, in *launcherInput) error {
	if in.Preset == "" {
		return nil
	}
	for _, pr := range h.plugins.launcherPresets(ctx) {
		if pr.ID != in.Preset {
			continue
		}
		if in.Kind == "" {
			in.Kind = pr.Kind
		}
		if in.API == "" {
			in.API = pr.API
		}
		if in.BaseURL == "" {
			in.BaseURL = pr.BaseURL
		}
		if in.Model == "" {
			in.Model = pr.Model
			if in.Model == "" && len(pr.Models) > 0 {
				in.Model = pr.Models[0].ID
			}
		}
		if in.SmallModel == "" && in.Kind != "api" {
			in.SmallModel = pr.SmallModel
		}
		return nil
	}
	return errNoPreset
}

// presetFor is the launcher-kind preset a launcher reaches its gateway through: the one it was added from, else the one
// whose address is the launcher's gateway (a provider added by hand, or a ~/.zshrc launcher, on a plugin's gateway).
func presetFor(presets []LauncherPreset, preset, baseURL string) (LauncherPreset, bool) {
	var byURL *LauncherPreset
	want := normalGateway(baseURL)
	for i, pr := range presets {
		if pr.Kind != "launcher" {
			continue
		}
		if preset != "" && pr.ID == preset {
			return pr, true
		}
		if byURL == nil && want != "" && normalGateway(pr.BaseURL) == want {
			byURL = &presets[i]
		}
	}
	if byURL != nil {
		return *byURL, true
	}
	return LauncherPreset{}, false
}

// normalGateway compares gateway addresses: no scheme case, host case or trailing slash differences.
func normalGateway(u string) string {
	return strings.TrimRight(strings.ToLower(strings.TrimSpace(u)), "/")
}

// withPresetModels adds a preset's models to a launcher's /model list (its own entries first, then the gateway's that
// are not among them): a gateway with a hundred models otherwise shows only the launcher's one in 新建 Agent.
func withPresetModels(res proto.LauncherModels, pr LauncherPreset) proto.LauncherModels {
	seen := map[string]bool{}
	for _, m := range res.Models {
		seen[m.ID] = true
		if m.Resolved != "" {
			seen[m.Resolved] = true
		}
	}
	for _, m := range pr.Models {
		if m.ID == "" || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		res.Models = append(res.Models, proto.AgentModel{ID: m.ID, Name: cmp.Or(m.Name, m.ID), Description: pr.Name})
	}
	return res
}

// presetModels adds the models of the plugin preset behind launcher name on machine m (managed: its preset or
// gateway; ~/.zshrc: the gateway the node read from it) to a Claude Code /model list.
func (h *Hub) presetModels(ctx context.Context, m *Machine, name string, res proto.LauncherModels) proto.LauncherModels {
	if res.Agent != "" && res.Agent != "claude" {
		return res
	}
	presets := h.plugins.launcherPresets(ctx)
	if len(presets) == 0 {
		return res
	}
	for _, s := range h.storedLaunchers(ctx) {
		if s.Name == name {
			if s.Kind == "api" {
				return res
			}
			if pr, ok := presetFor(presets, s.Preset, s.BaseURL); ok {
				return withPresetModels(res, pr)
			}
			return res
		}
	}
	if l, ok := h.knownZshrcLauncher(ctx, m, name); ok && l.Agent == "" {
		if pr, ok := presetFor(presets, "", l.BaseURL); ok {
			return withPresetModels(res, pr)
		}
	}
	return res
}

// knownZshrcLauncher is machine m's ~/.zshrc launcher name as last seen (in memory, else the stored list), so the
// picker does not wait on a second node call; only a launcher never seen is asked for now.
func (h *Hub) knownZshrcLauncher(ctx context.Context, m *Machine, name string) (proto.Launcher, bool) {
	find := func(list []proto.Launcher) (proto.Launcher, bool) {
		for _, l := range list {
			if l.Name == name {
				return l, true
			}
		}
		return proto.Launcher{}, false
	}
	h.launchers.mu.Lock()
	seen := h.launchersSeen[m.cfg.ID]
	h.launchers.mu.Unlock()
	if l, ok := find(seen); ok {
		return l, true
	}
	if h.store != nil {
		stored := map[string][]proto.Launcher{}
		if ok, _ := h.store.Setting(ctx, launchersZshrcKey, &stored); ok {
			if l, ok := find(stored[m.cfg.ID]); ok {
				return l, true
			}
		}
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	list, err := h.machineLaunchers(cctx, m)
	if err != nil {
		return proto.Launcher{}, false
	}
	return find(list)
}

func (h *Hub) launcherPresetRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/launchers/presets", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"presets": h.plugins.launcherPresets(r.Context())})
	})
}
