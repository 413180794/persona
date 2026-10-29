package node

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/herdr"
)

// The terminal tab's live stream (docs/terminal.md): while the hub watches a session's terminal, one
// `herdr terminal session observe` runs on its pane at the pane's own size and every frame goes to the hub as a
// terminal.frame event. The stream starts over — a new epoch, beginning with a whole frame — when the pane changes
// size, when herdr ends it (the pane moved, a live handoff) and when the diffs after the last whole frame pile up
// (the hub keeps them to replay to apps that reconnect).

const (
	// termReplayLimit: base64 bytes of diffs after a whole frame before a fresh observe (a new whole frame).
	termReplayLimit = 256 << 10
	// termSizeEvery: how often the pane's size is checked while streaming.
	termSizeEvery = 2 * time.Second
	// termGiveUp: failed starts in a row before the stream ends (the app falls back to reading the screen).
	termGiveUp = 4
)

type termStream struct {
	cancel context.CancelFunc
}

// observeSupport caches whether this herdr has `terminal session observe` (herdr 0.9 on Unix).
var observeSupport struct {
	once sync.Once
	ok   bool
}

func (n *Node) canObserve() bool {
	observeSupport.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		observeSupport.ok = exec.CommandContext(ctx, n.herdr.Bin, "terminal", "session", "observe", "--help").Run() == nil
	})
	return observeSupport.ok
}

// errNoTermStream: the hub falls back to the screen reads (old herdr, Windows).
var errNoTermStream = errors.New("unsupported: this herdr cannot stream a terminal (needs herdr terminal session observe)")

// terminalWatch starts streaming a session's terminal, or starts it over when it already streams (the hub asks again
// after it lost what it kept, and needs a whole frame). Returns the pane's size.
func (n *Node) terminalWatch(ctx context.Context, id string) (map[string]any, error) {
	if n.isAssistantSession(id) {
		return nil, fmt.Errorf("session %s has no terminal", id)
	}
	if !n.canObserve() {
		return nil, errNoTermStream
	}
	pane, err := n.paneFor(ctx, id)
	if err != nil {
		return nil, err
	}
	cols, rows, err := n.herdr.PaneSize(ctx, pane)
	if err != nil {
		return nil, err
	}
	sctx, cancel := context.WithCancel(context.Background())
	n.mu.Lock()
	if n.terms == nil {
		n.terms = map[string]*termStream{}
	}
	if old := n.terms[id]; old != nil {
		old.cancel()
	}
	s := &termStream{cancel: cancel}
	n.terms[id] = s
	n.mu.Unlock()
	go n.terminalLoop(sctx, id, s)
	return map[string]any{"cols": cols, "rows": rows}, nil
}

func (n *Node) terminalUnwatch(id string) {
	n.mu.Lock()
	s := n.terms[id]
	delete(n.terms, id)
	n.mu.Unlock()
	if s != nil {
		s.cancel()
	}
}

// terminalLoop runs observes one after another until the hub unwatches, or the session's terminal cannot be streamed
// any more (it stopped running): then a terminal.end tells the hub why.
func (n *Node) terminalLoop(ctx context.Context, id string, s *termStream) {
	fails := 0
	var why error
	for ctx.Err() == nil {
		frames, err := n.observeOnce(ctx, id)
		if ctx.Err() != nil {
			return
		}
		if frames > 0 {
			fails = 0
		} else {
			fails++
			why = err
		}
		if fails >= termGiveUp {
			break
		}
		select { // not a hot loop when herdr refuses at once
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(fails*fails)*250*time.Millisecond + 100*time.Millisecond):
		}
	}
	n.mu.Lock()
	mine := n.terms[id] == s
	if mine {
		delete(n.terms, id)
	}
	n.mu.Unlock()
	if mine && n.peer != nil {
		msg := "terminal stream ended"
		if why != nil {
			msg = why.Error()
		}
		_ = n.peer.Notify("terminal.end", map[string]any{"session_id": id, "error": msg})
	}
}

// observeOnce streams one epoch: from a whole frame until the pane changes size, herdr ends it or the replay is full.
// Returns how many frames went out.
func (n *Node) observeOnce(ctx context.Context, id string) (int, error) {
	pane, err := n.paneFor(ctx, id)
	if err != nil {
		return 0, err
	}
	cols, rows, err := n.herdr.PaneSize(ctx, pane)
	if err != nil {
		return 0, err
	}
	octx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		t := time.NewTicker(termSizeEvery)
		defer t.Stop()
		for {
			select {
			case <-octx.Done():
				return
			case <-t.C:
				if c, r, err := n.herdr.PaneSize(octx, pane); err == nil && (c != cols || r != rows) {
					cancel()
					return
				}
			}
		}
	}()
	epoch := strconv.FormatInt(time.Now().UnixNano(), 36)
	frames, replay := 0, 0
	err = n.herdr.Observe(octx, pane, cols, rows, func(f herdr.Frame) {
		frames++
		if f.Full {
			replay = 0
		} else {
			replay += len(f.Data)
		}
		if n.peer != nil {
			_ = n.peer.Notify("terminal.frame", map[string]any{"session_id": id, "epoch": epoch, "seq": frames,
				"full": f.Full, "cols": f.Width, "rows": f.Height, "data": f.Data})
		}
		if replay > termReplayLimit {
			cancel()
		}
	})
	return frames, err
}
