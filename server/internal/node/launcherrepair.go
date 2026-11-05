package node

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// launcherRepairUpgrade is the one repair a node does by itself for the hub's launcher health (docs/launcher-health.md):
// update the Claude Code CLI a launcher runs. Fixed commands only: the hub cannot have the node run anything else.
const launcherRepairUpgrade = "upgrade_cli"

// claudeUpdateScript updates Claude Code in the login shell (tests replace it).
var claudeUpdateScript = "claude update"

// launcherRepairTimeout bounds one repair: an update downloads the CLI.
var launcherRepairTimeout = 5 * time.Minute

// launcherRepair runs one of the fixed repairs for launcher name. A repair that does not apply to the launcher's agent
// (a Codex launcher, an unknown action) answers Unsupported, not an error.
func (n *Node) launcherRepair(ctx context.Context, name, action string) (proto.LauncherRepair, error) {
	if action != launcherRepairUpgrade {
		return proto.LauncherRepair{Unsupported: true, Output: fmt.Sprintf("unknown repair %q", action)}, nil
	}
	agent := "claude"
	if _, managed := n.managedLauncher(name); !managed && name != "claude" {
		if d := n.launcherProbeCached(ctx, name); d.Agent != "" {
			agent = d.Agent
		}
	}
	if agent != "claude" {
		return proto.LauncherRepair{Unsupported: true, Output: agent + " is not updated by the node"}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, launcherRepairTimeout)
	defer cancel()
	cmd := loginShell(ctx, claudeUpdateScript+" </dev/null")
	killGroupOnCancel(cmd)
	out, err := cmd.CombinedOutput()
	res := proto.LauncherRepair{Output: clipText(redactSecrets(lastLines(strings.TrimSpace(string(out)), 6)), 400)}
	switch {
	case ctx.Err() != nil:
		res.Output = strings.TrimSpace(res.Output + "\n" + fmt.Sprintf("timed out after %s", launcherRepairTimeout))
	case err != nil:
		if res.Output == "" {
			res.Output = err.Error()
		}
	default:
		res.OK = true
	}
	return res, nil
}
