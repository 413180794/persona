package hub

import (
	"testing"

	"github.com/nwverse/persona/server/internal/lark"
)

// (assistant.lark, what a node gets for the skills' gated commands, is the manager's: extmgr's larkcli_test.go.)

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
