package assistant

import (
	"context"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
)

// New items of a plugin's connector enter the inbox triaged (#1644): a collection keeps the items it starts aside
// (inboxBatch), and after the round the model is asked about a plugin connector's ones, in the background; only the
// ones that have a tier are listed and stored — the model's, or their fallback tier when the model can't sort them.
// One with no fallback is not written at all (no hidden rows): its connector answers Collect from its memory and
// the hub hands it back next round, so it comes in once the model works again. Meanwhile GET /api/v1/inbox says how
// many wait and why (triage). The hub's own sources (mail, its connectors, a built-in plugin's) go in at once, as they
// always did, and are triaged afterwards (triageInbox).

// Bounds of one admission: new items start within admitWindow, each gets admitTimeout; the ones not started by then
// wait for the next round (asked for right away when this one got anywhere). Variables for tests.
var (
	admitWindow  = 20 * time.Second
	admitTimeout = 30 * time.Second
)

// inboxBatch is what one collection starts of plugin connectors: the new items by id and the source each came from,
// the sources it read (with their names), whose held-back counts it recomputes, and the connectors the hub had then
// (nil: not said, every source kept).
type inboxBatch struct {
	items      map[string]*InboxItem
	from       map[string]string
	read       map[string]string
	registered map[string]string
}

func newInboxBatch() *inboxBatch {
	return &inboxBatch{items: map[string]*InboxItem{}, from: map[string]string{}, read: map[string]string{}}
}

// sortBatch files a round's new plugin items, under b.mu: muted ones (done at once) and ones with nothing to triage (a
// connector's rule decided, or no text to read) enter the list now; the others, newest first, wait for the model
// (todo) and leave changed.
func (b *inboxBook) sortBatch(batch *inboxBatch, changed []*InboxItem, now time.Time) (kept, todo []*InboxItem) {
	for _, it := range changed {
		if _, fresh := batch.items[it.ID]; !fresh {
			kept = append(kept, it)
		}
	}
	for _, it := range batch.items {
		if m := mutedBy(b.mutes, it); m != nil {
			it.done(now, "read")
			it.Reason = b.mutedReason(m.Name)
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
func (s *Service) admitInbox(ctx context.Context, batch *inboxBatch, todo []*InboxItem) {
	b := &s.inbox
	if !b.admitting.TryLock() {
		return
	}
	if len(todo) == 0 {
		b.mu.Lock()
		changed := s.recordHeld(batch, nil)
		b.mu.Unlock()
		b.admitting.Unlock()
		if changed {
			s.inboxChanged(ctx, nil) // the triage status moved: the apps' banner follows
		}
		return
	}
	ctx = context.WithoutCancel(ctx) // the round (a pull to refresh) does not wait for it
	go func() {
		defer b.admitting.Unlock()
		s.admit(ctx, batch, todo)
	}()
}

// admit lets in the items that got a tier: the model's, or — when the model can't (not set up, held, failing, too
// slow) — their fallback tier with 「按规则」, for good (this version is not asked again when the model is back). The
// rest are dropped this round and counted as held back.
func (s *Service) admit(ctx context.Context, batch *inboxBatch, todo []*InboxItem) {
	b := &s.inbox
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
				verdicts[j] = triageVerdict{state: triageBehind, why: i18n.T(s.lang(ctx), "assistant-inbox_admit.behind")}
			}
			break
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			_, verdicts[i] = s.askTriage(ctx, it, admitTimeout)
		}()
	}
	wg.Wait()

	var in []*InboxItem
	held := map[string]inboxHeld{}
	behind := false
	b.mu.Lock()
	for i, it := range todo {
		v := verdicts[i]
		fallback := b.ruleTier(it)
		switch {
		case v.state == "":
			it.Tier, it.Summary, it.Reason = v.tier, v.summary, v.reason
		case v.state != triageBehind && fallback != "":
			it.Tier, it.Reason = fallback, b.byRules()
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
		b.items[it.ID] = it
		in = append(in, it)
	}
	changed := s.recordHeld(batch, held)
	b.mu.Unlock()
	s.saveInbox(ctx, in)
	if changed && len(in) == 0 {
		s.inboxChanged(ctx, nil) // nothing came in, but the triage status moved (saveInbox tells otherwise)
	}
	if len(in) > 0 {
		s.handInbox(ctx) // a new 需要你 may be the AI 分身's
	}
	if behind && len(in) > 0 {
		// More to sort, and the model is getting through them: the next round comes now.
		if err := s.appCall(ctx, AppInbox, inboxKick, map[string]any{}, nil); err != nil {
			s.logf("inbox: ask for the next round: %v", err)
		}
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

// recordHeld keeps a round's held-back counts, under b.mu: the sources it read get theirs (none: nothing waits);
// the others keep their last ones while the hub still has them. A source's first hold (or a new reason) is logged
// once. It says whether the status the apps see (GET /api/v1/inbox triage) changed.
func (s *Service) recordHeld(batch *inboxBatch, held map[string]inboxHeld) bool {
	b := &s.inbox
	next := map[string]inboxHeld{}
	for src, x := range b.held {
		if _, again := batch.read[src]; again {
			continue
		}
		if _, there := batch.registered[src]; batch.registered == nil || there {
			next[src] = x
		}
	}
	for src, x := range held {
		x.Name = batch.read[src]
		if prev, ok := b.held[src]; !ok || prev.State != x.State {
			why := x.Reason
			if x.err != nil {
				why += ": " + x.err.Error()
			}
			s.logf("inbox: %s: %d new item(s) held back, not triaged (%s)", src, x.Waiting, why)
		}
		next[src] = x
	}
	changed := len(next) != len(b.held)
	for src, x := range next {
		prev, ok := b.held[src]
		changed = changed || !ok || prev.Name != x.Name || prev.State != x.State || prev.Reason != x.Reason || prev.Waiting != x.Waiting
	}
	b.held = next
	return changed
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

func (s *Service) inboxTriage() InboxTriage {
	b := &s.inbox
	b.mu.Lock()
	defer b.mu.Unlock()
	out := InboxTriage{State: "ok"}
	for src, x := range b.held {
		out.Waiting += x.Waiting
		out.Sources = append(out.Sources, InboxTriageSource{Source: src, Name: x.Name, State: x.State, Reason: x.Reason, Waiting: x.Waiting})
		if out.State == "ok" || slices.Index(triageStates, x.State) < slices.Index(triageStates, out.State) {
			out.State, out.Reason = x.State, x.Reason
		}
	}
	sort.Slice(out.Sources, func(i, j int) bool { return out.Sources[i].Source < out.Sources[j].Source })
	return out
}
