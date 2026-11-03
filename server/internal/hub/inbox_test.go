package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/lark"
	"github.com/nwverse/persona/server/internal/mail"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestInboxMailReplyTargetConflict(t *testing.T) {
	h := newTestHub(t)
	mux := http.NewServeMux()
	h.inboxRoutes(mux)
	item := &InboxItem{ID: "mail-item", Mail: &InboxMail{AccountID: "account", Mailbox: "INBOX", UIDs: []uint32{7}, MessageID: "original@example.com", From: mail.Address{Email: "sender@example.com"}}}
	h.inbox.items[item.ID] = item
	get := httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/v1/inbox/mail-item", nil))
	var detail struct {
		ReplyTarget inboxMailTarget `json:"reply_target"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &detail); err != nil || detail.ReplyTarget.UID != 7 || detail.ReplyTarget.Version == "" {
		t.Fatalf("target: %s %v", get.Body, err)
	}
	version := detail.ReplyTarget.Version
	item.Mail.UIDs = append(item.Mail.UIDs, 8)
	for _, body := range []string{
		`{"text":"reply","expected_uid":7}`,
		`{"text":"reply","expected_version":"` + version + `"}`,
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/inbox/mail-item/reply", strings.NewReader(body)))
		if response.Code != 409 || !strings.Contains(response.Body.String(), `"code":"mail_reply_target_changed"`) || !strings.Contains(response.Body.String(), `"delivered":"none"`) || item.DoneAt != nil {
			t.Fatalf("conflict: %d %s", response.Code, response.Body)
		}
	}
	// Changing the recipient without changing the UID must invalidate the version too.
	item.Mail.UIDs = []uint32{7}
	item.Mail.From.Email = "other@example.com"
	if mailTarget(*item).Version == version {
		t.Fatal("recipient change retained version")
	}
	upstream := &mailReplyTargetStub{}
	h.mail = upstream
	h.connectors = connector.NewRegistry(nil) // replies go to the connector the item belongs to (inbox_own.go)
	h.registerConnectors()
	current := mailTarget(*item)
	body, _ := json.Marshal(map[string]any{"text": "reply", "expected_uid": current.UID, "expected_version": current.Version})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/inbox/mail-item/reply", strings.NewReader(string(body))))
	if response.Code != http.StatusOK || upstream.sends != 1 || item.DoneBy != "replied" {
		t.Fatalf("confirmed reply: %d %s, sends=%d", response.Code, response.Body, upstream.sends)
	}
	// Legacy callers continue to send without target validation.
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/inbox/mail-item/reply", strings.NewReader(`{"text":"legacy"}`)))
	if response.Code != http.StatusOK || upstream.sends != 2 {
		t.Fatalf("legacy reply: %d %s", response.Code, response.Body)
	}
}

type mailReplyTargetStub struct {
	Mail
	sends int
}

func (m *mailReplyTargetStub) Tools() []proto.Tool { return nil } // registering the mail connector lists them

func (m *mailReplyTargetStub) GetMessage(_ context.Context, account string, uid uint32, mailbox string) (*mail.MailMessage, error) {
	return &mail.MailMessage{MailSummary: mail.MailSummary{AccountID: account, Mailbox: mailbox, UID: uid, From: mail.Address{Email: "other@example.com"}}}, nil
}
func (m *mailReplyTargetStub) Send(_ context.Context, _ mail.SendRequest) error {
	m.sends++
	return nil
}

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

func TestInboxLarkReadPosition(t *testing.T) {
	now := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	b := newInbox()
	chat := []lark.Message{larkMsg("20", "张三", false, now.Add(-3*time.Minute)), larkMsg("21", "张三", false, now.Add(-2*time.Minute)),
		larkMsg("22", "张三", false, now.Add(-time.Minute))}
	fetch := func(c lark.Conversation, n int) ([]lark.Message, error) { return chat[max(0, len(chat)-n):], nil }
	conv := lark.Conversation{ID: "f2", Name: "张三", Unread: 3, UpdatedAt: now.Add(-time.Minute), ReadPosition: "19"}

	ch := b.applyLark([]lark.Conversation{conv}, fetch, now, testUserZone)
	if len(ch) != 1 || ch[0].Lark.ReadPosition != "19" {
		t.Fatalf("collected: %+v", ch)
	}
	it := ch[0]
	raw, _ := json.Marshal(it.Lark)
	if !strings.Contains(string(raw), `"read_position":"19"`) {
		t.Fatalf("json = %s", raw)
	}
	// Read further elsewhere, nothing new: the read position follows without a fetch.
	conv.Unread, conv.ReadPosition = 1, "21"
	if ch := b.applyLark([]lark.Conversation{conv}, func(lark.Conversation, int) ([]lark.Message, error) {
		t.Fatal("fetched a chat with no new messages")
		return nil, nil
	}, now, testUserZone); len(ch) != 1 || ch[0] != it || it.Lark.ReadPosition != "21" || it.Last != "22" {
		t.Fatalf("read further: %+v", it.Lark)
	}
	// Same position again: no change.
	if ch := b.applyLark([]lark.Conversation{conv}, fetch, now, testUserZone); len(ch) != 0 {
		t.Fatalf("unchanged chat changed %d items", len(ch))
	}
	// A new message refreshes it along with everything else.
	chat = append(chat, larkMsg("23", "张三", false, now))
	conv.Unread, conv.UpdatedAt, conv.ReadPosition = 2, now, "22"
	if ch := b.applyLark([]lark.Conversation{conv}, fetch, now, testUserZone); len(ch) != 1 || it.Lark.ReadPosition != "22" || it.Last != "23" {
		t.Fatalf("follow-up: %+v", it.Lark)
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

// Items read before the settle pass cleared counts / before @所有人 left 需要你 are tidied on the next pass.
func TestInboxLarkSettleTidiesReadItems(t *testing.T) {
	now := time.Now()
	b := newInbox()
	mk := func(id string, it InboxItem) *InboxItem {
		it.ID, it.Kind, it.Read, it.Count, it.Last = id, KindMention, true, 2, "5"
		it.Lark = &InboxLark{FeedID: id, AtAll: it.Lark != nil && it.Lark.AtAll, AtMe: it.Lark != nil && it.Lark.AtMe}
		b.items[id] = &it
		return b.items[id]
	}
	ruleAll := mk("rule-all", InboxItem{Tier: TierAction, Reason: "按规则", Lark: &InboxLark{AtAll: true}})
	ruleMe := mk("rule-me", InboxItem{Tier: TierAction, Reason: "按规则", Lark: &InboxLark{AtAll: true, AtMe: true}})
	judged := mk("judged", InboxItem{Tier: TierAction, Reason: "问你要结论", Lark: &InboxLark{AtAll: true}})
	calls := 0
	latest := func(string, bool) (*lark.Message, error) {
		calls++
		m := larkMsg("5", "x", false, now)
		return &m, nil
	}
	if ch := b.settleLark(nil, latest, now); len(ch) != 3 {
		t.Fatalf("changed %d", len(ch))
	}
	if ruleAll.open() || ruleAll.Tier != TierFYI || ruleAll.DoneBy != "read" {
		t.Errorf("a read @所有人 in its rule tier should be done: %+v", ruleAll)
	}
	if !ruleMe.open() || ruleMe.Tier != TierAction || ruleMe.Count != 0 {
		t.Errorf("a read @me stays in 需要你 without its count: %+v", ruleMe)
	}
	if !judged.open() || judged.Tier != TierAction || judged.Count != 0 {
		t.Errorf("the model's tier is kept: %+v", judged)
	}
	if ch := b.settleLark(nil, latest, now); len(ch) != 0 {
		t.Errorf("tidied items come back: %d", len(ch))
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
		{InboxItem{Kind: KindMention, Lark: &InboxLark{AtAll: true}}, TierFYI},                // only @所有人: an announcement
		{InboxItem{Kind: KindMention, Lark: &InboxLark{AtAll: true, AtMe: true}}, TierAction}, // @me as well
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

func TestInboxLarkReplyTarget(t *testing.T) {
	now := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	b := newInbox()
	threadReply := larkMsg("31", "张三", false, now.Add(-2*time.Minute))
	threadReply.ThreadID, threadReply.ThreadKind = "m30", "reply"
	chat := []lark.Message{larkMsg("30", "李四", false, now.Add(-3*time.Minute)), threadReply,
		larkMsg("32", "我", true, now.Add(-time.Minute))}
	fetch := func(c lark.Conversation, n int) ([]lark.Message, error) { return chat[max(0, len(chat)-n):], nil }
	conv := lark.Conversation{ID: "f3", Name: "项目群", IsGroup: true, AtMe: true, Unread: 3, UpdatedAt: now.Add(-time.Minute)}

	// The newest message from someone else is a thread reply; my own after it is not what the reply answers.
	ch := b.applyLark([]lark.Conversation{conv}, fetch, now, testUserZone)
	if len(ch) != 1 || ch[0].Lark.ReplyTo != "m31" || ch[0].Lark.ThreadKind != LarkThreadThread {
		t.Fatalf("collected: %+v", ch[0].Lark)
	}
	it := ch[0]
	raw, _ := json.Marshal(it.Lark)
	if !strings.Contains(string(raw), `"reply_to":"m31"`) || !strings.Contains(string(raw), `"thread_kind":"thread"`) {
		t.Fatalf("json = %s", raw)
	}
	// A newer message in the chat itself moves both.
	chat = append(chat, larkMsg("33", "李四", false, now))
	conv.Unread, conv.UpdatedAt = 4, now
	if ch := b.applyLark([]lark.Conversation{conv}, fetch, now, testUserZone); len(ch) != 1 || it.Lark.ReplyTo != "m33" || it.Lark.ThreadKind != "" {
		t.Fatalf("follow-up: %+v", it.Lark)
	}
	raw, _ = json.Marshal(it.Lark)
	if strings.Contains(string(raw), "thread_kind") {
		t.Fatalf("json = %s, want thread_kind left out", raw)
	}

	// A topic group lists topics: whatever the newest is, a reply goes into it.
	topics := []lark.Message{larkMsg("40", "王五", false, now)}
	topicConv := lark.Conversation{ID: "f4", Name: "话题群", IsGroup: true, IsTopic: true, Unread: 1, UpdatedAt: now}
	ch = b.applyLark([]lark.Conversation{topicConv}, func(lark.Conversation, int) ([]lark.Message, error) { return topics, nil }, now, testUserZone)
	if len(ch) != 1 || ch[0].Lark.ReplyTo != "m40" || ch[0].Lark.ThreadKind != LarkThreadTopic {
		t.Fatalf("topic group: %+v", ch[0].Lark)
	}

	// A DM is never in a thread.
	dm := larkMsg("50", "赵六", false, now)
	dm.ThreadKind = "reply"
	ch = b.applyLark([]lark.Conversation{{ID: "f5", Name: "赵六", Unread: 1, UpdatedAt: now}},
		func(lark.Conversation, int) ([]lark.Message, error) { return []lark.Message{dm}, nil }, now, testUserZone)
	if len(ch) != 1 || ch[0].Lark.ReplyTo != "m50" || ch[0].Lark.ThreadKind != "" {
		t.Fatalf("dm: %+v", ch[0].Lark)
	}
}
