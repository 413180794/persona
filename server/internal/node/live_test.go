//go:build herdrlive

package node

import (
	"context"
	"os"
	"testing"

	"github.com/chyroc/jarvis/server/internal/herdr"
)

// Against a real claude pane: JARVIS_LIVE_PANE=<pane> JARVIS_LIVE_WANT=<code or ""> [JARVIS_LIVE_CLEAR=<draft>]
// go test -tags herdrlive -run TestLiveCheckPane ./internal/node/
func TestLiveCheckPane(t *testing.T) {
	pane := os.Getenv("JARVIS_LIVE_PANE")
	if pane == "" {
		t.Skip("JARVIS_LIVE_PANE not set")
	}
	n := &Node{herdr: herdr.New()}
	err := n.checkPane(context.Background(), pane, "claude", os.Getenv("JARVIS_LIVE_CLEAR"))
	code := ""
	if err != nil {
		code, _ = refusalOf(t, err)
	}
	t.Logf("pane %s: %v", pane, err)
	if want := os.Getenv("JARVIS_LIVE_WANT"); code != want {
		t.Fatalf("code %q, want %q", code, want)
	}
}
