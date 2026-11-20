package runtimepkg

import (
	"os"
	"testing"
)

// TestDistBudget bounds what personad, persona-node and persona-manager embed of the runtime packages: every file
// Generate writes from integrations/. Measured 185,049 B on 2026-11-20: fan/develop at 180,378 B, plus Antigravity's
// tool-result images on demand (upstream #11645), a refused request's cause (#11742) and its guard against pictures
// the model service cannot decode (#11789). Raise it only for code a runtime needs, never to embed the packages raw again.
func TestDistBudget(t *testing.T) {
	const budget = 185_049
	_, out := generateIntegrations(t)
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	size := int64(0)
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().IsRegular() {
			size += info.Size()
		}
	}
	if size > budget {
		t.Fatalf("generated runtime packages are %d bytes, over the budget of %d", size, budget)
	}
	t.Logf("generated runtime packages: %d bytes (budget %d)", size, budget)
}
