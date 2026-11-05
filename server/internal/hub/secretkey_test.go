package hub

import (
	"testing"
)

// The hub's own secrets (bots, connectors, peers) are sealed with secret.key; a restart opens them.
func TestSecretKeySealsAndOpens(t *testing.T) {
	h := &Hub{cfg: &Config{dir: t.TempDir()}}
	sealed, err := h.sealKey("sk-test")
	if err != nil || sealed == "" || sealed == "sk-test" {
		t.Fatalf("sealed %q %v", sealed, err)
	}
	h2 := &Hub{cfg: h.cfg} // a restart reads the same secret.key
	if plain, err := h2.openKey(sealed); err != nil || plain != "sk-test" {
		t.Fatalf("opened %q %v", plain, err)
	}
}
