package node

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/tunnel"
	"github.com/tailscale/tailcat"
	"tailscale.com/types/key"
)

// A machine that starts without a network (or cannot reach DERP) must get a start error back, which retryTailcat
// turns into a backoff; it must not crash the resident node. Draining the endpoint of a server that never started
// used to panic (tailcat's DrainTCP reads the netstack, which only exists once the server is up).
func TestServeTailcatOnceStartFailureReturnsError(t *testing.T) {
	derpMap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "offline", http.StatusServiceUnavailable)
	}))
	defer derpMap.Close()
	k := &tunnel.NodeKey{
		Key:          key.NewNode(),
		PresharedKey: tailcat.NewPresharedKey(),
		RegionID:     900,
		DERPMapURL:   derpMap.URL,
		Hubs:         []key.NodePublic{key.NewNode().Public()},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conns := make(chan net.Conn)
	err := serveTailcatOnce(ctx, k, log.New(io.Discard, "", 0), "", conns, func() {})
	if err == nil || !strings.HasPrefix(err.Error(), "start: ") {
		t.Fatalf("serveTailcatOnce = %v, want a start error", err)
	}
}
