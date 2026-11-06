package assistant

import (
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

// Mail with no subject (or only Re: / Fwd:) is not one thread for the whole account: one person's subject-less mail
// is not folded under another's, where the newest sender, preview and Direct would hide it (#4852). The same sender's
// stay together, and a subject still groups replies as before.
func TestMailWithoutASubjectIsGroupedBySender(t *testing.T) {
	b := newInbox()
	now := time.Now()
	d := now.Add(-time.Hour).Format(time.RFC3339)
	me := []assistantwire.MailAddress{{Email: "me@example.com"}}
	boss := assistantwire.MailAddress{Name: "老板", Email: "boss@example.com"}
	news := assistantwire.MailAddress{Name: "Newsletter", Email: "news@example.com"}
	a := assistantwire.InboxMailAccount{ID: "acc", Email: "me@example.com", Messages: []assistantwire.InboxMailSummary{
		{Mailbox: "INBOX", UID: 1, Subject: "", From: boss, To: me, Date: d, Snippet: "明天的会改到 9 点"},
		{Mailbox: "INBOX", UID: 2, Subject: "", From: news, Date: d, Snippet: "本周精选"},
		{Mailbox: "INBOX", UID: 3, Subject: "Re:", From: boss, To: me, Date: d, Snippet: "地点不变"},
	}}
	items := b.applyMail(a, a.Messages, now)
	if len(items) != 2 {
		t.Fatalf("%d items, want the boss's and the newsletter's apart", len(items))
	}
	for _, it := range items {
		switch it.Sender {
		case "老板":
			if it.Count != 2 || !it.Mail.Direct || it.Preview != "地点不变" {
				t.Errorf("the boss's subject-less mail: %+v (count %d)", it.Mail, it.Count)
			}
		case "Newsletter":
			if it.Count != 1 || it.Mail.Direct {
				t.Errorf("the newsletter's: %+v (count %d)", it.Mail, it.Count)
			}
		default:
			t.Errorf("an item from %q", it.Sender)
		}
	}
	if mailThread("周报", "a@example.com") != mailThread("Re: 周报", "b@example.com") {
		t.Fatal("a subject no longer groups replies from different senders")
	}
}
