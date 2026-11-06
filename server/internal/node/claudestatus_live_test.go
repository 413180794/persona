//go:build herdrlive

package node

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/herdr"
)

// Against a working claude pane (read only): PERSONA_LIVE_PANE=<pane> go test -tags herdrlive -run TestLiveClaudeStatus
// ./internal/node/ -v — logs the status line parsed off the screen next to the screen's own line.
func TestLiveClaudeStatus(t *testing.T) {
	pane := os.Getenv("PERSONA_LIVE_PANE")
	if pane == "" {
		t.Skip("PERSONA_LIVE_PANE not set")
	}
	n := &Node{hosts: herdrHosts(herdr.New())}
	text, err := n.readPane(context.Background(), pane, "visible", liveLines)
	if err != nil {
		t.Fatal(err)
	}
	l, ok := parseClaudeStatus(text)
	for _, line := range strings.Split(text, "\n") {
		if statusLineRE.MatchString(line) {
			t.Logf("screen: %s", line)
		}
	}
	t.Logf("parsed: %+v %v", l, ok)
}
