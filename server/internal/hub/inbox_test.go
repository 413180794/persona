package hub

import (
	"strings"
	"testing"
	"time"

	"github.com/chyroc/jarvis/server/internal/lark"
	"github.com/chyroc/jarvis/server/internal/mail"
)

func newInbox() *inbox {
	b := &inbox{}
	b.init()
	return b
}

func larkMsg(pos, sender string, mine bool, at time.Time) lark.Message {
	return lark.Message{ID: "m" + pos, Position: pos, SenderName: sender, IsMine: mine, Text: "hi " + pos, CreatedAt: at}
}

func TestInboxLarkKinds(t *testing.T) {
	cases := []struct {
		c    lark.Conversation
		want string
	}{
		{lark.Conversation{IsGroup: false}, KindDM},
		{lark.Conversation{IsGroup: false, Tags: []string{"机器人"}}, KindBot},
		{lark.Conversation{IsGroup: true, AtMe: true, Muted: true}, KindMention},
		{lark.Conversation{IsGroup: true, AtAll: true}, KindMention},
		{lark.Conversation{IsGroup: true, Muted: true}, ""},
		{lark.Conversation{IsGroup: true}, KindGroup},
	}
	for i, c := range cases {
		if got := larkKind(c.c); got != c.want {
			t.Errorf("%d: kind %q, want %q", i, got, c.want)
		}
	}
}

func TestInboxLarkOneItemPerChatUntilDone(t *testing.T) {
	now := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	b := newInbox()
	chat := []lark.Message{larkMsg("10", "我", true, now.Add(-5*time.Minute)), larkMsg("11", "张三", false, now.Add(-3*time.Minute)),
		larkMsg("12", "张三", false, now.Add(-time.Minute))}
	fetch := func(c lark.Conversation, n int) ([]lark.Message, error) { return chat[max(0, len(chat)-n):], nil }
	conv := lark.Conversation{ID: "f1", Name: "张三", Unread: 2, UpdatedAt: now.Add(-time.Minute)}

	changed := b.applyLark([]lark.Conversation{conv}, fetch, now, testUserZone)
	if len(changed) != 1 {
		t.Fatalf("changed = %d", len(changed))
	}
	it := changed[0]
	if it.ID != "lark:f1:11" || it.Kind != KindDM || it.Count != 2 || it.Last != "12" || it.Lark.StartPosition != "11" || it.Sender != "张三" {
		t.Fatalf("item = %+v", it)
	}
	// Nothing new: no fetch, no change.
	if ch := b.applyLark([]lark.Conversation{conv}, func(lark.Conversation, int) ([]lark.Message, error) {
		t.Fatal("fetched an unchanged chat")
		return nil, nil
	}, now, testUserZone); len(ch) != 0 {
		t.Fatalf("unchanged chat changed %d items", len(ch))
	}
	// A new message joins the open item.
	chat = append(chat, larkMsg("13", "张三", false, now))
	conv.Unread, conv.UpdatedAt = 3, now
	if ch := b.applyLark([]lark.Conversation{conv}, fetch, now, testUserZone); len(ch) != 1 || ch[0] != it || it.Count != 3 || it.Last != "13" {
		t.Fatalf("follow-up: %+v", it)
	}
	// Done, still unread in Feishu: the same messages don't come back...
	it.done(now, "user")
	conv.UpdatedAt = now.Add(time.Second) // a reaction or edit touches the chat
	if ch := b.applyLark([]lark.Conversation{conv}, fetch, now, testUserZone); len(ch) != 0 {
		t.Fatalf("done messages came back: %+v", ch[0])
	}
	// ...but a newer one starts a new item.
	chat = append(chat, larkMsg("14", "张三", false, now.Add(time.Minute)))
	conv.Unread, conv.UpdatedAt = 4, now.Add(time.Minute)
	ch := b.applyLark([]lark.Conversation{conv}, fetch, now, testUserZone)
	if len(ch) != 1 || ch[0].ID != "lark:f1:14" || ch[0].Count != 4 {
		t.Fatalf("new item: %+v", ch)
	}
}

func TestInboxLarkSkipsOldAndMuted(t *testing.T) {
	now := time.Now()
	b := newInbox()
	fetch := func(lark.Conversation, int) ([]lark.Message, error) {
		return []lark.Message{larkMsg("1", "x", false, now)}, nil
	}
	convs := []lark.Conversation{
		{ID: "old", Unread: 1, UpdatedAt: now.Add(-30 * 24 * time.Hour)},
		{ID: "muted", IsGroup: true, Muted: true, Unread: 5, UpdatedAt: now},
		{ID: "read", Unread: 0, UpdatedAt: now},
	}
	if ch := b.applyLark(convs, fetch, now, testUserZone); len(ch) != 0 {
		t.Fatalf("got %+v", ch[0])
	}
}

func TestInboxLarkSettle(t *testing.T) {
	now := time.Now()
	b := newInbox()
	mk := func(id, tier string) *InboxItem {
		it := &InboxItem{ID: id, Kind: KindMention, Tier: tier, Count: 4, Last: "5", Lark: &InboxLark{FeedID: id, AtAll: true}}
		b.items[id] = it
		return it
	}
	fyi, action, replied, still := mk("fyi", TierFYI), mk("action", TierAction), mk("replied", TierAction), mk("still", TierFYI)
	latest := func(feed string, _ bool) (*lark.Message, error) {
		m := larkMsg("5", "x", false, now)
		if feed == "replied" {
			m = larkMsg("6", "我", true, now)
		}
		return &m, nil
	}
	b.settleLark([]lark.Conversation{{ID: "still", Unread: 1}}, latest, now)
	if fyi.open() || !fyi.Read || fyi.DoneBy != "read" {
		t.Errorf("fyi read elsewhere should be done: %+v", fyi)
	}
	if !action.open() || !action.Read || action.Count != 0 {
		t.Errorf("action read elsewhere stays open without the unread count: %+v", action)
	}
	if replied.open() || replied.DoneBy != "replied" {
		t.Errorf("replied should be done: %+v", replied)
	}
	if !still.open() || still.Read || still.Count != 4 {
		t.Errorf("unread chat untouched: %+v", still)
	}
}

func TestInboxMailThreads(t *testing.T) {
	if mailThread("Re: 周报") != mailThread("周报") || mailThread("回复：[team] Fwd: 周报") != mailThread("周报") {
		t.Fatal("reply prefixes should join the thread")
	}
	if mailThread("周报") == mailThread("月报") {
		t.Fatal("different subjects")
	}
	now := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	date := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }
	a := mail.MailAccount{ID: "a1", Email: "me@example.com"}
	msgs := []mail.MailSummary{
		{UID: 3, Subject: "Re: 周报", From: mail.Address{Name: "李四", Email: "li@x"}, To: []mail.Address{{Email: "me@example.com"}}, Date: date(-time.Hour)},
		{UID: 2, Subject: "周报", From: mail.Address{Email: "wang@x"}, Date: date(-2 * time.Hour)},
		{UID: 1, Subject: "已读的", Seen: true, Date: date(-3 * time.Hour)},
	}
	b := newInbox()
	ch := b.applyMail(a, msgs, now)
	if len(ch) != 1 {
		t.Fatalf("items = %d", len(ch))
	}
	it := ch[0]
	if it.Count != 2 || it.Sender != "李四" || !it.Mail.Direct || it.Last != "3" || len(it.Mail.UIDs) != 2 || it.AccountName != "me@example.com" {
		t.Fatalf("item = %+v %+v", it, it.Mail)
	}
	if ch := b.applyMail(a, msgs, now); len(ch) != 0 {
		t.Fatal("unchanged mail changed")
	}
	// Read in another client: an FYI is done, something that needs me stays.
	it.Tier = TierFYI
	msgs[0].Seen, msgs[1].Seen = true, true
	if ch := b.applyMail(a, msgs, now); len(ch) != 1 || it.open() || it.DoneBy != "read" {
		t.Fatalf("read elsewhere: %+v", it)
	}
}

func TestInboxRuleTier(t *testing.T) {
	cases := []struct {
		it   InboxItem
		want string
	}{
		{InboxItem{Kind: KindDM}, TierAction},
		{InboxItem{Kind: KindMention}, TierAction},
		{InboxItem{Kind: KindGroup}, TierFYI},
		{InboxItem{Kind: KindBot}, TierNoise},
		{InboxItem{Kind: KindMail, Mail: &InboxMail{Direct: true}}, TierFYI},
		{InboxItem{Kind: KindMail, Mail: &InboxMail{}}, TierNoise},
	}
	for i, c := range cases {
		if got := ruleTier(&c.it); got != c.want {
			t.Errorf("%d: %q, want %q", i, got, c.want)
		}
	}
}

func TestInboxMailReply(t *testing.T) {
	orig := &mail.MailMessage{MailSummary: mail.MailSummary{Subject: "周报", MessageID: "<m2@x>", From: mail.Address{Name: "HR", Email: "hr@x"},
		Date: "2026-10-27T03:00:00Z"}, Text: "请确认\n谢谢", References: []string{"<m1@x>"}}
	r := mailReply("a1", orig, "收到")
	if r.Subject != "Re: 周报" || r.InReplyTo != "<m2@x>" || r.References != "<m1@x> <m2@x>" || len(r.To) != 1 || r.To[0].Email != "hr@x" {
		t.Fatalf("reply = %+v", r)
	}
	if !strings.HasPrefix(r.Text, "收到\n\n") || !strings.Contains(r.Text, "> 请确认\n> 谢谢") {
		t.Fatalf("body = %q", r.Text)
	}
	if mailReply("a1", &mail.MailMessage{MailSummary: mail.MailSummary{Subject: "RE: x"}}, "y").Subject != "RE: x" {
		t.Fatal("kept an existing Re:")
	}
}
