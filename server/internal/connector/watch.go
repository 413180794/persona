package connector

import (
	"context"
	"time"
)

// Backoff between restarts of a failing Watch: doubling from the first to the last, back to the first once a Watch
// has run for a while.
var (
	watchBackoffFirst = 5 * time.Second
	watchBackoffLast  = 5 * time.Minute
	watchHealthy      = time.Minute
)

// Start runs every connector registered so far until ctx ends: its Run, and its Watch, calling changed with the
// connector's id and the scope; a Watch that returns is started again after a backoff (its error is logged as the
// connector wrote it — connectors keep secrets out of their errors).
func (r *Registry) Start(ctx context.Context, changed func(id, scope string), logf func(string, ...any)) {
	for _, c := range r.All() {
		if run, ok := c.(Runner); ok {
			go run.Run(ctx)
		}
		if e, ok := c.(Events); ok {
			go watchOne(ctx, c.ID(), e, changed, logf)
		}
	}
}

func watchOne(ctx context.Context, id string, e Events, changed func(id, scope string), logf func(string, ...any)) {
	wait := watchBackoffFirst
	for ctx.Err() == nil {
		start := time.Now()
		err := e.Watch(ctx, func(scope string) { changed(id, scope) })
		if ctx.Err() != nil {
			return
		}
		if time.Since(start) >= watchHealthy {
			wait = watchBackoffFirst
		}
		logf("connector %s: watch stopped (%v), again in %s", id, err, wait)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, watchBackoffLast)
	}
}
