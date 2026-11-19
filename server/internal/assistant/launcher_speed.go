package assistant

import (
	"fmt"
	"strings"

	"github.com/nwverse/persona/server/internal/proto"
)

// A start's speed (agent_start's and task_plan's speed, an Agent preset's; docs/integrations/launchers.md "Speed"):
// fast and ultrafast cost more usage, so one the Agent that runs cannot take is refused before the user is asked, and
// nothing starts. The node holds the same line and also refuses a Codex model that does not offer the tier
// (node/start_speed.go); an agent the computer's list does not name is left to it.

// checkSpeedArg refuses a speed that is none of proto.AgentSpeeds before anything else is resolved.
func checkSpeedArg(speed string) error {
	if !proto.ValidAgentSpeed(speed) {
		return fmt.Errorf("unknown speed %q: use %s, or leave it out", speed, strings.Join(proto.AgentSpeeds, ", "))
	}
	return nil
}

// startSpeedRefused is the refusal of speed for launcher, whose Agent on the computer it starts on is agent (fitLauncher);
// nil when the speed asks nothing faster, the agent takes it, or the computer's list does not say what runs.
func startSpeedRefused(launcher, agent, speed string) error {
	if err := checkSpeedArg(speed); err != nil {
		return err
	}
	if ok, known := proto.AgentTakesSpeed(agent, speed); ok || !known {
		return nil
	}
	if launcher == "" {
		launcher = "claude"
	}
	return fmt.Errorf("launcher %s runs %s, which cannot start at speed %s: fast works only with Codex and Claude Code, "+
		"ultrafast only with Codex models that offer it; leave speed out or choose such a launcher", launcher, agent, speed)
}
