package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"time"
)

// A write whose row failed must not leave memory ahead of the database (#4366): a restart would read the old row,
// and until then the apps and the AI twin would act on a state that was never kept. inboxWrite snapshots an item
// before a change and, when its row could not be written, gives it back exactly what the row still holds, but only
// while nothing else moved the item since the change (its stored form and its mutation stamp are still the ones the
// change left). A concurrent move wins: its own save writes the row.

// inboxWrite is one item's change on its way to the database.
type inboxWrite struct {
	it      *InboxItem
	prev    []byte // the item's stored form before the change; nil for an item the change created
	told    int    // the agent's in-memory note count before the change
	applied []byte // the stored form the change left (settle)
	gen     uint64 // the mutation stamp settle took
	undone  bool   // its row failed and the change was undone (saveWrites)
}

// itemState is an item's stored form, leaving out the arrival stamp the save itself sets (arrived). Under b.mu.
func itemState(it *InboxItem) []byte {
	c := *it
	c.ArrivedOf, c.ArrivedAt = "", time.Time{}
	raw, _ := json.Marshal(&c)
	return raw
}

// beginWrite snapshots an item before a change; newWrite stands for an item the change creates. Under b.mu.
func beginWrite(it *InboxItem) *inboxWrite {
	w := &inboxWrite{it: it, prev: itemState(it)}
	if it.Agent != nil {
		w.told = it.Agent.told
	}
	return w
}

func newWrite(it *InboxItem) *inboxWrite { return &inboxWrite{it: it} }

// settle records what the change left and stamps the item. Under b.mu, right after the change.
func (b *inboxBook) settle(w *inboxWrite) {
	w.applied = itemState(w.it)
	w.gen = b.touched(w.it.ID)
}

// holds says the item is still the one this change left. Under b.mu.
func (b *inboxBook) holds(w *inboxWrite) bool {
	return b.items[w.it.ID] == w.it && b.mark[w.it.ID] == w.gen && bytes.Equal(itemState(w.it), w.applied)
}

// restore gives the item back what it held before the change; an item the change created is forgotten. Pointers whose value did not change are kept, as are the agent and handling records
// themselves (copied into), so code holding them (deliverNotes compares it.Agent) still finds them. Under b.mu.
func (b *inboxBook) restore(w *inboxWrite) {
	if w.prev == nil {
		delete(b.items, w.it.ID)
		return
	}
	var p InboxItem
	if err := json.Unmarshal(w.prev, &p); err != nil {
		return
	}
	cur, agent, handling := w.it, w.it.Agent, w.it.Handling
	cv, pv := reflect.ValueOf(cur).Elem(), reflect.ValueOf(&p).Elem()
	for i := range cv.NumField() {
		switch name := cv.Type().Field(i).Name; name {
		case "ArrivedOf", "ArrivedAt": // the save's own stamp, not the change's
		default:
			if !reflect.DeepEqual(cv.Field(i).Interface(), pv.Field(i).Interface()) {
				cv.Field(i).Set(pv.Field(i))
			}
		}
	}
	if agent != nil && p.Agent != nil {
		*agent = *p.Agent
		cur.Agent = agent
	}
	if cur.Agent != nil {
		cur.Agent.told = w.told
	}
	if handling != nil && p.Handling != nil {
		*handling = *p.Handling
		cur.Handling = handling
	}
}

// saveWrites persists the items of settled writes and restores, inside the saving window (saveInboxUndo), each one
// whose row failed while it still holds what its change left; what names the change in the log. It returns the
// failed ids.
func (s *Service) saveWrites(ctx context.Context, ws []*inboxWrite, what string) map[string]bool {
	if len(ws) == 0 {
		return nil
	}
	return s.saveInboxUndo(ctx, itemsOf(ws), s.undoWrites(ws, what))
}

// undoWrites is saveInboxUndo's undo for writes saved along with other items.
func (s *Service) undoWrites(ws []*inboxWrite, what string) func(failed map[string]bool) {
	b := &s.inbox
	return func(failed map[string]bool) {
		for _, w := range ws {
			if failed[w.it.ID] && b.holds(w) {
				b.restore(w)
				w.undone = true
				s.logf("inbox: %s: %s could not be saved; it is undone and comes again on the next pass", w.it.ID, what)
			}
		}
	}
}
