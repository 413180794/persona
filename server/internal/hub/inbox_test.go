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
