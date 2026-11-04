package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/nwverse/persona/server/internal/extension"
)

// A source's own triage rules (#1644) for jarvis_settings: a plugin whose settings declare triage_rules (a string at the
// top of its schema) sends them as its inbox things' rules, which replace inbox.rules for its items. The entry reads
// and writes that setting the way 设置 › 插件 does (the registry's SetConfig: held to the schema, the plugin restarts).
// The hub's own connectors (GitHub, iMessage) carry no rules yet.

// triageRulesField is the plugin setting that holds a source's triage rules (docs/plugins.md「约定的设置项」).
const triageRulesField = "triage_rules"

// sourceRulesSettings is one entry per plugin that declares triage_rules.
func (h *Hub) sourceRulesSettings() []selfSetting {
	if h.extensions == nil {
		return nil
	}
	var out []selfSetting
	for _, p := range h.extensions.reg.List() {
		if !declaresTriageRules(p.Schema) {
			continue
		}
		id, name := p.Record.ID, p.Record.ID
		if p.Manifest != nil && p.Manifest.Name != "" {
			name = p.Manifest.Name
		}
		title := name + "的分拣规则"
		out = append(out, selfSetting{key: "inbox.rules." + id, title: title,
			about: fmt.Sprintf("the user's own rules for sorting the inbox items from %s (plugin %s), in their words; when set they "+
				"replace inbox.rules for its items (the defaults stay underneath)", name, id),
			value: `a string (the whole rules text; "" clears it: its items use inbox.rules)`,
			get: func(context.Context) (string, error) {
				return orNone(h.extensionTriageRules(id)), nil
			},
			set: func(ctx context.Context, v json.RawMessage) (*selfChange, error) {
				return h.setExtensionTriageRules(id, title, v)
			}})
	}
	return out
}

func declaresTriageRules(s *extension.Schema) bool {
	return s != nil && slices.ContainsFunc(s.Fields, func(f extension.Field) bool { return f.Name == triageRulesField && f.Type == "string" })
}

// pluginTriageRules is the plugin's triage_rules as it gets them (its default filled in).
func (h *Hub) extensionTriageRules(id string) string {
	p := h.extensions.reg.Get(id)
	if p == nil {
		return ""
	}
	var cfg map[string]json.RawMessage
	_ = json.Unmarshal(h.extensions.reg.Config(p), &cfg)
	var rules string
	_ = json.Unmarshal(cfg[triageRulesField], &rules)
	return strings.TrimSpace(rules)
}

// setPluginTriageRules changes the plugin's triage_rules, the rest of its settings as they are ("" removes the key).
func (h *Hub) setExtensionTriageRules(id, title string, v json.RawMessage) (*selfChange, error) {
	var text string
	if err := json.Unmarshal(v, &text); err != nil {
		return nil, errors.New("value must be a string")
	}
	text = strings.TrimSpace(text)
	p := h.extensions.reg.Get(id)
	if p == nil {
		return nil, fmt.Errorf("no plugin %s", id)
	}
	if p.ReadOnly() {
		return nil, fmt.Errorf("%s is set in hub.json: change it there (or in the admin page)", id)
	}
	before := h.extensionTriageRules(id)
	if text == before {
		return &selfChange{summary: title + "没有变化"}, nil
	}
	return &selfChange{summary: fmt.Sprintf("改%s：%s → %s", title, clipped(before), clipped(text)),
		apply: func(ctx context.Context, _ map[string]string) (string, error) {
			p := h.extensions.reg.Get(id)
			if p == nil {
				return "", fmt.Errorf("no plugin %s", id)
			}
			cfg := map[string]json.RawMessage{}
			if len(p.Record.Config) > 0 {
				if err := json.Unmarshal(p.Record.Config, &cfg); err != nil {
					return "", err
				}
			}
			if text == "" {
				delete(cfg, triageRulesField)
			} else {
				cfg[triageRulesField], _ = json.Marshal(text)
			}
			raw, _ := json.Marshal(cfg)
			if err := h.extensions.reg.SetConfig(ctx, id, raw, "assistant"); err != nil {
				return "", err
			}
			return title + " updated; " + id + " restarts with it and sends it with its next inbox items.", nil
		}}, nil
}
