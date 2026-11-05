package connector

import (
	"context"
	"time"
)

// Backoff between restarts of a failing Watch: doubling from the first to the last, back to the first once a Watch
// has run for a while. Start reads them once, so a test that changes them does not race with the watches of a
// registry started earlier.
var (
	watchBackoffFirst = 5 * time.Second
	watchBackoffLast  = 5 * time.Minute
	watchHealthy      = time.Minute
)

// Start runs every connector registered so far that is switched on, until ctx ends: its Run, and its Watch, calling
// changed with the connector's id and the scope; a Watch that returns is started again after a backoff (its error is
// logged as the connector wrote it — connectors keep secrets out of their errors). Each runs under its own context:
// switching it off cancels it, switching it on starts it again (SetEnabled).
func (r *Registry) Start(ctx context.Context, changed func(id string, scope Scope), logf func(string, ...any)) {
	r.mu.Lock()
	r.run = &starter{ctx: ctx, changed: changed, logf: logf,
		backoff: watchBackoff{first: watchBackoffFirst, last: watchBackoffLast, healthy: watchHealthy}}
	r.mu.Unlock()
	for _, c := range r.All() {
		r.startOne(c)
	}
}

// startOne runs c unless Start has not been called, it is switched off or it runs already.
func (r *Registry) startOne(c Connector) {
	id := c.ID()
	r.mu.Lock()
	if r.run == nil || r.off[id] || r.stop[id] != nil {
		r.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(r.run.ctx)
	r.stop[id] = cancel
	run := r.run
	r.mu.Unlock()
	if rn, ok := c.(Runner); ok {
		go rn.Run(ctx)
	}
	if e, ok := c.(Events); ok {
		go watchOne(ctx, id, e, run.changed, run.logf, run.backoff)
	}
}

// watchBackoff is the restart backoff of the Watches of one Start.
type watchBackoff struct{ first, last, healthy time.Duration }

func watchOne(ctx context.Context, id string, e Events, changed func(id string, scope Scope), logf func(string, ...any),
	b watchBackoff) {
	wait := b.first
	for ctx.Err() == nil {
		start := time.Now()
		err := e.Watch(ctx, func(scope Scope) { changed(id, scope) })
		if ctx.Err() != nil {
			return
		}
		if time.Since(start) >= b.healthy {
			wait = b.first
		}
		logf("connector %s: watch stopped (%v), again in %s", id, err, wait)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, b.last)
	}
}
