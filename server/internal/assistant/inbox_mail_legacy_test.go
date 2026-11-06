package assistant

import (
	"fmt"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

// An open item from before #4852, holding several senders' subject-less mail under the empty subject's key, is filed
// as superseded on the first round that sees its mail: each mail is then listed once, in its sender's item (#3796, #4920).
// Once all of its mail is read elsewhere it is read like any item.
func TestSubjectlessItemFromBeforeIsSuperseded(t *testing.T) {
	now := time.Now()
	d := now.Add(-time.Hour).Format(time.RFC3339)
	boss := assistantwire.MailAddress{Name: "老板", Email: "boss@example.com"}
	news := assistantwire.MailAddress{Name: "Newsletter", Email: "news@example.com"}
	legacy := func(b *inboxBook) *InboxItem {
		it := &InboxItem{ID: fmt.Sprintf("mail:acc:%s:1", subjectlessThread), Source: "mail", Kind: KindMail, Account: "acc", Tier: TierNoise,
			Last: "2", Count: 2, Mail: &InboxMail{AccountID: "acc", Mailbox: "INBOX", Thread: subjectlessThread, UIDs: []uint32{1, 2}}}
		b.items[it.ID] = it
		return it
	}

	b := newInbox()
	old := legacy(b)
	a := assistantwire.InboxMailAccount{ID: "acc", Email: "me@example.com", Messages: []assistantwire.InboxMailSummary{
		{Mailbox: "INBOX", UID: 1, From: boss, To: []assistantwire.MailAddress{{Email: "me@example.com"}}, Date: d, Snippet: "明天的会改到 9 点"},
		{Mailbox: "INBOX", UID: 2, From: news, Date: d, Snippet: "本周精选"},
	}}
	b.applyMail(a, a.Messages, now)
	if old.open() || old.DoneBy != "superseded" {
		t.Fatalf("the old subject-less item is still open: done_by %q", old.DoneBy)
	}
	listed := map[uint32]int{}
	for _, it := range b.items {
		if it.open() && it.Mail != nil {
			for _, uid := range it.Mail.UIDs {
				listed[uid]++
			}
		}
	}
	if listed[1] != 1 || listed[2] != 1 {
		t.Fatalf("each mail is listed once, got %v", listed)
	}

	b = newInbox()
	old = legacy(b)
	for i := range a.Messages {
		a.Messages[i].Seen = true
	}
	b.applyMail(a, a.Messages, now)
	if old.open() || old.DoneBy != "read" || !old.Read {
		t.Fatalf("read elsewhere, the old item: open %v, done_by %q", old.open(), old.DoneBy)
	}
}

func listedUIDs(b *inboxBook) map[uint32]int {
	out := map[uint32]int{}
	for _, it := range b.items {
		if it.open() && it.Mail != nil {
			for _, uid := range it.Mail.UIDs {
				out[uid]++
			}
		}
	}
	return out
}

// Review of #4926: mail with no sender stays listed (in the old item, kept open for it), and an old item the owner
// has to act on, or one the AI twin drafted for, keeps its mail, tier and draft instead of being filed silently.
func TestSubjectlessItemKeepsWhatCannotMove(t *testing.T) {
	now := time.Now()
	d := now.Add(-time.Hour).Format(time.RFC3339)
	boss := assistantwire.MailAddress{Name: "老板", Email: "boss@example.com"}
	a := assistantwire.InboxMailAccount{ID: "acc", Email: "me@example.com", Messages: []assistantwire.InboxMailSummary{
		{Mailbox: "INBOX", UID: 1, From: boss, Date: d, Snippet: "明天的会改到 9 点"},
		{Mailbox: "INBOX", UID: 2, Date: d, Snippet: "没有发件人的一封"},
	}}
	legacy := func(b *inboxBook) *InboxItem {
		it := &InboxItem{ID: fmt.Sprintf("mail:acc:%s:1", subjectlessThread), Source: "mail", Kind: KindMail, Account: "acc", Tier: TierNoise,
			Last: "2", Count: 2, Mail: &InboxMail{AccountID: "acc", Mailbox: "INBOX", Thread: subjectlessThread, UIDs: []uint32{1, 2}}}
		b.items[it.ID] = it
		return it
	}

	b := newInbox()
	old := legacy(b)
	b.applyMail(a, a.Messages, now)
	if got := listedUIDs(b); got[1] != 1 || got[2] != 1 {
		t.Fatalf("mail with and without a sender each listed once, got %v", got)
	}
	if !old.open() || len(old.Mail.UIDs) != 1 || old.Mail.UIDs[0] != 2 {
		t.Fatalf("the old item keeps the mail without a sender: open %v, uids %v", old.open(), old.Mail.UIDs)
	}

	for name, set := range map[string]func(*InboxItem){
		"需要你":     func(it *InboxItem) { it.Tier = TierAction },
		"drafted": func(it *InboxItem) { it.Agent = &InboxAgent{} },
	} {
		b := newInbox()
		old := legacy(b)
		set(old)
		a.Messages[1].From = assistantwire.MailAddress{Name: "Newsletter", Email: "news@example.com"}
		b.applyMail(a, a.Messages, now)
		if !old.open() || old.DoneBy != "" || len(old.Mail.UIDs) != 2 || (name == "需要你" && old.Tier != TierAction) || (name == "drafted" && old.Agent == nil) {
			t.Fatalf("%s: the old item was not kept: open %v, done_by %q, uids %v", name, old.open(), old.DoneBy, old.Mail.UIDs)
		}
		if got := listedUIDs(b); got[1] != 1 || got[2] != 1 {
			t.Fatalf("%s: each mail listed once, got %v", name, got)
		}
		a.Messages[1].From = assistantwire.MailAddress{}
	}
}

// On a hub that already ran #4852, the sender items hold the same mail as an old Needs you item that keeps it: they are
// filed, so each mail is still listed once.
func TestSubjectlessItemKeptOnAnUpgradedHub(t *testing.T) {
	now := time.Now()
	d := now.Add(-time.Hour).Format(time.RFC3339)
	a := assistantwire.InboxMailAccount{ID: "acc", Email: "me@example.com", Messages: []assistantwire.InboxMailSummary{
		{Mailbox: "INBOX", UID: 1, From: assistantwire.MailAddress{Email: "boss@example.com"}, Date: d, Snippet: "明天的会改到 9 点"},
		{Mailbox: "INBOX", UID: 2, From: assistantwire.MailAddress{Email: "news@example.com"}, Date: d, Snippet: "本周精选"},
	}}
	b := newInbox()
	b.applyMail(a, a.Messages, now.Add(-time.Minute)) // a round since #4852: an item per sender
	old := &InboxItem{ID: fmt.Sprintf("mail:acc:%s:1", subjectlessThread), Source: "mail", Kind: KindMail, Account: "acc", Tier: TierAction,
		Last: "2", Count: 2, Mail: &InboxMail{AccountID: "acc", Mailbox: "INBOX", Thread: subjectlessThread, UIDs: []uint32{1, 2}}}
	b.items[old.ID] = old
	b.applyMail(a, a.Messages, now)
	if got := listedUIDs(b); got[1] != 1 || got[2] != 1 || !old.open() || old.Tier != TierAction {
		t.Fatalf("listed %v, old open %v tier %q", got, old.open(), old.Tier)
	}
}
