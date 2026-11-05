package hub

import (
	"testing"

	"github.com/nwverse/persona/server/internal/lark"
)

// (assistant.lark, what a node gets for the skills' gated commands, is the manager's: extmgr's larkcli_test.go.)

func TestLarkPreferValidated(t *testing.T) {
	for prefer, ok := range map[string]bool{"": true, "web-gateway": true, "cli": true, "lark-cli": false, "Web Gateway": false} {
		if err := (lark.Config{Prefer: prefer}).Validate(); (err == nil) != ok {
			t.Errorf("prefer %q: %v", prefer, err)
		}
	}
	if (lark.Config{CLI: &lark.CLIConfig{}}).LegacyGatewayConfigured() || !(lark.Config{Container: "lark-gateway"}).LegacyGatewayConfigured() {
		t.Error("LegacyGatewayConfigured reads token / container only")
	}
}
