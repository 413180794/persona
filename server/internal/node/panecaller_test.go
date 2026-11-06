package node

import (
	"testing"

	"github.com/nwverse/persona/server/internal/host"
)

// An agent's MCP call names no session: the node takes the one its pane runs as the host shows it now, else as the
// hooks bound it; a stale binding does not win over the live agent.
func TestPaneCaller(t *testing.T) {
	n := &Node{panes: map[string]string{"w1:p1": "sess-hook", "w1:p2": "sess-stale"},
		agents: map[string]host.Agent{"sess-host": {PaneID: "w1:p2"}}}
	for pane, want := range map[string]string{"w1:p1": "sess-hook", "w1:p2": "sess-host", "w1:p3": "", "": ""} {
		if got := n.paneCaller(pane); got != want {
			t.Errorf("paneCaller(%q) = %q, want %q", pane, got, want)
		}
	}
}
