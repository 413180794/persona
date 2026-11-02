package hub

import (
	"context"
	"testing"

	"github.com/chyroc/jarvis/server/internal/lark"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// With only lark-cli in hub.json (docs/plan-lark-cli.md), the assistant's machine learns how to run it, and there is
// no lark-connector token to hand out (asking lark-connector would fail: it is not configured).
func TestAssistantLarkWithLarkCLIOnly(t *testing.T) {
	h := newTestHub(t)
	h.cfg.Lark = lark.Config{CLI: &lark.CLIConfig{Bin: "/opt/lark/bin/lark-cli", Profile: "work"}, Prefer: "cli"}
	h.lark = lark.New(h.cfg.Lark)
	m := newMachine(h, MachineConfig{ID: "server", Name: "服务器", Kind: "local"})
	h.machines, h.byID["server"] = []*Machine{m}, m

	out, err := m.onRequest(context.Background(), "assistant.lark", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(proto.AssistantLark)
	if got.CLI == nil || got.CLI.Bin != "/opt/lark/bin/lark-cli" || got.CLI.Profile != "work" || got.Prefer != "cli" {
		t.Fatalf("lark-cli config not passed on: %+v", got)
	}
	if got.Base != "" || got.Token != "" {
		t.Fatalf("no lark-connector, no token: %+v", got)
	}
}

func TestLarkPreferValidated(t *testing.T) {
	for prefer, ok := range map[string]bool{"": true, "lark-connector": true, "cli": true, "lark-cli": false} {
		if err := (lark.Config{Prefer: prefer}).Validate(); (err == nil) != ok {
			t.Errorf("prefer %q: %v", prefer, err)
		}
	}
	if (lark.Config{CLI: &lark.CLIConfig{}}).lark-connectorConfigured() || !(lark.Config{Container: "lark-connector"}).lark-connectorConfigured() {
		t.Error("lark-connectorConfigured reads token / container only")
	}
}
