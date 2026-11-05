package node

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"time"
)

// Another PermissionRequest hook's answer (#2648). Claude Code runs every PermissionRequest hook at once and goes
// with the first allow; the others are not stopped, they run until their own timeout. Persona's hook would then keep
// a card on the phone (the agent shown as waiting for you) for a call that already ran. A hook that answers tells the
// node over the socket (kind hook.decided, with the PermissionRequest input Claude Code gave it, the same one Persona's
// hook got): a card not shown yet is not shown, one shown is withdrawn.
//
// A card waits permissionGrace before it is shown, so that the other hook's word, a few milliseconds behind, keeps it
// off the phone altogether; only on a machine where some hook has said hook.decided since the node started.

// permissionGrace is how long a tool card waits for another hook's answer, once any hook here has given one.
var permissionGrace = time.Second

// decidedKeep is how long an answer is remembered for a card that asks after it.
const decidedKeep = 2 * time.Minute

type decisions struct {
	mu      sync.Mutex
	seen    bool                 // some hook said hook.decided since the node started
	done    map[string]time.Time // calls answered, by permissionKey
	waiting map[string]map[*int]context.CancelFunc
}

// permissionKey names one tool call: its session, its subagent, the tool and its input (decoded and encoded again,
// so a hook that passes it on re-encoded, with other spacing or escapes, names the same call).
func permissionKey(in hookInput) string {
	input := string(in.ToolInput)
	var v any
	if json.Unmarshal(in.ToolInput, &v) == nil {
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		if enc.Encode(v) == nil {
			input = b.String()
		}
	}
	return in.SessionID + "\x00" + in.AgentID + "\x00" + in.ToolName + "\x00" + input
}

// decide records that another hook answered the call in and withdraws a card waiting for it.
func (d *decisions) decide(in hookInput, now time.Time) {
	key := permissionKey(in)
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seen = true
	if d.done == nil {
		d.done = map[string]time.Time{}
	}
	for k, at := range d.done {
		if now.Sub(at) > decidedKeep {
			delete(d.done, k)
		}
	}
	d.done[key] = now
	for _, cancel := range d.waiting[key] {
		cancel()
	}
}

// answered says whether another hook answered key.
func (d *decisions) answered(key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.done[key]
	return ok
}

// watch returns a context that ends when another hook answers key (or ctx ends), and the function that stops watching.
func (d *decisions) watch(ctx context.Context, key string) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	id := new(int)
	d.mu.Lock()
	if d.waiting == nil {
		d.waiting = map[string]map[*int]context.CancelFunc{}
	}
	if d.waiting[key] == nil {
		d.waiting[key] = map[*int]context.CancelFunc{}
	}
	d.waiting[key][id] = cancel
	_, done := d.done[key]
	d.mu.Unlock()
	if done {
		cancel()
	}
	return ctx, func() {
		d.mu.Lock()
		delete(d.waiting[key], id)
		if len(d.waiting[key]) == 0 {
			delete(d.waiting, key)
		}
		d.mu.Unlock()
		cancel()
	}
}

// grace waits permissionGrace on a machine where hooks say hook.decided, and says whether key was answered by then
// (ctx is a watch's: it ends when it is).
func (d *decisions) grace(ctx context.Context, key string) bool {
	d.mu.Lock()
	seen := d.seen
	d.mu.Unlock()
	if seen {
		t := time.NewTimer(permissionGrace)
		defer t.Stop()
		select {
		case <-ctx.Done():
		case <-t.C:
		}
	}
	return d.answered(key)
}
