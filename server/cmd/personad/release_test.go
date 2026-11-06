package main

import (
	"strings"
	"testing"
)

// release manifest no longer takes --min-node: it wrote a key nothing read, so a release could ask for a node order
// that never happened (docs/hub-update.md, #3226).
func TestReleaseManifestHasNoMinNodeFlag(t *testing.T) {
	err := runRelease([]string{"manifest", "--min-node", "209901010000"})
	if err == nil || !strings.Contains(err.Error(), "min-node") {
		t.Fatalf("--min-node is still accepted: %v", err)
	}
}
