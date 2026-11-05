package hub

import (
	"fmt"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// The node's reasons for running an Agent SDK conversation on the CLI read in the app's language, the cause as it was;
// a reason the hub does not know, and Chinese, stay as they are.
func TestRuntimeFallbackInEachLanguage(t *testing.T) {
	for _, c := range []struct{ node, en string }{
		{fmt.Sprintf(proto.FallbackShellLauncher, "kcc"), "Launcher kcc is a shell command on this computer; the Agent SDK can’t use its settings"},
		{proto.FallbackNoNode + "exec: \"node\": not found", "No Node.js 22 or later: exec: \"node\": not found"},
		{proto.FallbackNoClaude + "nope", "Can’t find claude: nope"},
		{proto.FallbackNoBridge, "This personad doesn’t include the Agent SDK bridge"},
		{proto.FallbackNoBridgeScript, "The Agent SDK bridge package has no bridge.mjs"},
		{proto.FallbackReadBridge + "x", "Couldn’t read the Agent SDK bridge: x"},
		{proto.FallbackWriteBridge + "y", "Couldn’t write the Agent SDK bridge: y"},
		{proto.FallbackNoBridge + "，还有别的", proto.FallbackNoBridge + "，还有别的"},
		{"别的原因", "别的原因"},
		{"", ""},
	} {
		s := proto.Session{RuntimeFallback: c.node}
		localizeSession(i18n.En, &s)
		if s.RuntimeFallback != c.en {
			t.Errorf("%q: %q", c.node, s.RuntimeFallback)
		}
		s = proto.Session{RuntimeFallback: c.node}
		localizeSession(i18n.ZhHans, &s)
		if s.RuntimeFallback != c.node {
			t.Errorf("zh %q: %q", c.node, s.RuntimeFallback)
		}
	}
}
