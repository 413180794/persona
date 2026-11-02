package link

import (
	"net/netip"
	"sync"
	"time"
)

// Failed authentication on the passive listener, counted per source: a pairing code or credential that is wrong counts,
// and a source with too many is turned away for a while, before its credential is even looked at (the hub does the
// same, hub/ratelimit.go). Every source counts, the LAN and loopback too: the hub is usually on the LAN. The numbers
// are settings, not measurements.
const (
	authFailures   = 10               // failures within authWindow that block a source
	authWindow     = time.Minute      // from a source's first failure in this round
	authBlock      = 15 * time.Minute // how long a blocked source waits
	authMaxSources = 4096             // sources tracked at once
)

type authBucket struct {
	failures int
	until    time.Time
	blocked  bool
}

type authLimiter struct {
	mu      sync.Mutex
	buckets map[string]authBucket
	// failures, window, block, max: the settings above unless a test changed them.
	failures      int
	window, block time.Duration
	max           int
}

func newAuthLimiter() *authLimiter {
	return &authLimiter{buckets: map[string]authBucket{}, failures: authFailures, window: authWindow, block: authBlock, max: authMaxSources}
}

// sourceKey is what a source counts as: an IPv4 address, or an IPv6 /64 (one home's or one machine's addresses).
func sourceKey(ip netip.Addr) string {
	ip = ip.Unmap().WithZone("")
	if !ip.IsValid() {
		return "unknown"
	}
	if ip.Is6() {
		return netip.PrefixFrom(ip, 64).Masked().String()
	}
	return ip.String()
}

// retry is how long a blocked source still has to wait; 0 when it may try.
func (l *authLimiter) retry(key string, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	switch {
	case !ok:
		return 0
	case !now.Before(b.until):
		delete(l.buckets, key)
		return 0
	case b.blocked:
		return b.until.Sub(now)
	}
	return 0
}

// fail counts a failure of key and says whether it is blocked now.
func (l *authLimiter) fail(key string, now time.Time) (blocked bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok && len(l.buckets) >= l.max {
		l.makeRoom(now)
	}
	if !now.Before(b.until) {
		b = authBucket{until: now.Add(l.window)}
	}
	b.failures++
	if b.failures >= l.failures && !b.blocked {
		b.blocked, b.until = true, now.Add(l.block)
	}
	l.buckets[key] = b
	return b.blocked
}

// makeRoom drops the sources that are done waiting, else the unblocked one that ends first, else the blocked one that
// ends first: someone cycling through many sources must not lock everyone else out.
func (l *authLimiter) makeRoom(now time.Time) {
	for key, b := range l.buckets {
		if !now.Before(b.until) {
			delete(l.buckets, key)
		}
	}
	if len(l.buckets) < l.max {
		return
	}
	victim, victimBlocked := "", true
	var until time.Time
	for key, b := range l.buckets {
		if victim == "" || (victimBlocked && !b.blocked) || (victimBlocked == b.blocked && b.until.Before(until)) {
			victim, victimBlocked, until = key, b.blocked, b.until
		}
	}
	delete(l.buckets, victim)
}
