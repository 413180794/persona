package node

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/chyroc/jarvis/server/internal/herdr"
	"github.com/chyroc/jarvis/server/internal/rpc"
)

// The terminal tab's live stream (docs/terminal.md): while the hub watches a session's terminal, one
// `herdr terminal session observe` runs on its pane at the pane's own size and every frame goes to the hub as a
// terminal.frame event. The stream starts over — a new epoch, beginning with a whole frame — when the pane changes
// size, when herdr ends it (the pane moved, a live handoff) and when the diffs after the last whole frame pile up
// (the hub keeps them to replay to apps that reconnect).
//
// A phone that drives the terminal takes it instead (`herdr terminal session control` at the phone's grid): the
// pane's programs see the phone's size and lay themselves out for it, the frames are the same kind, and the phone's
// keys go straight in (terminal.input). Letting go — the hub says so, the lease runs out, this process dies — puts
// the pane back to its own size (herdr does that when the control client goes, however it goes).

const (
	// termReplayLimit: base64 bytes of diffs after a whole frame before a fresh observe (a new whole frame).
	termReplayLimit = 256 << 10
	// termSizeEvery: how often the pane's size is checked while streaming.
	termSizeEvery = 2 * time.Second
	// termGiveUp: failed starts in a row before the stream ends (the app falls back to reading the screen).
	termGiveUp = 4
)

// termMinGap: frames closer together than this go out as one. herdr emits a frame per change up to every ~16 ms
// (60 a second: a build log, vim scrolling); the phone takes ~12 ms a frame, so it gets at most ~33. Claude Code
// redraws about every 110 ms while it streams, which passes untouched; the first frame after a quiet spell goes out
// at once (typing echoes are not held back).
var termMinGap = 30 * time.Millisecond

// frameGate merges frames that come within termMinGap of the last one sent: the diffs' bytes run on (they are VT
// output, applied in order), a whole frame drops the diffs before it. What is held goes out when the gap is up.
type frameGate struct {
	mu      sync.Mutex
	send    func(herdr.Frame)
	last    time.Time
	pending *herdr.Frame
	timer   *time.Timer
}

func (g *frameGate) put(f herdr.Frame) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.pending == nil {
		if wait := termMinGap - time.Since(g.last); wait > 0 {
			g.pending = &f
			g.timer = time.AfterFunc(wait, g.flush)
			return
		}
		g.last = time.Now()
		g.send(f)
		return
	}
	if f.Full {
		*g.pending = f
		return
	}
	g.pending.Data = joinFrameData(g.pending.Data, f.Data)
	g.pending.Width, g.pending.Height = f.Width, f.Height
}

// flush sends what is held (when the gap is up, and when the stream ends).
func (g *frameGate) flush() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.timer != nil {
		g.timer.Stop()
		g.timer = nil
	}
	if g.pending != nil {
		g.last = time.Now()
		g.send(*g.pending)
		g.pending = nil
	}
}

func joinFrameData(a, b string) string {
	x, err1 := base64.StdEncoding.DecodeString(a)
	y, err2 := base64.StdEncoding.DecodeString(b)
	if err1 != nil || err2 != nil {
		return a + b // not ours to fix; keep both
	}
	return base64.StdEncoding.EncodeToString(append(x, y...))
}

type termStream struct {
	cancel context.CancelFunc

	// The phone driving the terminal at its own size (nil: observing). Anything the hub sends about it renews the
	// lease; one that runs out lets the terminal go, in case the hub lost track of it.
	mu    sync.Mutex
	ctl   *herdr.Control
	lease time.Time
}

// termLease: a controlled terminal goes back to its own size when the hub said nothing about it for this long (it
// sends terminal.keepalive every 20 s while the phone holds it).
var termLease = 75 * time.Second

func (s *termStream) control() *herdr.Control {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lease = time.Now()
	return s.ctl
}

func (s *termStream) leaseOver() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Since(s.lease) > termLease
}

// observeSupport, controlSupport cache whether this herdr has `terminal session observe` / `control` (herdr 0.9 on
// Unix).
var observeSupport, controlSupport struct {
	once sync.Once
	ok   bool
}

func (n *Node) herdrHas(cache *struct {
	once sync.Once
	ok   bool
}, sub string) bool {
	cache.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cache.ok = exec.CommandContext(ctx, n.herdr.Bin, "terminal", "session", sub, "--help").Run() == nil
	})
	return cache.ok
}

func (n *Node) canObserve() bool { return n.herdrHas(&observeSupport, "observe") }
func (n *Node) canControl() bool { return n.herdrHas(&controlSupport, "control") }

// errNoTermStream: the hub falls back to the screen reads (old herdr, Windows).
var errNoTermStream = errors.New("unsupported: this herdr cannot stream a terminal (needs herdr terminal session observe)")

// termControl is what the phone asks for to drive the terminal: its grid, and whether to take the attach slot from
// whoever holds it.
type termControl struct {
	Cols     int  `json:"cols"`
	Rows     int  `json:"rows"`
	Takeover bool `json:"takeover"`
}

// terminalWatch starts streaming a session's terminal, or starts it over when it already streams (the hub asks again
// after it lost what it kept, and needs a whole frame). With ctl the phone takes the terminal at its size
// (terminal session control) instead of watching it at the pane's; an attach slot held by another client is
// attach_busy, unless ctl.Takeover. Returns the size streamed and whether it is controlled (an old node leaves that
// out: the hub then knows the phone only watches).
func (n *Node) terminalWatch(ctx context.Context, id string, ctl *termControl) (map[string]any, error) {
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
	if ctl != nil && ctl.Cols > 0 && ctl.Rows > 0 && n.canControl() {
		return n.terminalControl(ctx, id, pane, *ctl)
	}
	cols, rows, err := n.herdr.PaneSize(ctx, pane)
	if err != nil {
		return nil, err
	}
	sctx, cancel := context.WithCancel(context.Background())
	s := &termStream{cancel: cancel}
	n.setTermStream(id, s)
	go n.terminalLoop(sctx, id, s)
	return map[string]any{"cols": cols, "rows": rows, "control": false}, nil
}

// setTermStream makes s the session's stream, stopping the one before it.
func (n *Node) setTermStream(id string, s *termStream) {
	n.mu.Lock()
	if n.terms == nil {
		n.terms = map[string]*termStream{}
	}
	old := n.terms[id]
	if s == nil {
		delete(n.terms, id)
	} else {
		n.terms[id] = s
	}
	n.mu.Unlock()
	if old != nil && old != s {
		old.cancel()
	}
}

func (n *Node) terminalUnwatch(id string) { n.setTermStream(id, nil) }

func (n *Node) termStreamOf(id string) *termStream {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.terms[id]
}

// terminalControl takes the session's terminal at the phone's size. The stream before it (an observe) stops first,
// so its frames are not mixed into the new epoch; when the slot is busy the hub starts an observe again.
func (n *Node) terminalControl(ctx context.Context, id, pane string, p termControl) (map[string]any, error) {
	if old := n.termStreamOf(id); old != nil {
		old.mu.Lock()
		prev := old.ctl
		old.mu.Unlock()
		n.setTermStream(id, nil)
		if prev != nil { // ours holds the slot until it is gone: taking it again too soon finds it busy
			select {
			case <-prev.Done():
			case <-time.After(3 * time.Second):
			}
		}
	}
	sctx, cancel := context.WithCancel(context.Background())
	s := &termStream{cancel: cancel, lease: time.Now()}
	epoch := strconv.FormatInt(time.Now().UnixNano(), 36)
	frames, replay := 0, 0
	gate := &frameGate{send: func(f herdr.Frame) {
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
			// A resize to the same size: herdr sends a whole frame, the programs see no change (no SIGWINCH).
			replay = 0
			if c := s.control(); c != nil {
				go func() { _ = c.Resize(f.Width, f.Height) }()
			}
		}
	}}
	c, err := n.herdr.StartControl(sctx, pane, p.Cols, p.Rows, p.Takeover, gate.put)
	if err != nil {
		cancel()
		if errors.Is(err, herdr.ErrAttachBusy) {
			return nil, &rpc.Error{Code: "attach_busy", Message: err.Error()}
		}
		return nil, err
	}
	s.mu.Lock()
	s.ctl = c
	s.mu.Unlock()
	n.setTermStream(id, s)
	go n.controlLoop(sctx, id, s, gate)
	return map[string]any{"cols": p.Cols, "rows": p.Rows, "control": true}, nil
}

// controlLoop holds the terminal until the hub lets it go (unwatch, or a watch that replaces it), the lease runs out
// or herdr ends it (someone took the slot over, the pane went away): then terminal.end tells the hub, which goes
// back to watching.
func (n *Node) controlLoop(ctx context.Context, id string, s *termStream, gate *frameGate) {
	c := s.control()
	defer c.Close()
	t := time.NewTicker(termLease / 5)
	defer t.Stop()
	var why error
	for why == nil {
		select {
		case <-ctx.Done():
			gate.flush()
			return
		case <-c.Done():
			why = c.Err()
		case <-t.C:
			if s.leaseOver() {
				why = errors.New("the hub stopped renewing the terminal control")
			}
		}
	}
	gate.flush()
	n.mu.Lock()
	mine := n.terms[id] == s
	if mine {
		delete(n.terms, id)
	}
	n.mu.Unlock()
	s.cancel()
	if mine && n.peer != nil {
		_ = n.peer.Notify("terminal.end", map[string]any{"session_id": id, "error": why.Error(), "control": true,
			"taken": errors.Is(why, herdr.ErrTakenOver)})
	}
}

// onTerminalEvent handles what the hub forwards from the phone driving a terminal; events come in order, so keys do.
func (n *Node) onTerminalEvent(event string, params json.RawMessage) {
	var p struct {
		SessionID  string   `json:"session_id"`
		SessionIDs []string `json:"session_ids"`
		Text       string   `json:"text"`
		Cols       int      `json:"cols"`
		Rows       int      `json:"rows"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	switch event {
	case "terminal.input":
		if s := n.termStreamOf(p.SessionID); s != nil {
			if c := s.control(); c != nil && p.Text != "" {
				_ = c.Input(p.Text)
			}
		}
	case "terminal.resize":
		if s := n.termStreamOf(p.SessionID); s != nil {
			if c := s.control(); c != nil && p.Cols > 0 && p.Rows > 0 {
				_ = c.Resize(p.Cols, p.Rows)
			}
		}
	case "terminal.keepalive":
		for _, id := range p.SessionIDs {
			if s := n.termStreamOf(id); s != nil {
				s.control()
			}
		}
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
	gate := &frameGate{send: func(f herdr.Frame) {
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
	}}
	err = n.herdr.Observe(octx, pane, cols, rows, gate.put)
	gate.flush()
	gate.mu.Lock() // a flush the timer started may still be sending
	sent := frames
	gate.mu.Unlock()
	return sent, err
}
