package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"
)

// otherSave marks the context of the concurrent save in TestInboxUndoBeforeOtherSave.
type otherSave struct{}

// A request whose write fails undoes its change in memory. Another save of the same item (a triage round: it changes
// another field and does not stamp the item's mutation generation) may be queued behind the failed write; when the
// store has recovered by then, it writes the item as memory has it. That save must come after the undo, or the row
// keeps the change the request reported as not made while memory has dropped it, until a restart (#4924).
//
// The failing write lets the other save start (it waits for b.saving) and recovers the store; the request's
// announcement (inboxChanged, after b.saving) waits for the other save to finish, which is the widest the window gets.
func TestInboxUndoBeforeOtherSave(t *testing.T) {
	for _, tc := range []struct {
		name    string
		request func(h *inboxHub) bool // true when the request reports its write as failed
	}{
		{"done", func(h *inboxHub) bool {
			_, err := h.setInboxDone(context.Background(), "b", true, false)
			return err == errInboxNotPersisted
		}},
		{"bulk", func(h *inboxHub) bool {
			res := h.setInboxBulk(context.Background(), bulkDone, []inboxBulkTarget{{ID: "b"}}, nil, time.Time{}, time.Now())
			return len(res.Failed) == 1 && len(res.Changed) == 0
		}},
		{"parent", func(h *inboxHub) bool {
			_, _, err := h.setInboxParent(context.Background(), "b", &InboxParent{Kind: ParentInbox, ID: "a"})
			return err == errInboxNotPersisted
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, st := newBulkFailHub(t)
			seedFailItems(h, "a", "b")
			other := make(chan struct{})
			h.f.mu.Lock()
			h.f.inboxHook = func(ctx context.Context) {
				if ctx.Value(otherSave{}) != nil {
					return
				}
				select {
				case <-other:
				case <-time.After(5 * time.Second):
					t.Error("the other save never finished")
				}
			}
			h.f.mu.Unlock()
			st.failAll = true
			st.failHook = func(id string) {
				st.failHook, st.failAll = nil, false // the store is back for the next write
				go func() {
					defer close(other)
					h.inbox.mu.Lock()
					it := h.inbox.items[id]
					it.Title = "seen by triage"
					h.inbox.mu.Unlock()
					h.saveInbox(context.WithValue(context.Background(), otherSave{}, true), []*InboxItem{it})
				}()
			}
			if !tc.request(h) {
				t.Fatal("the request did not report its write as failed")
			}
			<-other
			h.inbox.mu.Lock()
			mem, _ := json.Marshal(h.inbox.items["b"])
			h.inbox.mu.Unlock()
			st.imu.Lock()
			row := st.rows["b"]
			st.imu.Unlock()
			if !bytes.Equal(mem, row) {
				t.Fatalf("memory and the stored row differ after the undo:\n memory: %s\n row:    %s", mem, row)
			}
		})
	}
}
