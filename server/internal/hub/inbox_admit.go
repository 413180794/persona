package hub

import (
	"context"
	"slices"
	"sort"
	"sync"
	"time"
)

// New items of a plugin's connector enter the inbox triaged (#1644): a collection keeps the items it starts aside
// (inboxBatch), and after the round the model is asked about a plugin connector's ones, in the background; only the
// ones that have a tier are listed and stored — the model's, or their fallback tier when the model can't sort them.
// One with no fallback is not written at all (no hidden rows): its connector answers Collect from its memory and
// hands it back next round, so it comes in once the model works again. Meanwhile GET /api/v1/inbox says how many wait
// and why (triage). The hub's own sources (mail, its connectors, a built-in plugin's) go in at once, as they always
// did, and are triaged afterwards (triageInbox).

// Bounds of one admission: new items start within admitWindow, each gets admitTimeout; the ones not started by then
// wait for the next round (asked for right away when this one got anywhere). Variables for tests.
var (
	admitWindow  = 20 * time.Second
	admitTimeout = 30 * time.Second
)

// inboxBatch is what one collection starts of plugin connectors: the new items by id and the source each came from,
// and the sources it read (with their names), whose held-back counts it recomputes.
type inboxBatch struct {
	items map[string]*InboxItem
	from  map[string]string
	read  map[string]string
}

func newInboxBatch() *inboxBatch {
	return &inboxBatch{items: map[string]*InboxItem{}, from: map[string]string{}, read: map[string]string{}}
}

// sortBatch files a round's new plugin items, under b.mu: muted ones (done at once) and ones with nothing to triage (a
// connector's rule decided, or no text to read) enter the list now; the others, newest first, wait for the model
// (todo) and leave changed.
func (b *inbox) sortBatch(batch *inboxBatch, changed []*InboxItem, now time.Time) (kept, todo []*InboxItem) {
	for _, it := range changed {
		if _, fresh := batch.items[it.ID]; !fresh {
			kept = append(kept, it)
		}
	}
	for _, it := range batch.items {
		if m := mutedBy(b.mutes, it); m != nil {
			it.done(now, "read")
			it.Reason = "已静音：" + m.Name
		} else if it.TriagedAt != it.Last && it.Context != "" {
			todo = append(todo, it)
			continue
		}
		b.items[it.ID] = it
		kept = append(kept, it)
	}
	sort.Slice(todo, func(i, j int) bool { return todo[i].UpdatedAt.After(todo[j].UpdatedAt) })
	return kept, todo
}

// admitInbox triages a round's new plugin items in the background, one admission at a time: a round that comes
// while one runs drops its own (they come back next round) and leaves the held-back counts as they are.
func (h *Hub) admitInbox(ctx context.Context, batch *inboxBatch, todo []*InboxItem) {
	if !h.inbox.admitting.TryLock() {
		return
	}
	if len(todo) == 0 {
		h.inbox.mu.Lock()
		h.recordHeld(batch, nil)
		h.inbox.mu.Unlock()
		h.inbox.admitting.Unlock()
		return
	}
	ctx = context.WithoutCancel(ctx) // the round (a pull to refresh) does not wait for it
	go func() {
		defer h.inbox.admitting.Unlock()
		h.admit(ctx, batch, todo)
	}()
}

// admit lets in the items that got a tier: the model's, or — when the model can't (not set up, held, failing, too
// slow) — their fallback tier with 「按规则」, for good (this version is not asked again when the model is back). The
// rest are dropped this round and counted as held back.
func (h *Hub) admit(ctx context.Context, batch *inboxBatch, todo []*InboxItem) {
	verdicts := make([]triageVerdict, len(todo))
	window, stop := context.WithTimeout(context.Background(), admitWindow)
	defer stop()
	sem := make(chan struct{}, triageConcurrency)
	var wg sync.WaitGroup
	for i, it := range todo {
		select {
		case sem <- struct{}{}:
		case <-window.Done():
		}
		if window.Err() != nil {
			for j := i; j < len(todo); j++ {
				verdicts[j] = triageVerdict{state: triageBehind, why: "这一轮没分完"}
			}
			break
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			_, verdicts[i] = h.askTriage(ctx, it, admitTimeout)
		}()
	}
	wg.Wait()

	var in []*InboxItem
	held := map[string]inboxHeld{}
	behind := false
	h.inbox.mu.Lock()
	for i, it := range todo {
		v := verdicts[i]
		fallback := ruleTier(it)
		switch {
		case v.state == "":
			it.Tier, it.Summary, it.Reason = v.tier, v.summary, v.reason
		case v.state != triageBehind && fallback != "":
			it.Tier, it.Reason = fallback, "按规则"
		default:
			src := batch.from[it.ID]
			x := held[src]
			x.Waiting++
			if x.State == "" || slices.Index(triageStates, v.state) < slices.Index(triageStates, x.State) {
				x.State, x.Reason, x.err = v.state, v.why, v.err
			}
			held[src] = x
			behind = behind || v.state == triageBehind
			continue
		}
		it.TriagedAt = it.Last
		h.inbox.items[it.ID] = it
		in = append(in, it)
	}
	h.recordHeld(batch, held)
	h.inbox.mu.Unlock()
	h.saveInbox(ctx, in)
	if len(in) > 0 {
		h.handInbox(ctx) // a new 需要你 may be the AI 分身's
	}
	if behind && len(in) > 0 {
		h.kickInbox() // more to sort, and the model is getting through them: the next round comes now
	}
}

// inboxHeld is a source's new items its last collection held back: not triaged, so not in the inbox yet.
type inboxHeld struct {
	Name    string
	Waiting int
	State   string // why, the most telling of theirs (triageStates)
	Reason  string // the same for people
	err     error
}

// recordHeld keeps a round's held-back counts, under h.inbox.mu: the sources it read get theirs (none: nothing
// waits); the others keep their last ones while they are still registered. A source's first hold (or a new reason)
// is logged once.
func (h *Hub) recordHeld(batch *inboxBatch, held map[string]inboxHeld) {
	next := map[string]inboxHeld{}
	for src, x := range h.inbox.held {
		if _, again := batch.read[src]; !again && h.connectors.Get(src) != nil {
			next[src] = x
		}
	}
	for src, x := range held {
		x.Name = batch.read[src]
		if prev, ok := h.inbox.held[src]; !ok || prev.State != x.State {
			why := x.Reason
			if x.err != nil {
				why += ": " + x.err.Error()
			}
			h.logf("inbox: %s: %d new item(s) held back, not triaged (%s)", src, x.Waiting, why)
		}
		next[src] = x
	}
	h.inbox.held = next
}

// InboxTriage is how triage stands for the apps (GET /api/v1/inbox triage): the new items held back by the last
// collection of each source, because the model could not sort them and they have no fallback tier, or because the
// round had more than it sorts. "ok" when none wait.
type InboxTriage struct {
	State   string              `json:"state"`            // ok | unconfigured | paused | error | timeout | behind
	Reason  string              `json:"reason,omitempty"` // the state for people: 模型没有额度
	Waiting int                 `json:"waiting"`
	Sources []InboxTriageSource `json:"sources,omitempty"`
}

// InboxTriageSource is one source's share.
type InboxTriageSource struct {
	Source  string `json:"source"`
	Name    string `json:"name,omitempty"`
	State   string `json:"state"`
	Reason  string `json:"reason,omitempty"`
	Waiting int    `json:"waiting"`
}

func (h *Hub) inboxTriage() InboxTriage {
	h.inbox.mu.Lock()
	defer h.inbox.mu.Unlock()
	out := InboxTriage{State: "ok"}
	for src, x := range h.inbox.held {
		out.Waiting += x.Waiting
		out.Sources = append(out.Sources, InboxTriageSource{Source: src, Name: x.Name, State: x.State, Reason: x.Reason, Waiting: x.Waiting})
		if out.State == "ok" || slices.Index(triageStates, x.State) < slices.Index(triageStates, out.State) {
			out.State, out.Reason = x.State, x.Reason
		}
	}
	sort.Slice(out.Sources, func(i, j int) bool { return out.Sources[i].Source < out.Sources[j].Source })
	return out
}
