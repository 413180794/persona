package assistant

import (
	"slices"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connector"
)

// An older item moved back ("Move back") while a newer one of the same thread is open: the newer one gathers what comes
// next, every collection; the older one keeps what it held (map order picked either before, and the older one
// became a copy of the newer, #3796).
func TestReopenedItemDoesNotTakeTheThread(t *testing.T) {
	now := time.Now()
	at := func(d time.Duration) string { return now.Add(d).UTC().Format(time.RFC3339) }

	t.Run("mail", func(t *testing.T) {
		a := assistantwire.InboxMailAccount{ID: "a1", Email: "me@example.com"}
		for range 30 {
			b := newInbox()
			thread := mailThread("周报")
			old := &InboxItem{ID: "mail:a1:old:3", Source: "mail", Kind: KindMail, UpdatedAt: now.Add(-2 * time.Hour), Last: "3",
				Count: 1, Mail: &InboxMail{AccountID: "a1", Mailbox: "INBOX", Thread: thread, UIDs: []uint32{3}}}
			cur := &InboxItem{ID: "mail:a1:new:5", Source: "mail", Kind: KindMail, UpdatedAt: now.Add(-time.Hour), Last: "5",
				Count: 1, Mail: &InboxMail{AccountID: "a1", Mailbox: "INBOX", Thread: thread, UIDs: []uint32{5}}}
			b.items[old.ID], b.items[cur.ID] = old, cur
			// UID 3 is still unread on the server: it stays the older item's, not counted in the newer one too.
			b.applyMail(a, []assistantwire.InboxMailSummary{
				{Mailbox: "INBOX", UID: 3, Subject: "周报", Date: at(-2 * time.Hour)},
				{Mailbox: "INBOX", UID: 5, Subject: "Re: 周报", Date: at(-time.Hour)},
				{Mailbox: "INBOX", UID: 7, Subject: "Re: 周报", Date: at(-time.Minute)},
			}, now)
			if old.Last != "3" || !slices.Equal(old.Mail.UIDs, []uint32{3}) {
				t.Fatalf("the moved-back item at %q %v; want 3 [3]", old.Last, old.Mail.UIDs)
			}
			if cur.Last != "7" || cur.Count != 2 || !slices.Equal(cur.Mail.UIDs, []uint32{5, 7}) {
				t.Fatalf("the newer item at %q, count %d, %v; want 7, 2, [5 7]", cur.Last, cur.Count, cur.Mail.UIDs)
			}
		}
	})

	t.Run("connector", func(t *testing.T) {
		for range 30 {
			b := newInbox()
			old := &InboxItem{ID: "gh:old", Source: "github", UpdatedAt: now.Add(-2 * time.Hour), Last: "m1", Count: 1,
				Connector: &InboxConnector{Thread: "t1"}}
			cur := &InboxItem{ID: "gh:new", Source: "github", UpdatedAt: now.Add(-time.Hour), Last: "m2", Count: 1,
				Connector: &InboxConnector{Thread: "t1"}}
			b.items[old.ID], b.items[cur.ID] = old, cur
			b.applyConnector("github", "GitHub", []connector.InboxThing{{Thread: "t1", Last: "m3", At: now}}, now)
			if old.Last != "m1" || old.Count != 1 || cur.Last != "m3" || cur.Count != 2 {
				t.Fatalf("moved-back at %q (%d), newer at %q (%d); want m1 (1), m3 (2)", old.Last, old.Count, cur.Last, cur.Count)
			}
		}
	})
}
