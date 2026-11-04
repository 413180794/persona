package hub

import (
	"context"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nwverse/persona/server/internal/connector"
)

// countingConnector counts its inbox collections.
type countingConnector struct{ n atomic.Int32 }

func (*countingConnector) ID() string   { return "example" }
func (*countingConnector) Name() string { return "Example" }
func (c *countingConnector) Collect(context.Context) ([]connector.InboxThing, error) {
	c.n.Add(1)
	return nil, nil
}
func (*countingConnector) Reply(context.Context, connector.InboxReply) error { return nil }
func (*countingConnector) Manifest() connector.Manifest {
	return connector.Manifest{Capabilities: []connector.Capability{connector.CapInbox}}
}

// lark-connectorCalls counts the fake lark-connector's requests by path prefix ("GET /documents"), from its sorted lines with
// their "  ×n" repeat counts.
func lark-connectorCalls(lines []string) map[string]int {
	out := map[string]int{}
	for _, l := range lines {
		n := 1
		if i := strings.LastIndex(l, "  ×"); i >= 0 {
			n, _ = strconv.Atoi(l[i+len("  ×"):])
			l = l[:i]
		}
		path := strings.SplitN(strings.TrimPrefix(l, "GET "), "?", 2)[0]
		out["all"] += n
		for _, p := range []string{"/conversations", "/documents", "/moments"} {
			if strings.HasPrefix(path, p) {
				out[p] += n
			}
		}
	}
	return out
}

func diffCalls(after, before map[string]int) map[string]int {
	out := map[string]int{}
	for k, v := range after {
		if d := v - before[k]; d != 0 {
			out[k] = d
		}
	}
	return out
}

// Feishu's kicks ask for the chats; a connector's kick (or one meanwhile) still asks for every source.
func TestInboxKickRounds(t *testing.T) {
	h := newTestHub(t)
	h.kickLarkChats()
	<-h.inbox.kick
	if r := h.inbox.kickedRound(); r != roundChats {
		t.Fatalf("Feishu kick: round %d", r)
	}
	h.kickLarkChats()
	h.kickInbox()
	h.kickLarkChats()
	<-h.inbox.kick
	select {
	case <-h.inbox.kick:
		t.Fatal("three kicks in a burst signalled twice")
	default:
	}
	if r := h.inbox.kickedRound(); r != roundPoll {
		t.Fatalf("Feishu and connector kicks: round %d", r)
	}
	h.kickLarkChats()
	<-h.inbox.kick
	if r := h.inbox.kickedRound(); r != roundChats {
		t.Fatalf("after a wide round, a Feishu kick: round %d", r)
	}
}
