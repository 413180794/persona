package assistant

import "testing"

// Changing the main chat's launcher in its settings rewrites model_config.assistant: the runtime (#2455) must stay.
func TestSendModelRuntimeSurvivesALauncherChange(t *testing.T) {
	b := mergeModel(ModelBinding{Provider: "launcher.a", Model: "m", Effort: "high", Runtime: "agentsdk"}, ModelBinding{Provider: "launcher.b"})
	if b != (ModelBinding{Provider: "launcher.b", Runtime: "agentsdk"}) {
		t.Fatalf("%+v", b)
	}
}
