package netaddr

import (
	"context"
	"slices"
	"time"
)

// These are scheduling settings, not measured response-time guarantees.
const (
	watchWindow  = time.Second
	pollInterval = time.Minute
)

// Watch signals that callers should run List again. An initial signal requests a
// baseline after the subscription is installed; call Watch before the first List
// to avoid a startup gap. Signals carry no addresses and may be coalesced.
//
// Linux subscribes to IPv4/IPv6 address and link changes. Every platform also
// polls snapshots as a recovery path; failed subscriptions fall back to polling.
// Events in a fixed window are merged (the timer is not reset by more events).
// The output buffers one pending refresh, never blocks a reader, and closes when
// ctx is cancelled. Errors do not publish empty snapshots: retry List on the next
// signal. The channel has no error payload, so callers needing diagnostics must
// report List errors themselves.
func Watch(ctx context.Context) <-chan struct{} {
	out := make(chan struct{}, 1)
	if ctx.Err() != nil {
		close(out)
		return out
	}
	events := watchEvents(ctx)
	go func() {
		defer close(out)
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		runWatch(ctx, out, events, ticker.C, List, watchWindow)
	}()
	return out
}

func notify(out chan<- struct{}) {
	select {
	case out <- struct{}{}:
	default:
	}
}

func runWatch(ctx context.Context, out chan<- struct{}, events <-chan struct{}, ticks <-chan time.Time, list func() ([]Addr, error), window time.Duration) {
	notify(out)
	previous, err := list()
	havePrevious := err == nil
	previous = comparableSnapshot(previous)
	var timer *time.Timer
	var ready <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	changed := func() {
		if ready == nil {
			timer = time.NewTimer(window)
			ready = timer.C
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-events:
			if !ok {
				events = nil
			} else {
				changed()
			}
		case <-ticks:
			current, err := list()
			if err != nil {
				changed()
				continue
			}
			current = comparableSnapshot(current)
			if !havePrevious || !slices.Equal(previous, current) {
				changed()
			}
			previous = current
			havePrevious = true
		case <-ready:
			notify(out)
			ready = nil
		}
	}
}

// Ignore countdown noise, but retain transitions to expired or infinite. Actual
// renewals still generate Linux notifications. Non-Linux lifetimes are unknown.
func comparableSnapshot(addrs []Addr) []Addr {
	out := slices.Clone(addrs)
	for i := range out {
		if out[i].PreferredLifetime != 0 && out[i].PreferredLifetime != InfiniteLifetime {
			out[i].PreferredLifetime = 1
		}
		if out[i].ValidLifetime != 0 && out[i].ValidLifetime != InfiniteLifetime {
			out[i].ValidLifetime = 1
		}
	}
	slices.SortFunc(out, compareIdentity)
	return out
}
