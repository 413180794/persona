package node

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/rpc"
)

// A Claude Code screen with a message queued, as 2.1.319 draws it (#6088; the words made up).
const queuedScreen = `⏺ Bash(go test ./...)
  ⎿  Running…

✻ Waddling… (12s · ↓ 120 tokens)

❯ Also run the linter at the end.   ctrl+x ctrl+s to send now
────────────────────────────────────────
  Press up to edit queued messages · esc to interrupt`

const idleScreen = `⏺ All tests pass.

❯
────────────────────────────────────────
  ? for shortcuts`

// queueHost shows the queued screen until ctrl+s is pressed (unless stuck), then the screen with nothing queued.
type queueHost struct {
	bareHost
	mu    sync.Mutex
	sent  bool
	stuck bool
	first string
}

func (h *queueHost) Read(context.Context, host.PaneRef, host.ReadOpts) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.first != "" {
		return h.first, nil
	}
	if h.sent && !h.stuck {
		return idleScreen, nil
	}
	return queuedScreen, nil
}

func (h *queueHost) SendKeys(ctx context.Context, p host.PaneRef, keys ...string) error {
	h.mu.Lock()
	if slices.Contains(keys, "ctrl+s") {
		h.sent = true
	}
	h.mu.Unlock()
	return h.bareHost.SendKeys(ctx, p, keys...)
}

func codeOf(err error) string {
	var re *rpc.Error
	if errors.As(err, &re) {
		return re.Code
	}
	return ""
}

// Send now presses Claude Code's own send-now only while its screen offers it, and is done when the queue is gone.
func TestSendQueuedNow(t *testing.T) {
	defer func(d time.Duration) { sendNowWait = d }(sendNowWait)
	sendNowWait = 600 * time.Millisecond
	node := func(h *queueHost, agent string) *Node {
		return &Node{hosts: host.NewRegistry(h), agents: map[string]host.Agent{"s1": {Agent: agent, PaneID: "orca:t1:l1"}}, paths: map[string]string{}, metas: map[string]quickMeta{}, bgScans: map[string]*bgScan{}}
	}

	h := &queueHost{bareHost: bareHost{kind: host.Orca}}
	if err := node(h, "claude").sendQueuedNow(context.Background(), "s1"); err != nil {
		t.Fatalf("send now: %v", err)
	}
	if strings.Join(h.keys, " ") != "ctrl+x ctrl+s" {
		t.Fatalf("keys %q, want Claude Code's send-now", h.keys)
	}

	// nothing queued on the screen (the queue was taken a moment ago): no key at all
	h = &queueHost{bareHost: bareHost{kind: host.Orca}, first: idleScreen}
	if err := node(h, "claude").sendQueuedNow(context.Background(), "s1"); codeOf(err) != "nothing_queued" || len(h.keys) != 0 {
		t.Fatalf("nothing queued: %v, keys %q", err, h.keys)
	}

	// not a Claude: refused before looking
	h = &queueHost{bareHost: bareHost{kind: host.Orca}}
	if err := node(h, "codex").sendQueuedNow(context.Background(), "s1"); codeOf(err) != "send_now_unsupported" || len(h.keys) != 0 {
		t.Fatalf("codex: %v, keys %q", err, h.keys)
	}

	// the key went in and the queue stays on the screen: in doubt, delivery unknown
	h = &queueHost{bareHost: bareHost{kind: host.Orca}, stuck: true}
	err := node(h, "claude").sendQueuedNow(context.Background(), "s1")
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != "send_now_in_doubt" || !strings.Contains(string(re.Data), `"delivered":"unknown"`) {
		t.Fatalf("stuck: %v", err)
	}
}

func TestOffersSendNow(t *testing.T) {
	if !offersSendNow(queuedScreen) || offersSendNow(idleScreen) {
		t.Fatal("the hint is what tells a queued message")
	}
	if !offersSendNow("❯ x\n   ctrl+x   ctrl+s  to send   now") {
		t.Fatal("spacing as the terminal lays it out")
	}
}
