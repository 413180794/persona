package hub

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/rpc"
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
	// What the node is asked about a session's terminal (watch, unwatch) goes one call at a time: each looks at what
	// is wanted when its turn comes, so the last one leaves the node as the hub wants.
	calls map[[2]string]*sync.Mutex
}

func (t *terminals) callLock(key [2]string) *sync.Mutex {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.calls == nil {
		t.calls = map[[2]string]*sync.Mutex{}
	}
	if t.calls[key] == nil {
		t.calls[key] = &sync.Mutex{}
	}
	return t.calls[key]
}

type termRelay struct {
	subs   map[*client]bool
	epoch  string
	frames []termFrame // from the latest whole frame on

	// The app driving the terminal at its size (nil: the stream is of the pane at its own size) and that size. What
	// the node runs follows these: every change starts a sync (terminalSync).
	controller *client
	ctl        termCtl
	takeover   bool
	synced     *termCtl // what the node runs: controlled at that size, or (nil) observing if observing
	observing  bool
}

// termCtl is a controlled terminal's grid.
type termCtl struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
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

// terminalSubscribe adds an app to a session's terminal. ctl: the app wants to drive it at that grid (the phone's
// size); takeover: even if another program holds the terminal's attach slot (after the app asked its user).
func (h *Hub) terminalSubscribe(ctx context.Context, c *client, machineID, sessionID, epoch string, after int, ctl *termCtl, takeover bool) {
	m := h.localMachine(machineID)
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
	sync := first
	switch {
	case ctl != nil && r.controller != c:
		if old := r.controller; old != nil { // the newest device drives; the one before watches
			old.sendTerm(termControlMessage(machineID, sessionID, "taken", nil, "另一台设备接管了这个终端"))
		}
		r.controller, r.ctl, r.takeover, sync = c, *ctl, takeover, true
	case ctl != nil: // the same app again (it lost frames, or reconnected): continue, at its size now
		if takeover && !r.takeover {
			r.takeover, sync = true, true
		}
		if *ctl != r.ctl {
			r.ctl = *ctl
			if r.synced != nil {
				h.terminalNotify(m, "terminal.resize", map[string]any{"session_id": sessionID, "cols": ctl.Cols, "rows": ctl.Rows})
			}
		}
	case r.controller == c: // it only watches now
		r.controller, sync = nil, true
	}
	// Under the lock: the replay goes out before any frame that arrives meanwhile. A sync starts the stream over.
	if !sync {
		for _, f := range r.replay(epoch, after) {
			c.sendTerm(termMessage(machineID, sessionID, r.epoch, f))
		}
	}
	t.mu.Unlock()
	if sync {
		go h.terminalSync(context.WithoutCancel(ctx), m, key, r)
	}
}

func termControlMessage(machineID, sessionID, state string, ctl *termCtl, why string) map[string]any {
	msg := map[string]any{"type": "terminal_control", "machine_id": machineID, "session_id": sessionID, "state": state}
	if ctl != nil {
		msg["cols"], msg["rows"] = ctl.Cols, ctl.Rows
	}
	if why != "" {
		msg["error"] = why
	}
	return msg
}

// terminalSync makes the node stream the relay as it now wants: driven at the controller's size, or watched at the
// pane's. The controller hears how that went (terminal_control): active, busy (another program holds the terminal:
// the app asks its user before it takes over; meanwhile it watches), unsupported (an old node or herdr: it watches).
func (h *Hub) terminalSync(ctx context.Context, m *Machine, key [2]string, r *termRelay) {
	mu := h.terms.callLock(key)
	mu.Lock()
	defer mu.Unlock()
	for h.terminalSyncOnce(ctx, m, key, r) {
	}
}

// terminalSyncOnce is one watch call; true when what is wanted changed meanwhile (sync again). A relay gone by then
// has nothing to sync.
func (h *Hub) terminalSyncOnce(ctx context.Context, m *Machine, key [2]string, r *termRelay) bool {
	t := &h.terms
	t.mu.Lock()
	if t.relays[key] != r {
		t.mu.Unlock()
		return false
	}
	controller, ctl, takeover := r.controller, r.ctl, r.takeover
	if controller == nil && r.observing ||
		controller != nil && r.synced != nil && *r.synced == ctl && !takeover {
		t.mu.Unlock() // the node already streams it so
		return false
	}
	t.mu.Unlock()

	params := map[string]any{"session_id": key[1]}
	if controller != nil {
		params["control"] = map[string]any{"cols": ctl.Cols, "rows": ctl.Rows, "takeover": takeover}
	}
	var res struct {
		Control *bool `json:"control"`
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	err := m.call(cctx, "terminal.watch", params, &res)
	cancel()

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.relays[key] != r {
		return false
	}
	var rerr *rpc.Error
	switch {
	case controller != nil && errors.As(err, &rerr) && rerr.Code == "attach_busy":
		if r.controller == controller {
			r.controller, r.takeover = nil, false
		}
		r.synced, r.observing = nil, false // the node stopped watching to try: watch again
		controller.sendTerm(termControlMessage(key[0], key[1], "busy", nil, "电脑上另一个程序正在使用这个终端"))
		return true
	case err != nil:
		go h.terminalEnded(key[0], key[1], err.Error(), r)
		return false
	case controller != nil && (res.Control == nil || !*res.Control):
		if r.controller == controller {
			r.controller, r.takeover = nil, false
		}
		r.synced, r.observing = nil, true // it ignored control and watches
		controller.sendTerm(termControlMessage(key[0], key[1], "unsupported", nil, "这台电脑的 Persona 或 herdr 太旧，只能看不能接管"))
	case controller != nil:
		r.synced, r.observing = &termCtl{ctl.Cols, ctl.Rows}, false
		if r.controller == controller {
			r.takeover = false
		}
		controller.sendTerm(termControlMessage(key[0], key[1], "active", &ctl, ""))
		if r.controller == controller && r.ctl != ctl { // it resized while the node was starting
			r.synced = &termCtl{r.ctl.Cols, r.ctl.Rows}
			h.terminalNotify(m, "terminal.resize", map[string]any{"session_id": key[1], "cols": r.ctl.Cols, "rows": r.ctl.Rows})
		}
	default:
		r.synced, r.observing = nil, true
	}
	return r.controller != controller || r.controller != nil && r.takeover
}

// terminalNotify sends the node an event about a terminal it streams (keys, sizes, the lease); events keep their order.
func (h *Hub) terminalNotify(m *Machine, event string, params map[string]any) {
	go func() { _ = m.notify(event, params) }()
}

// terminalInput forwards what the driving app typed: bytes for the terminal, as they are; and the wheel (scroll lines,
// > 0 back in history) over a cell of its screen, at ([column, row]; older apps do not say, the node takes the middle).
// From an app that does not drive it, nothing.
func (h *Hub) terminalInput(c *client, machineID, sessionID, text string, ctl *termCtl, scroll int, at []int) {
	m := h.localMachine(machineID)
	if m == nil {
		return
	}
	t := &h.terms
	t.mu.Lock()
	r := t.relays[[2]string{machineID, sessionID}]
	if r == nil || r.controller != c {
		t.mu.Unlock()
		return
	}
	resize := false
	if ctl != nil && *ctl != r.ctl && ctl.Cols > 0 && ctl.Rows > 0 {
		r.ctl = *ctl
		if r.synced != nil {
			r.synced = &termCtl{ctl.Cols, ctl.Rows}
			resize = true
		}
	}
	live := r.synced != nil
	t.mu.Unlock()
	// Written here, not from a goroutine: the app's messages come in order and so must the keys.
	if resize {
		_ = m.notify("terminal.resize", map[string]any{"session_id": sessionID, "cols": ctl.Cols, "rows": ctl.Rows})
	}
	if text != "" && live {
		_ = m.notify("terminal.input", map[string]any{"session_id": sessionID, "text": text})
	}
	if scroll != 0 && live {
		lines := scroll
		if lines < 0 {
			lines = -lines
		}
		params := map[string]any{"session_id": sessionID, "lines": lines, "up": scroll > 0}
		if len(at) == 2 && at[0] >= 0 && at[1] >= 0 {
			params["column"], params["row"] = at[0], at[1]
		}
		_ = m.notify("terminal.scroll", params)
	}
}

// terminalClick is a tap on a terminal the app drives, to its node in order with the keys (the driver's only).
func (h *Hub) terminalClick(c *client, machineID, sessionID string, column, row int) {
	m := h.localMachine(machineID)
	if m == nil || column < 0 || row < 0 {
		return
	}
	t := &h.terms
	t.mu.Lock()
	r := t.relays[[2]string{machineID, sessionID}]
	live := r != nil && r.controller == c && r.synced != nil
	t.mu.Unlock()
	if live {
		_ = m.notify("terminal.click", map[string]any{"session_id": sessionID, "column": column, "row": row})
	}
}

// terminalKeepalive renews the node's lease on every terminal an app drives; a node that stops hearing it lets the
// terminal go (docs/terminal.md).
func (h *Hub) terminalKeepalive(ctx context.Context) {
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		byMachine := map[string][]string{}
		t := &h.terms
		t.mu.Lock()
		for k, r := range t.relays {
			if r.controller != nil && r.synced != nil {
				byMachine[k[0]] = append(byMachine[k[0]], k[1])
			}
		}
		t.mu.Unlock()
		for mid, ids := range byMachine {
			if m := h.localMachine(mid); m != nil {
				_ = m.notify("terminal.keepalive", map[string]any{"session_ids": ids})
			}
		}
	}
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
	drove := r.controller == c
	if drove {
		r.controller = nil // the pane goes back to its size; whoever still watches sees it there
	}
	t.mu.Unlock()
	m := h.localMachine(machineID)
	if m == nil {
		return
	}
	if last {
		go h.terminalUnwatch(m, key)
	} else if drove {
		go h.terminalSync(context.Background(), m, key, r)
	}
}

// terminalUnwatch stops the node's stream of a relay that went, after any watch on its way; one subscribed again
// meanwhile keeps it (its own watch replaces the stream).
func (h *Hub) terminalUnwatch(m *Machine, key [2]string) {
	t := &h.terms
	mu := t.callLock(key)
	mu.Lock()
	defer mu.Unlock()
	t.mu.Lock()
	again := t.relays[key] != nil
	t.mu.Unlock()
	if again {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = m.call(ctx, "terminal.unwatch", map[string]any{"session_id": key[1]}, nil)
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

// terminalControlEnded: the node stopped driving a terminal on its own — someone took the attach slot over (taken),
// or its lease ran out. The app that drove it hears why and watches it from now on.
func (h *Hub) terminalControlEnded(machineID, sessionID, why string, taken bool) {
	key := [2]string{machineID, sessionID}
	t := &h.terms
	t.mu.Lock()
	r := t.relays[key]
	if r == nil || r.synced == nil {
		t.mu.Unlock()
		return
	}
	state, text := "lost", "终端已回到电脑上的尺寸"
	if taken {
		state, text = "taken", "电脑上另一个程序接管了这个终端"
	}
	if c := r.controller; c != nil {
		c.sendTerm(termControlMessage(machineID, sessionID, state, nil, text))
	}
	r.controller, r.synced, r.observing = nil, nil, false
	t.mu.Unlock()
	h.logf("[%s] terminal %s: control ended: %s", machineID, sessionID, why)
	if m := h.localMachine(machineID); m != nil {
		go h.terminalSync(context.Background(), m, key, r)
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
	if m := h.localMachine(machineID); m != nil && r.synced != nil {
		go h.terminalUnwatch(m, key) // the node may still drive it (a watch that timed out here but started there)
	}
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
		if c.conn != nil {
			go c.conn.Close(websocket.StatusPolicyViolation, "too slow")
		}
	}
}

// terminalsOffline ends every stream of a machine whose node went away.
func (h *Hub) terminalsOffline(machineID string) { h.terminalsEnded(machineID, "电脑离线") }

// terminalsEnded ends every terminal stream of the machine, saying why.
func (h *Hub) terminalsEnded(machineID, why string) {
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
		h.terminalEnded(machineID, id, why, nil)
	}
}
