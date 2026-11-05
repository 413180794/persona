package node

import (
	"context"
	"testing"
)

// The node's launcher repair runs only its fixed update, and says so for a launcher or an action it does not cover.
func TestLauncherRepair(t *testing.T) {
	n := &Node{dir: t.TempDir()}
	old := claudeUpdateScript
	t.Cleanup(func() { claudeUpdateScript = old })

	if r, err := n.launcherRepair(context.Background(), "claude", "rm -rf /"); err != nil || !r.Unsupported || r.OK {
		t.Fatalf("unknown action: %+v %v", r, err)
	}
	claudeUpdateScript = "echo Successfully updated"
	if r, err := n.launcherRepair(context.Background(), "claude", launcherRepairUpgrade); err != nil || !r.OK || r.Output != "Successfully updated" {
		t.Fatalf("update: %+v %v", r, err)
	}
	claudeUpdateScript = "echo no network >&2; exit 3"
	if r, err := n.launcherRepair(context.Background(), "claude", launcherRepairUpgrade); err != nil || r.OK || r.Output != "no network" {
		t.Fatalf("failed update: %+v %v", r, err)
	}
}
