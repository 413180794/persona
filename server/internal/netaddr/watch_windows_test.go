//go:build windows

package netaddr

import (
	"context"
	"testing"
	"time"
)

func TestWatchEventsSubscribesAndForgets(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	if watchEvents(ctx) == nil {
		t.Fatal("NotifyIpInterfaceChange failed: Watch would only poll")
	}
	onInterfaceChange(0, 0, 0) // what Windows calls on a change
	cancel()
	for i := 0; i < 100; i++ {
		subscribersMu.Lock()
		n := len(subscribers)
		subscribersMu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("subscriber kept after ctx ended")
}
