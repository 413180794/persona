package extmgr

import (
	"context"
	"encoding/json"
	"testing"
)

// The inbox's rules are the manager's settings (its own store, hub.json's inbox section underneath): a change is a
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
