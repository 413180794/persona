//go:build windows

package netaddr

import (
	"context"
	"sync"

	"golang.org/x/sys/windows"
)

// The callback is one per process: Windows calls it on its own thread pool, and syscall.NewCallback slots are never
// freed. Each change goes to every Watch subscribed at the time.
var (
	interfaceCallback = windows.NewCallback(onInterfaceChange)
	subscribersMu     sync.Mutex
	subscribers       = map[chan struct{}]bool{}
)

func onInterfaceChange(_, _ uintptr, _ uint32) uintptr {
	subscribersMu.Lock()
	defer subscribersMu.Unlock()
	for ch := range subscribers {
		notify(ch)
	}
	return 0
}

// watchEvents subscribes to interface changes (an adapter coming up or going down, a new address on it), for IPv4
// and IPv6 at once. nil when the subscription fails: Watch then only polls.
func watchEvents(ctx context.Context) <-chan struct{} {
	out := make(chan struct{}, 1)
	subscribersMu.Lock()
	subscribers[out] = true
	subscribersMu.Unlock()
	forget := func() {
		subscribersMu.Lock()
		delete(subscribers, out)
		subscribersMu.Unlock()
	}
	var handle windows.Handle
	if err := windows.NotifyIpInterfaceChange(windows.AF_UNSPEC, interfaceCallback, nil, false, &handle); err != nil {
		forget()
		return nil
	}
	context.AfterFunc(ctx, func() {
		_ = windows.CancelMibChangeNotify2(handle) // waits for a callback in flight
		forget()
	})
	return out
}
