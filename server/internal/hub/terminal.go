package hub

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// terminals relays the terminal tab's live streams (docs/terminal.md). The first app to subscribe to a session's
// terminal makes its node stream it (terminal.watch); every frame fans out to the subscribers, and the frames since the
// latest whole one stay here, so an app that subscribes — or comes back after a dropped connection — starts from a
// whole frame, or continues after the last frame it has when its epoch is still the stream's. The last one to leave
// stops the stream. Any failure (node offline, stream ended, a node or herdr that cannot stream) drops the relay and
// tells its apps, which subscribe again later or read the screen instead (unsupported).
type terminals struct {
	mu     sync.Mutex
	relays map[[2]string]*termRelay
}

type termRelay struct {
	subs   map[*client]bool
	epoch  string
	frames []termFrame // from the latest whole frame on
}

type termFrame struct {
	Seq  int    `json:"seq"`
	Full bool   `json:"full"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
	Data string `json:"data"`
}

func termMessage(machineID, sessionID, epoch string, f termFrame) map[string]any {
	return map[string]any{"type": "terminal", "machine_id": machineID, "session_id": sessionID, "epoch": epoch,
		"seq": f.Seq, "full": f.Full, "cols": f.Cols, "rows": f.Rows, "data": f.Data}
}

// replay is what a subscriber holding (epoch, after) is missing: the frames after its last when it is on this stream
// and the kept frames still reach back to it, else everything from the whole frame.
func (r *termRelay) replay(epoch string, after int) []termFrame {
	if len(r.frames) == 0 {
		return nil
	}
	if epoch == r.epoch && after >= r.frames[0].Seq {
		for i, f := range r.frames {
			if f.Seq > after {
				return r.frames[i:]
			}
		}
		return nil
	}
	return r.frames
}

func (h *Hub) terminalSubscribe(ctx context.Context, c *client, machineID, sessionID, epoch string, after int) {
	m := h.byID[machineID]
	if m == nil {
		c.sendJSON(map[string]any{"type": "terminal", "machine_id": machineID, "session_id": sessionID, "error": "unknown machine"})
		return
	}
	key := [2]string{machineID, sessionID}
	t := &h.terms
	t.mu.Lock()
	if t.relays == nil {
		t.relays = map[[2]string]*termRelay{}
	}
	r := t.relays[key]
	first := r == nil
	if first {
		r = &termRelay{subs: map[*client]bool{}}
		t.relays[key] = r
	}
	r.subs[c] = true
	// Under the lock: the replay goes out before any frame that arrives meanwhile.
	for _, f := range r.replay(epoch, after) {
		c.sendTerm(termMessage(machineID, sessionID, r.epoch, f))
	}
	t.mu.Unlock()
	if !first {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		defer cancel()
		if err := m.call(ctx, "terminal.watch", map[string]any{"session_id": sessionID}, nil); err != nil {
			h.terminalEnded(machineID, sessionID, err.Error(), r)
		}
	}()
}

func (h *Hub) terminalUnsubscribe(c *client, machineID, sessionID string) {
	key := [2]string{machineID, sessionID}
	t := &h.terms
	t.mu.Lock()
	r := t.relays[key]
	if r == nil || !r.subs[c] {
		t.mu.Unlock()
		return
	}
	delete(r.subs, c)
	last := len(r.subs) == 0
	if last {
		delete(t.relays, key)
	}
	t.mu.Unlock()
	if m := h.byID[machineID]; m != nil && last {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = m.call(ctx, "terminal.unwatch", map[string]any{"session_id": sessionID}, nil)
		}()
	}
}

// terminalDrop unsubscribes a client that went away from every terminal it watched.
func (h *Hub) terminalDrop(c *client) {
	t := &h.terms
	t.mu.Lock()
	var keys [][2]string
	for k, r := range t.relays {
		if r.subs[c] {
			keys = append(keys, k)
		}
	}
	t.mu.Unlock()
	for _, k := range keys {
		h.terminalUnsubscribe(c, k[0], k[1])
	}
}

// onTerminalFrame keeps a node's frame and sends it to the session's subscribers. A new epoch starts with a whole
// frame; a diff with nothing to apply to (the whole frame was never seen) is dropped.
func (h *Hub) onTerminalFrame(machineID string, params json.RawMessage) {
	var p struct {
		SessionID string `json:"session_id"`
		Epoch     string `json:"epoch"`
		termFrame
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	t := &h.terms
	t.mu.Lock()
	defer t.mu.Unlock()
	r := t.relays[[2]string{machineID, p.SessionID}]
	if r == nil {
		return
	}
	if p.Epoch != r.epoch {
		if !p.Full {
			return
		}
		r.epoch = p.Epoch
	}
	if p.Full {
		r.frames = []termFrame{p.termFrame}
	} else if len(r.frames) > 0 {
		r.frames = append(r.frames, p.termFrame)
	} else {
		return
	}
	msg := termMessage(machineID, p.SessionID, r.epoch, p.termFrame)
	for c := range r.subs {
		c.sendTerm(msg)
	}
}

// terminalEnded tells a relay's apps its stream stopped, and drops it. only: the relay the failure is about (nil:
// whichever is there), so a late failure of an old stream leaves a new one alone.
func (h *Hub) terminalEnded(machineID, sessionID, why string, only *termRelay) {
	key := [2]string{machineID, sessionID}
	t := &h.terms
	t.mu.Lock()
	r := t.relays[key]
	if r == nil || (only != nil && r != only) {
		t.mu.Unlock()
		return
	}
	delete(t.relays, key)
	t.mu.Unlock()
	msg := map[string]any{"type": "terminal", "machine_id": machineID, "session_id": sessionID, "error": why,
		// an old node ("unknown method") or herdr: read the screen instead of trying again
		"unsupported": strings.Contains(why, "unknown method") || strings.HasPrefix(why, "unsupported")}
	for c := range r.subs {
		c.sendTerm(msg)
	}
}

// sendTerm queues a terminal message without ever blocking (it runs under the relays' lock): a client whose queue is
// full is dropped — a missing diff would leave its screen wrong — and continues from its last frame when it reconnects.
func (c *client) sendTerm(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	select {
	case c.out <- b:
	default:
		go c.conn.Close(websocket.StatusPolicyViolation, "too slow")
	}
}

// terminalsOffline ends every stream of a machine whose node went away.
func (h *Hub) terminalsOffline(machineID string) {
	t := &h.terms
	t.mu.Lock()
	var ids []string
	for k := range t.relays {
		if k[0] == machineID {
			ids = append(ids, k[1])
		}
	}
	t.mu.Unlock()
	for _, id := range ids {
		h.terminalEnded(machineID, id, "电脑离线", nil)
	}
}
