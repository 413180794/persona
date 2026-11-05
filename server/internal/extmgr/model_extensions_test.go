package extmgr

import (
	"encoding/json"
	"testing"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

// A voice extension whose models cannot be read: the model configuration (the assistant's, which asks with
// plugins.voice) shows why, in the user's language, not an empty list.
func TestVoiceExtensionModelsUnreadable(t *testing.T) {
	h := newTestManager(t)
	h.extensions = newExtensions(t.TempDir(), []ExtensionConfig{{ID: "voice", Name: "Voice", Kind: "voice", Socket: "/nonexistent/model-test.sock"}}, t.Logf)
	raw, err := pluginsCall(t, h, assistantwire.PluginsVoice, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Plugins []assistantwire.VoicePlugin `json:"plugins"`
	}
	_ = json.Unmarshal(raw, &out)
	if len(out.Plugins) != 1 || out.Plugins[0].ID != "voice" || out.Plugins[0].Name != "Voice" || out.Plugins[0].Error != "读不到扩展模型列表" {
		t.Fatalf("voice plugins %+v", out.Plugins)
	}
}
