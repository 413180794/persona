package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sync"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connector"
)

// The hub's plain capabilities the assistant asks for its directory and its settings (app.call, assistantwire): the
// model configuration's records and launchers, where the knowledge base keeps its files, the connectors signed in,
// the plugins' assistant contributions, and the hub's own settings for jarvis_settings. Nothing here knows what the
// assistant does with them.

// assistantAppCall answers the app.call methods of this file; false for the others.
func (h *Hub) assistantAppCall(ctx context.Context, p assistantwire.AppCall) (json.RawMessage, bool, error) {
	var a struct {
		Purpose   string         `json:"purpose"`
		Revision  int64          `json:"revision"`
		Binding   ModelBinding   `json:"binding"`
		Params    map[string]any `json:"params"`
		MachineID string         `json:"machine_id"`
		Launcher  string         `json:"launcher"`
		Model     string         `json:"model"`
		Names     []string       `json:"names"`
	}
	unmarshal := func() error {
		if len(p.Params) == 0 {
			return nil
		}
		return json.Unmarshal(p.Params, &a)
	}
	var out any
	var err error
	switch p.AppID + " " + p.Method {
	case assistantwire.AppModels + " " + assistantwire.ModelsRecord:
		if err = unmarshal(); err == nil {
			out, err = h.modelRecord(ctx, a.Purpose)
		}
	case assistantwire.AppModels + " " + assistantwire.ModelsSave:
		if err = unmarshal(); err == nil {
			h.modelMu.Lock()
			err = h.saveModelRecord(ctx, a.Purpose, a.Revision, a.Binding)
			h.modelMu.Unlock()
			out = map[string]any{}
		}
	case assistantwire.AppModels + " " + assistantwire.ModelsApplyPicked:
		if err = unmarshal(); err == nil {
			if a.Params == nil {
				a.Params = map[string]any{}
			}
			err = h.applyPickedModel(ctx, a.Params)
			out = map[string]any{"params": a.Params}
		}
	case assistantwire.AppModels + " " + assistantwire.ModelsLaunchers:
		if err = unmarshal(); err == nil {
			m := h.localMachine(a.MachineID)
			if m == nil {
				return nil, true, busCodedError(noDevice(a.MachineID))
			}
			list, lerr := h.machineLaunchers(ctx, m)
			out, err = map[string]any{"launchers": list}, lerr
		}
	case assistantwire.AppModels + " " + assistantwire.ModelsLauncherCheck:
		if err = unmarshal(); err == nil {
			m := h.localMachine(a.MachineID)
			if m == nil {
				return nil, true, busCodedError(noDevice(a.MachineID))
			}
			available := h.launcherAvailable(ctx, m, a.Launcher)
			modelOK := !available || a.Model == "" || h.launcherHasModel(ctx, m, a.Launcher, a.Model)
			out = map[string]any{"available": available, "model_ok": modelOK}
		}
	case assistantwire.AppKB + " " + assistantwire.KBDirs:
		res := map[string]any{"knowledge_dir": "", "extra_dirs": h.kb.cfg.ExtraDirs}
		if h.cfg != nil && h.cfg.dir != "" {
			res["knowledge_dir"] = path.Join(h.cfg.dir, "data")
		}
		out = res
	case assistantwire.AppConnectors + " " + assistantwire.ConnectorsSignedIn:
		signedIn := h.signedInConnectors(ctx)
		clis := map[string]string{}
		for _, id := range signedIn {
			if c := h.connectors.Get(id); c != nil && c.Manifest().CLI != "" {
				clis[c.Manifest().CLI] = id
			}
		}
		out = map[string]any{"connectors": signedIn, "clis": clis, "lark_cli": h.cfg != nil && h.cfg.Lark.CLI != nil}
	case assistantwire.AppPlugins + " " + assistantwire.PluginsAssistant:
		if err = unmarshal(); err == nil {
			out = map[string]any{"instructions": h.extensions.extensionInstructions(), "skills": h.extensions.extensionSkillsFor(a.MachineID)}
		}
	case assistantwire.AppPlugins + " " + assistantwire.PluginsSkillsNeed:
		if err = unmarshal(); err == nil && h.extensions != nil {
			if v, ok := h.extensions.skillsSent.Load(a.MachineID); ok {
				for _, n := range a.Names {
					v.(*sync.Map).Delete(n)
				}
			}
		}
		out = map[string]any{}
	case assistantwire.AppSettings + " " + assistantwire.SettingsList:
		list := []assistantwire.Setting{}
		for _, s := range h.selfSettings() {
			list = append(list, assistantwire.Setting{Key: s.key, Title: s.title, About: s.about, Value: s.value, Actions: s.actions()})
		}
		out = map[string]any{"settings": list}
	default:
		return nil, false, nil
	}
	if err != nil {
		return nil, true, busCodedError(err)
	}
	raw, err := json.Marshal(out)
	return raw, true, err
}

// signedInConnectors are the connectors that are ok or rate limited (switched on and signed in).
func (h *Hub) signedInConnectors(ctx context.Context) []string {
	out := []string{}
	if h.connectors == nil {
		return out
	}
	for _, c := range h.connectors.All() {
		if st := connector.StatusOf(ctx, c).State; st == connector.StateOK || st == connector.StateRateLimited {
			out = append(out, c.ID())
		}
	}
	return out
}

// noDevice is device.call's and app.call's error for a machine the hub does not have.
func noDevice(id string) error { return fmt.Errorf("%w %q", assistantwire.ErrNoDevice, id) }
