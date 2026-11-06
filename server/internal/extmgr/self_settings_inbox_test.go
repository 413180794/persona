package extmgr

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The inbox's rules are the manager's settings (its own store, manager.json's inbox section underneath): a change is a
// card that sets them, "" falls back; both are listed among the manager's settings.
func TestSelfSettingsInboxRulesPersist(t *testing.T) {
	h := newTestManager(t)
	h.store = newMemStore()
	ctx := context.Background()
	h.cfg.Inbox.Rules = "fallback"
	change, err := h.textSetting(ctx, json.RawMessage(`"custom"`), "规则", h.inboxRules, "inbox.rules")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = change.apply(ctx, nil); err != nil || h.inboxRules(ctx) != "custom" {
		t.Fatalf("rules: %v %q", err, h.inboxRules(ctx))
	}
	change, err = h.textSetting(ctx, json.RawMessage(`""`), "规则", h.inboxRules, "inbox.rules")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = change.apply(ctx, nil); err != nil || h.inboxRules(ctx) != "fallback" {
		t.Fatalf("cleared rules: %v %q", err, h.inboxRules(ctx))
	}
	keys := map[string]bool{}
	for _, s := range h.settingsList() {
		keys[s.Key] = true
	}
	if !keys["inbox.rules"] || !keys["inbox.agent_rules"] {
		t.Fatalf("the manager's settings: %v", keys)
	}
}

// With the AI twin off, the assistant reading or changing inbox.agent_rules is told so: the list's about, a get and
// the result of a set all say it drafts nothing. With it on, the about says so and nothing is added (#5865).
func TestSelfSettingsAgentRulesTellTheTwinSwitch(t *testing.T) {
	ctx := context.Background()
	const rule = "group chats about releases link to the release task"
	for _, on := range []bool{false, true} {
		h := newTestManager(t)
		h.store = newMemStore()
		h.cfg.Inbox.Agent.Enabled = on
		var about string
		for _, s := range h.settingsList() {
			if s.Key == "inbox.agent_rules" {
				about = s.About
			}
		}
		var setting selfSetting
		for _, s := range h.selfSettings() {
			if s.key == "inbox.agent_rules" {
				setting = s
			}
		}
		got, err := setting.get(ctx)
		if err != nil {
			t.Fatal(err)
		}
		change, err := setting.set(ctx, json.RawMessage(`"`+rule+`"`))
		if err != nil {
			t.Fatal(err)
		}
		res, err := change.apply(ctx, nil)
		if err != nil || h.inboxAgentRules(ctx) != rule {
			t.Fatalf("set: %v, stored %q", err, h.inboxAgentRules(ctx))
		}
		const off = "the AI twin is off"
		if on {
			if !strings.Contains(about, "the AI twin is on") || strings.Contains(about+got+res, off) {
				t.Fatalf("twin on: about %q, get %q, set %q", about, got, res)
			}
			continue
		}
		for name, s := range map[string]string{"about": about, "get": got, "set": res} {
			if !strings.Contains(s, off) {
				t.Fatalf("twin off, %s says nothing of it: %q", name, s)
			}
		}
	}
}
