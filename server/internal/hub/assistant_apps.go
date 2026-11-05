package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connector"
)

// The hub's plain capabilities the assistant asks for its directory and its settings (app.call, assistantwire): the
// plugins' model providers (assistant_models.go), where the knowledge base keeps its files, the connectors signed in,
// the plugins' assistant contributions, and the hub's own settings for jarvis_settings. Nothing here knows what the
// assistant does with them.

// assistantAppCall answers the app.call methods of this file; false for the others.
func (h *Hub) assistantAppCall(ctx context.Context, p assistantwire.AppCall) (json.RawMessage, bool, error) {
	var a struct {
		MachineID string   `json:"machine_id"`
		Names     []string `json:"names"`
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
