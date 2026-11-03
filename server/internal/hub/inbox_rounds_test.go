package hub

import (
	"context"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/lark"
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

// A Feishu event collects the Feishu chats only: documents, removed and the other connectors wait for the poll.
func TestFeishuEventCollectsOnlyChats(t *testing.T) {
	base := time.Now().Add(-24 * time.Hour).Truncate(time.Hour)
	srv, requests := baselineLark(t, base)
	h := newTestHub(t)
	h.inbox.init()
	h.lark = lark.New(lark.Config{BaseURL: srv.URL, Token: "t"})
	h.connectors = connector.NewRegistry(nil)
	h.registerConnectors()
	other := &countingConnector{}
	if err := h.addConnector(other); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	collect := func(r inboxRound) map[string]int {
		before := lark-connectorCalls(requests())
		h.collectInbox(ctx, r)
		h.inbox.triaging.Lock() // triage runs in the background
		h.inbox.triaging.Unlock()
		return diffCalls(lark-connectorCalls(requests()), before)
	}

	event := collect(roundChats)
	if event["/conversations"] == 0 || event["/documents"] != 0 || event["/moments"] != 0 || other.n.Load() != 0 {
		t.Fatalf("event round read %v, the other connector %d times; want the chats only", event, other.n.Load())
	}
	poll := collect(roundPoll)
	if poll["/documents"] == 0 || poll["/moments"] == 0 || other.n.Load() != 1 {
		t.Fatalf("poll round read %v, the other connector %d times; want documents, removed and the connector", poll, other.n.Load())
	}
	// The same chats again: what an event costs from now on, against what it cost when every event was a poll.
	again, pollAgain := collect(roundChats), collect(roundPoll)
	t.Logf("lark-connector calls per Feishu event: %d (was %d); first rounds: event %v, poll %v", again["all"], pollAgain["all"], event, poll)
	if again["/documents"] != 0 || again["/moments"] != 0 || again["all"] >= pollAgain["all"] {
		t.Fatalf("event round %v, poll round %v", again, pollAgain)
	}
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
