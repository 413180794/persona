package hub

import (
	"context"
	"reflect"
	"testing"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/lark"
	"github.com/nwverse/persona/server/internal/proto"
)

// With only lark-cli in hub.json (docs/plan-lark-cli.md), the assistant's machine learns how to run it, and there is
// no lark-connector token to hand out (asking lark-connector would fail: it is not configured).
func TestAssistantLarkWithLarkCLIOnly(t *testing.T) {
	h := newTestHub(t)
	h.cfg.Lark = lark.Config{CLI: &lark.CLIConfig{Bin: "/opt/lark/bin/lark-cli", Profile: "work"}, Prefer: "cli"}
	h.cfg.ConnectorGate = []connector.Rule{{Connector: "lark", Action: "CLI /im/+messages-send", Decision: "allow"}}
	h.cfg.LarkGate = []proto.LarkRule{{Method: "CLI", Path: "/docs/**", Action: "deny"}}
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
	// connector_gate's lark rules first, then the older lark_gate ones.
	if want := []proto.LarkRule{{Method: "CLI", Path: "/im/+messages-send", Action: "allow"}, h.cfg.LarkGate[0]}; !reflect.DeepEqual(got.Rules, want) {
		t.Fatalf("rules: %+v, want %+v", got.Rules, want)
	}
	if len(got.Gates) != 0 {
		t.Fatalf("no plugin, no gated command: %+v", got.Gates)
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
