package voice

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nwverse/persona/server/internal/prompts"
)

// The voice plugin's prompts and tools, byte for byte (docs/prompts.md). After changing one on purpose:
//
//	UPDATE_GOLDEN=1 go test ./internal/voice -run Golden
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "prompts", name+".golden")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (UPDATE_GOLDEN=1 writes it)", name, err)
	}
	if got != string(want) {
		t.Errorf("%s differs from %s\n--- got ---\n%s\n--- want ---\n%s", name, path, got, want)
	}
}

func TestGoldenVoicePrompts(t *testing.T) {
	history := []prompts.VoiceLine{{Role: "user", Text: "记住数字七"}, {Role: "assistant", Text: "好的，记住了。"}}
	golden(t, "instructions.delegate", prompts.RenderVoiceInstructions(prompts.VoiceInstructions{}))
	golden(t, "instructions.tools", prompts.RenderVoiceInstructions(prompts.VoiceInstructions{Owner: "老板", Notes: "说话简短。", Tools: true}))
	golden(t, "instructions.reconnected", prompts.RenderVoiceInstructions(prompts.VoiceInstructions{Owner: "老板", History: history}))
	golden(t, "delegated.plain", prompts.RenderVoiceDelegated(prompts.VoiceDelegated{Task: "查明天的日程"}))
	golden(t, "delegated.history", prompts.RenderVoiceDelegated(prompts.VoiceDelegated{Task: "查明天的日程", History: history}))
	golden(t, "handoff", prompts.RenderVoiceHandoff())
	for _, d := range []delegation{
		{Status: "done", Text: "明天下午三点有会"},
		{Status: "done"},
		{Status: "needs_you"},
		{Status: "failed", Text: "assistant machine is not configured"},
		{Status: "timeout"},
	} {
		name := "report." + d.Status
		if d.Status == "done" && d.Text == "" {
			name += "_empty"
		}
		golden(t, name, report("查明天的日程", d))
	}
	b, err := json.MarshalIndent(toolDecls(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "tools", string(b)+"\n")
}
