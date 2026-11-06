package node

import (
	"context"
	"testing"
	"time"
)

func TestTerminalLoopCancelsContextOnNaturalEnd(t *testing.T) {
	for _, replaced := range []bool{false, true} {
		name := "current"
		if replaced {
			name = "replaced"
		}
		t.Run(name, func(t *testing.T) {
			connection, disconnect := context.WithTimeout(context.Background(), 30*time.Second)
			defer disconnect()
			ctx, cancel := context.WithCancel(connection)
			defer cancel()
			stream := &termStream{cancel: cancel}
			const id = "ended-session"
			n := &Node{terms: map[string]*termStream{id: stream}}
			var current *termStream
			var currentCtx context.Context
			if replaced {
				var stop context.CancelFunc
				currentCtx, stop = context.WithCancel(connection)
				defer stop()
				current = &termStream{cancel: stop}
				// Registration precedes cancellation of the old stream in setTermStream.
				n.terms[id] = current
			}

			// The pane is gone: exhaust the real retry backoff while the connection stays live.
			n.terminalLoop(ctx, id, stream)

			if err := connection.Err(); err != nil {
				t.Fatalf("observation did not end while the connection was live: %v", err)
			}
			if err := ctx.Err(); err != context.Canceled {
				t.Errorf("ended observation context = %v, want context.Canceled", err)
			}
			if got := n.termStreamOf(id); got != current {
				t.Errorf("stream entry = %p, want %p", got, current)
			}
			if currentCtx != nil && currentCtx.Err() != nil {
				t.Errorf("replacement context was canceled: %v", currentCtx.Err())
			}
		})
	}
}
