package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/extwire"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// The hub's own settings jarvis_settings offers (docs/assistant.md "Operating Persona itself", 2026-11-01): the tool is the
// assistant's (its own settings are its own: "About me", its launcher, …), which lists these (app.call settings.list) and
// hands the actions on them to the hub (app.tool jarvis_settings). One registry: each setting says what it is, how to
// read it and how a change is made. Reads are free; every change is a card on the phone that says what changes (before
// → after) and is applied only when approved. What the card needs filled in (a password) is typed on the phone and
// never passes through the model. Some things are never here: the hub's and devices' tokens, pairing, which machines
// the hub reaches, permission modes.

// selfSetting is one thing Persona may change. A value setting has set; a list has add / remove.
type selfSetting struct {
	key, title, about string
	value             string // what set / add take as value, for the model
	get               func(ctx context.Context) (string, error)
	set               func(ctx context.Context, v json.RawMessage) (*selfChange, error)
	add               func(ctx context.Context, v json.RawMessage) (*selfChange, error)
	remove            func(ctx context.Context, id string) (*selfChange, error)
}

// selfChange is a change waiting for the phone: what the card says, what it asks for, and how it is made.
type selfChange struct {
	summary string
	fields  []proto.Field
	apply   func(ctx context.Context, values map[string]string) (string, error)
}

func (s selfSetting) actions() []string {
	a := []string{"get"}
	if s.set != nil {
		a = append(a, "set")
	}
	if s.add != nil {
		a = append(a, "add")
	}
	if s.remove != nil {
		a = append(a, "remove")
	}
	return a
}

// selfSettings is the registry of the hub's own; the manager's (mail, the sources' own rules) are listed beside them
// (managerSettings).
func (h *Hub) selfSettings() []selfSetting {
	l := h.userLang() // the titles are on the phone's card
	rulesTitle, agentRulesTitle := ss(l, "title_inbox_rules"), ss(l, "title_agent_rules")
	out := []selfSetting{
		{key: "inbox.rules", title: rulesTitle,
			about: "the user's own rules for sorting the inbox (需要你 / 知悉即可 / 噪音), in their words; they win over the defaults " +
				"(a source with rules of its own, inbox.rules.<plugin>, uses those instead)",
			value: `a string (the whole rules text; "" clears it and falls back to hub.json)`,
			get:   func(ctx context.Context) (string, error) { return orNone(h.inboxRules(ctx)), nil },
			set: func(ctx context.Context, v json.RawMessage) (*selfChange, error) {
				return h.textSetting(ctx, v, rulesTitle, h.inboxRules, "inbox.rules")
			}},
		{key: "inbox.agent_rules", title: agentRulesTitle,
			about: "the user's rules for which 需要你 inbox items the AI 分身 takes on and drafts a reply for",
			value: `a string ("" clears it)`,
			get:   func(ctx context.Context) (string, error) { return orNone(h.inboxAgentRules(ctx)), nil },
			set: func(ctx context.Context, v json.RawMessage) (*selfChange, error) {
				return h.textSetting(ctx, v, agentRulesTitle, h.inboxAgentRules, "inbox.agent_rules")
			}},
	}
	if h.store != nil {
		out = append(out, h.blockedClaudeDirsSetting())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

func (h *Hub) toolJarvisSettings(ctx context.Context, m *Machine, sessionID string, args json.RawMessage) (string, error) {
	var a struct {
		Action string          `json:"action"`
		Key    string          `json:"key"`
		Value  json.RawMessage `json:"value"`
		ID     string          `json:"id"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	all := h.selfSettings() // the list is the assistant's (it adds its own: settings.list)
	var s *selfSetting
	for i := range all {
		if all[i].key == a.Key {
			s = &all[i]
		}
	}
	if s == nil {
		for _, ms := range h.managerSettings(ctx) { // the manager's (mail, the sources' own rules): it does the change
			if ms.Key == a.Key {
				var res struct {
					Text string `json:"text"`
				}
				if err := h.extCall(ctx, extwire.HubSettingsTool, map[string]any{"machine_id": m.cfg.ID, "session_id": sessionID,
					"arguments": args}, &res); err != nil {
					return "", err
				}
				return res.Text, nil
			}
		}
		return "", fmt.Errorf("no setting %q (action list shows them)", a.Key)
	}
	var change *selfChange
	var err error
	switch {
	case a.Action == "get":
		return s.get(ctx)
	case a.Action == "set" && s.set != nil:
		change, err = s.set(ctx, a.Value)
	case a.Action == "add" && s.add != nil:
		change, err = s.add(ctx, a.Value)
	case a.Action == "remove" && s.remove != nil:
		change, err = s.remove(ctx, a.ID)
	default:
		return "", fmt.Errorf("%s does not take %q (it takes %s)", s.key, a.Action, strings.Join(s.actions(), ", "))
	}
	if err != nil {
		return "", err
	}
	if change.apply == nil {
		return change.summary, nil // nothing to change
	}
	shown, _ := json.Marshal(map[string]string{"setting": s.title, "action": a.Action})
	ix := proto.Interaction{ID: "ix_" + randomHex(6), MachineID: m.cfg.ID, SessionID: sessionID, Kind: proto.IxPermission,
		ToolName: "jarvis_settings", Summary: change.summary, ToolInput: shown, Fields: change.fields, SessionTitle: "Persona",
		CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(15 * time.Minute).UTC()}
	r, err := h.waitInteraction(ctx, ix)
	if err != nil {
		return "", err
	}
	if r.Allow == nil || !*r.Allow {
		return "The user declined on the phone: nothing changed.", nil
	}
	for _, f := range change.fields {
		if !f.Optional && strings.TrimSpace(r.Values[f.Key]) == "" {
			return "", fmt.Errorf("%s was left empty on the phone: nothing changed", f.Label)
		}
	}
	return change.apply(ctx, r.Values)
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "（无）"
	}
	return s
}

// ss is a hub-self_settings text in l.
func ss(l i18n.Lang, name string, args ...i18n.Args) string {
	return i18n.T(l, "hub-self_settings."+name, args...)
}

// clipped is a value as the card shows it, in l.
func clipped(l i18n.Lang, s string) string {
	if strings.TrimSpace(s) == "" {
		return ss(l, "none")
	}
	return clip(oneLine(s), 120)
}

// changeSummary is the card's "change title: before → after".
func changeSummary(l i18n.Lang, title, before, after string) string {
	return ss(l, "change", i18n.Args{"title": title, "before": clipped(l, before), "after": clipped(l, after)})
}

// ---- inbox rules (settings in Postgres win over hub.json) ----

func (h *Hub) settingText(ctx context.Context, key, fallback string) string {
	if h.store != nil {
		var v string
		if ok, err := h.store.Setting(ctx, key, &v); err == nil && ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return strings.TrimSpace(fallback)
}

func (h *Hub) inboxRules(ctx context.Context) string {
	return h.settingText(ctx, "inbox.rules", h.cfg.Inbox.Rules)
}
func (h *Hub) inboxAgentRules(ctx context.Context) string {
	return h.settingText(ctx, "inbox.agent_rules", h.cfg.Inbox.Agent.Rules)
}

// textSetting changes a text setting kept in Postgres under key ("" deletes the override).
func (h *Hub) textSetting(ctx context.Context, v json.RawMessage, title string, now func(context.Context) string, key string) (*selfChange, error) {
	var text string
	if err := json.Unmarshal(v, &text); err != nil {
		return nil, errors.New("value must be a string")
	}
	text = strings.TrimSpace(text)
	before := now(ctx)
	l := h.userLang()
	if text == before {
		return &selfChange{summary: ss(l, "no_change", i18n.Args{"title": title})}, nil
	}
	if h.store == nil {
		return nil, errors.New("no database on this hub")
	}
	return &selfChange{summary: changeSummary(l, title, before, text),
		apply: func(ctx context.Context, _ map[string]string) (string, error) {
			if err := h.store.PutSetting(ctx, key, text); err != nil {
				return "", err
			}
			return title + " updated; it applies from the next items sorted.", nil
		}}, nil
}

// managerSettings are the manager's jarvis_settings entries (none while it is away).
func (h *Hub) managerSettings(ctx context.Context) []extwire.Setting {
	if h.busConnOf(devicesOwner) == nil {
		return nil
	}
	var res struct {
		Settings []extwire.Setting `json:"settings"`
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := h.extCall(ctx, extwire.HubSettingsList, map[string]any{}, &res); err != nil {
		h.logf("settings: the manager's: %v", err)
	}
	return res.Settings
}
