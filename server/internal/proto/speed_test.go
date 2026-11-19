package proto

import "testing"

func TestAgentTakesSpeed(t *testing.T) {
	for _, tc := range []struct {
		agent, speed string
		ok, known    bool
	}{
		{"agy", "", true, true},
		{"agy", SpeedStandard, true, true},
		{"codex", SpeedFast, true, true},
		{"codex", SpeedUltrafast, true, true},
		{"claude", SpeedFast, true, true},
		{"claude", SpeedUltrafast, false, true},
		{"agy", SpeedFast, false, true},
		{"traex", SpeedFast, false, true},
		{"", SpeedFast, false, false},
	} {
		if ok, known := AgentTakesSpeed(tc.agent, tc.speed); ok != tc.ok || known != tc.known {
			t.Errorf("AgentTakesSpeed(%q, %q) = %v, %v; want %v, %v", tc.agent, tc.speed, ok, known, tc.ok, tc.known)
		}
	}
	for speed, tier := range map[string]string{"": "", SpeedStandard: "", SpeedFast: "priority", SpeedUltrafast: "ultrafast"} {
		if got := CodexServiceTier(speed); got != tier {
			t.Errorf("CodexServiceTier(%q) = %q, want %q", speed, got, tier)
		}
	}
	if ValidAgentSpeed("turbo") || !ValidAgentSpeed("") || !ValidAgentSpeed(SpeedUltrafast) {
		t.Error("ValidAgentSpeed")
	}
}
