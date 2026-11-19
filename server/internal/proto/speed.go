package proto

import "slices"

// Agent start speeds (docs/integrations/launchers.md "Speed"): what agent_start, task_plan and an Agent preset ask a new
// Agent to run at. Standard asks for nothing faster: the launcher's own setting applies. Fast is Codex's native Fast tier
// (priority) or Claude Code's fast mode; Ultrafast only a Codex model whose catalog advertises it. Both cost more usage.
const (
	SpeedStandard  = "standard"
	SpeedFast      = "fast"
	SpeedUltrafast = "ultrafast"
)

// AgentSpeeds are the speeds a start may name, slowest first.
var AgentSpeeds = []string{SpeedStandard, SpeedFast, SpeedUltrafast}

// ValidAgentSpeed: speed is "" (not given) or one of AgentSpeeds.
func ValidAgentSpeed(speed string) bool { return speed == "" || slices.Contains(AgentSpeeds, speed) }

// FasterSpeed: speed asks for more than the launcher's own speed, which only some agents can run.
func FasterSpeed(speed string) bool { return speed == SpeedFast || speed == SpeedUltrafast }

// AgentTakesSpeed says whether an agent of type agent starts at speed: anything for a speed that asks for nothing faster,
// Codex at fast and ultrafast (its model then has to advertise the tier), Claude Code only at fast. known is false
// for an agent type nobody knows yet (""): the node decides.
func AgentTakesSpeed(agent, speed string) (ok, known bool) {
	switch {
	case !FasterSpeed(speed):
		return true, true
	case agent == "":
		return false, false
	case agent == "codex":
		return true, true
	case agent == "claude":
		return speed == SpeedFast, true
	}
	return false, true
}

// CodexServiceTier is the native Codex service tier (its model catalog's serviceTiers id) for a faster speed; "" for
// one that asks for nothing faster.
func CodexServiceTier(speed string) string {
	switch speed {
	case SpeedFast:
		return "priority"
	case SpeedUltrafast:
		return "ultrafast"
	}
	return ""
}
