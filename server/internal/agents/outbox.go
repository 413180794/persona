package agents

import (
	"context"
	"sort"
	"sync"
	"time"
)

// KV is the hub's settings store (one JSON value per key); nil keeps the outbox in memory only.
type KV interface {
	Setting(ctx context.Context, key string, out any) (bool, error)
	PutSetting(ctx context.Context, key string, v any) error
}

const outboxKey = "agents.outbox"
const outboxMax = 500

// Outbox states.
const (
	OutSending = "sending" // handed to the source, no answer yet (a retry reuses the clientId)
	OutSent    = "sent"    // the source accepted it
	OutFailed  = "failed"  // the source refused or was unreachable; retrying with the same clientId is safe
	OutDeleted = "deleted"
)

// OutboxEntry is the audit row of one send: what went where, from whom, and how it ended. Only a preview of the
// text is kept.
type OutboxEntry struct {
	ClientID  string    `json:"clientId"`
	SessionID string    `json:"sessionId"`
	Preview   string    `json:"preview"`
	Length    int       `json:"length"`
	Origin    string    `json:"origin,omitempty"` // app, assistant, …
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	Attempts  int       `json:"attempts"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type outbox struct {
	mu     sync.Mutex
	kv     KV
	rows   map[string]*OutboxEntry
	loaded bool
}

func (o *outbox) load(ctx context.Context) {
	if o.loaded {
		return
	}
	o.loaded = true
	if o.rows == nil {
		o.rows = map[string]*OutboxEntry{}
	}
	if o.kv == nil {
		return
	}
	var list []*OutboxEntry
	if ok, err := o.kv.Setting(ctx, outboxKey, &list); err == nil && ok {
		for _, e := range list {
			o.rows[e.ClientID] = e
		}
	}
}

func (o *outbox) get(ctx context.Context, clientID string) (OutboxEntry, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.load(ctx)
	e, ok := o.rows[clientID]
	if !ok {
		return OutboxEntry{}, false
	}
	return *e, true
}

func (o *outbox) put(ctx context.Context, e OutboxEntry) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.load(ctx)
	o.rows[e.ClientID] = &e
	list := o.listLocked()
	if len(list) > outboxMax {
		for _, old := range list[outboxMax:] {
			delete(o.rows, old.ClientID)
		}
		list = list[:outboxMax]
	}
	if o.kv == nil {
		return nil
	}
	return o.kv.PutSetting(ctx, outboxKey, list)
}

// listLocked is newest first.
func (o *outbox) listLocked() []*OutboxEntry {
	list := make([]*OutboxEntry, 0, len(o.rows))
	for _, e := range o.rows {
		list = append(list, e)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.After(list[j].CreatedAt) })
	return list
}

func (o *outbox) list(ctx context.Context, sessionID string, limit int) []OutboxEntry {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.load(ctx)
	out := []OutboxEntry{}
	for _, e := range o.listLocked() {
		if sessionID == "" || e.SessionID == sessionID {
			out = append(out, *e)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out
}
