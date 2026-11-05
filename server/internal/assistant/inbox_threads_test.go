package assistant

import (
	"encoding/json"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connector"
)

// A collection finds a thread by its source (and Feishu account), the item a thing earlier in the same collection
// started included, and a thread whose blocks a thing moved by its new blocks.
func TestApplyFindsThreads(t *testing.T) {
	now := time.Now()
	larkBlock := func(account int, feed string) *connector.ThingExtra {
		block, _ := json.Marshal(InboxLark{AccountID: account, FeedID: feed})
		return &connector.ThingExtra{Lark: block}
	}

	t.Run("connector", func(t *testing.T) {
		b := &inboxBook{}
		b.init()
		b.items["other"] = &InboxItem{ID: "other", Source: "other", UpdatedAt: now, Last: "m0",
			Connector: &InboxConnector{Thread: "t1"}}
		changed := b.applyConnector("github", "GitHub", []connector.InboxThing{
			{Thread: "t1", Last: "m1", At: now},
			{Thread: "t1", Last: "m2", At: now.Add(time.Second)},
			{Thread: "t2", Last: "m1", At: now},
		}, now)
		var t1 []*InboxItem
		for _, it := range b.items {
			if it.Source == "github" && it.Connector.Thread == "t1" {
				t1 = append(t1, it)
			}
		}
		if len(t1) != 1 || t1[0].Count != 2 || t1[0].Last != "m2" {
			t.Fatalf("t1 items %+v, want one item that gathered both moves", t1)
		}
		if len(b.items) != 3 || len(changed) != 3 || b.items["other"].Last != "m0" {
			t.Fatalf("%d items, %d changed, other source's at %q", len(b.items), len(changed), b.items["other"].Last)
		}
	})

	t.Run("feishu", func(t *testing.T) {
		b := &inboxBook{}
		b.init()
		done := now.Add(-time.Minute)
		b.items["a1"] = &InboxItem{ID: "a1", Source: "lark", UpdatedAt: now, Last: "m1",
			Lark: &InboxLark{AccountID: 1, FeedID: "f"}}
		b.items["a2"] = &InboxItem{ID: "a2", Source: "lark", UpdatedAt: done, Last: "m1", DoneAt: &done,
			Lark: &InboxLark{AccountID: 2, FeedID: "f"}}
		b.applyMigrated("lark", []connector.InboxThing{
			{Thread: "chat:f", Last: "m2", At: now, Extra: larkBlock(1, "f")},
			{Thread: "chat:2:f", Last: "m1", At: now, Extra: larkBlock(2, "f")}, // done, nothing new: no item
			{Thread: "chat:g", Last: "m1", At: now, Extra: larkBlock(1, "g")},
			{Thread: "chat:g", Last: "m2", At: now.Add(time.Second), Extra: larkBlock(1, "g")},
			{Thread: "chat:g", Last: "m9", At: now, Extra: &connector.ThingExtra{Source: "mail2lark", Lark: larkBlock(1, "g").Lark}},
		}, now)
		if a1 := b.items["a1"]; a1.Last != "m2" || !a1.open() {
			t.Fatalf("a1 %+v, want it to gather chat:f's move", a1)
		}
		bySource := map[string]int{}
		for _, it := range b.items {
			if migratedThread(it) == "chat:g" {
				bySource[it.Source]++
				if it.Source == "lark" && it.Last != "m2" {
					t.Fatalf("chat:g at %q, want the later move gathered", it.Last)
				}
			}
		}
		if len(b.items) != 4 || bySource["lark"] != 1 || bySource["mail2lark"] != 1 {
			t.Fatalf("%d items, chat:g by source %v; want a1, a2 and one chat:g item per source", len(b.items), bySource)
		}
	})

	t.Run("mail", func(t *testing.T) { // the applyMail side (the hub's test had none): one account's threads only
		b := &inboxBook{}
		b.init()
		other := &InboxItem{ID: "mail:a2:x", Source: "mail", Kind: KindMail, UpdatedAt: now, Last: "1",
			Mail: &InboxMail{AccountID: "a2", Mailbox: "INBOX", Thread: mailThread("周报"), UIDs: []uint32{1}}}
		b.items[other.ID] = other
		at := now.UTC().Format(time.RFC3339)
		b.applyMail(assistantwire.InboxMailAccount{ID: "a1", Email: "me@example.com"}, []assistantwire.InboxMailSummary{
			{Mailbox: "INBOX", UID: 3, Subject: "周报", Date: at},
			{Mailbox: "INBOX", UID: 5, Subject: "Re: 周报", Date: at},
		}, now)
		var a1 []*InboxItem
		for _, it := range b.items {
			if it.Mail != nil && it.Mail.AccountID == "a1" {
				a1 = append(a1, it)
			}
		}
		if len(a1) != 1 || a1[0].Last != "5" || len(a1[0].Mail.UIDs) != 2 || other.Last != "1" || len(b.items) != 2 {
			t.Fatalf("a1's items %+v, the other account's at %q, %d items", a1, other.Last, len(b.items))
		}
		// The next listing has one more: the open item gathers it.
		b.applyMail(assistantwire.InboxMailAccount{ID: "a1", Email: "me@example.com"}, []assistantwire.InboxMailSummary{
			{Mailbox: "INBOX", UID: 3, Subject: "周报", Date: at},
			{Mailbox: "INBOX", UID: 5, Subject: "Re: 周报", Date: at},
			{Mailbox: "INBOX", UID: 7, Subject: "Re: 周报", Date: at},
		}, now)
		if len(b.items) != 2 || a1[0].Last != "7" {
			t.Fatalf("%d items, a1's at %q", len(b.items), a1[0].Last)
		}
	})

	t.Run("moved blocks", func(t *testing.T) {
		b := &inboxBook{}
		b.init()
		b.items["x"] = &InboxItem{ID: "x", Source: "lark", UpdatedAt: now, Last: "m1",
			Lark: &InboxLark{AccountID: 1, FeedID: "old"}}
		b.applyMigrated("lark", []connector.InboxThing{
			{Thread: "chat:old", Last: "m2", At: now, Extra: larkBlock(1, "new")}, // its block names chat:new now
			{Thread: "chat:new", Last: "m3", At: now.Add(time.Second), Extra: larkBlock(1, "new")},
			{Thread: "chat:old", Last: "m4", At: now.Add(2 * time.Second), Extra: larkBlock(1, "old")},
		}, now)
		if x := b.items["x"]; x.Last != "m3" || migratedThread(x) != "chat:new" {
			t.Fatalf("x at %q in %s, want chat:new's later move", x.Last, migratedThread(x))
		}
		if len(b.items) != 2 {
			t.Fatalf("%d items, want x and a new one for chat:old", len(b.items))
		}
	})
}

// A collection looks each thing's thread up in an index of the inbox, not by going over the whole inbox for each:
// a Feishu collection of a few hundred chats against a week's inbox made a thread name of every item for every chat.
func TestApplyMigratedCost(t *testing.T) {
	now := time.Now()
	b := benchInbox(2000, now)
	var chats []connector.InboxThing
	for i := range 300 {
		block, _ := json.Marshal(InboxLark{AccountID: 2, FeedID: fmt.Sprintf("feed%d", i)})
		chats = append(chats, connector.InboxThing{Thread: fmt.Sprintf("chat:2:feed%d", i), Last: "m1", At: now,
			Extra: &connector.ThingExtra{Lark: block}})
	}
	b.applyMigrated("lark", chats, now) // warm
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	b.applyMigrated("lark", chats, now)
	runtime.ReadMemStats(&after)
	// A name for each item once, and each chat's own work (its blocks compared): linear, not items × chats.
	items, most := len(b.items), uint64(4*len(b.items)+100*len(chats))
	if allocs := after.Mallocs - before.Mallocs; allocs > most {
		t.Fatalf("a collection of %d chats against %d items allocates %d times, want at most %d", len(chats), items, allocs, most)
	}
}
