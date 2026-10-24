package mail

import (
	"context"
	"strings"
	"testing"
	"time"
)

// newServiceWithAccount builds a Service whose only account points at the test
// IMAP server.
func newServiceWithAccount(t *testing.T, srv *testIMAP) *Service {
	t.Helper()
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	a := srv.testAccount()
	s.mu.Lock()
	s.accts[a.ID] = a
	s.mu.Unlock()
	if err := s.store.save([]*MailAccount{a}); err != nil {
		t.Fatalf("save account: %v", err)
	}
	return s
}

func TestListMessagesAndPaging(t *testing.T) {
	srv := newTestIMAP(t)
	s := newServiceWithAccount(t, srv)
	ctx := context.Background()

	subjects := []string{"One", "Two", "Three", "Four", "Five"}
	for _, subj := range subjects {
		srv.appendMessage(t, "INBOX", rawMessage(subj, "张三 <zhang@example.com>", "Body of "+subj+"\r\nsecond line"))
	}

	res, err := s.ListMessages(ctx, ListOptions{AccountID: "acc_test", Limit: 3})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if got := len(res.Messages); got != 3 {
		t.Fatalf("len = %d, want 3", got)
	}
	if !res.HasMore {
		t.Fatal("has_more should be true when there are older messages")
	}
	// Newest first.
	if res.Messages[0].Subject != "Five" || res.Messages[2].Subject != "Three" {
		t.Fatalf("unexpected order: %v", subjectsOf(res.Messages))
	}
	first := res.Messages[0]
	if first.From.Name != "张三" || first.From.Email != "zhang@example.com" {
		t.Fatalf("from = %+v", first.From)
	}
	if first.AccountID != "acc_test" || first.Mailbox != "INBOX" || first.UID == 0 {
		t.Fatalf("summary header = %+v", first)
	}
	if first.Snippet != "Body of Five second line" {
		t.Fatalf("snippet = %q", first.Snippet)
	}
	if !strings.HasPrefix(first.Date, "2006-01-02T") && first.Date == "" {
		t.Fatalf("date = %q", first.Date)
	}

	// Page two uses before_uid of the oldest row on page one.
	oldest := res.Messages[len(res.Messages)-1].UID
	page2, err := s.ListMessages(ctx, ListOptions{AccountID: "acc_test", Limit: 3, BeforeUID: oldest})
	if err != nil {
		t.Fatalf("ListMessages page 2: %v", err)
	}
	if got := subjectsOf(page2.Messages); len(got) != 2 || got[0] != "Two" || got[1] != "One" {
		t.Fatalf("page 2 = %v, want [Two One]", got)
	}
	if page2.HasMore {
		t.Fatal("page 2 should not have more")
	}
}

func TestListMessagesSearch(t *testing.T) {
	srv := newTestIMAP(t)
	s := newServiceWithAccount(t, srv)
	ctx := context.Background()

	srv.appendMessage(t, "INBOX", rawMessage("Invoice", "billing@example.com", "Please pay the invoice"))
	srv.appendMessage(t, "INBOX", rawMessage("Lunch", "friend@example.com", "See you at noon"))
	srv.appendMessage(t, "INBOX", rawMessage("Newsletter", "news@example.com", "Weekly digest"))

	res, err := s.ListMessages(ctx, ListOptions{AccountID: "acc_test", Query: "invoice"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Messages) != 1 || res.Messages[0].Subject != "Invoice" {
		t.Fatalf("search results = %v", subjectsOf(res.Messages))
	}

	res, err = s.ListMessages(ctx, ListOptions{AccountID: "acc_test", Query: "Lunch"})
	if err != nil {
		t.Fatalf("search subject: %v", err)
	}
	if len(res.Messages) != 1 || res.Messages[0].Subject != "Lunch" {
		t.Fatalf("subject search results = %v", subjectsOf(res.Messages))
	}

	res, err = s.ListMessages(ctx, ListOptions{AccountID: "acc_test", Query: "nothing-matches-this"})
	if err != nil {
		t.Fatalf("empty search: %v", err)
	}
	if len(res.Messages) != 0 || res.HasMore {
		t.Fatalf("empty search = %+v", res)
	}
}

func TestMailboxes(t *testing.T) {
	srv := newTestIMAP(t)
	s := newServiceWithAccount(t, srv)
	ctx := context.Background()

	srv.appendMessage(t, "INBOX", rawMessage("Unread", "a@example.com", "hi"))
	srv.appendMessage(t, "INBOX", rawMessage("Read", "b@example.com", "hi"))

	boxes, err := s.Mailboxes(ctx, "acc_test")
	if err != nil {
		t.Fatalf("Mailboxes: %v", err)
	}
	byName := map[string]Mailbox{}
	for _, b := range boxes {
		byName[b.Name] = b
	}
	inbox, ok := byName["INBOX"]
	if !ok {
		t.Fatalf("INBOX missing from %v", boxes)
	}
	if inbox.Role != RoleInbox {
		t.Errorf("INBOX role = %q", inbox.Role)
	}
	if inbox.Total != 2 || inbox.Unseen != 2 {
		t.Errorf("INBOX counters = %d total / %d unseen, want 2/2", inbox.Total, inbox.Unseen)
	}
	// Special-use is not set by the in-memory server, so the name fallback
	// applies.
	if got := byName["Sent"].Role; got != RoleSent {
		t.Errorf("Sent role = %q", got)
	}
	if got := byName["Junk"].Role; got != RoleJunk {
		t.Errorf("Junk role = %q", got)
	}
}

func TestGetMessage(t *testing.T) {
	srv := newTestIMAP(t)
	s := newServiceWithAccount(t, srv)
	ctx := context.Background()

	raw := "From: =?utf-8?q?=E6=9D=8E=E5=9B=9B?= <li@example.com>\r\n" +
		"To: me@example.com, Second <two@example.com>\r\n" +
		"Cc: cc@example.com\r\n" +
		"Subject: =?utf-8?q?=E4=B8=AD=E6=96=87=E4=B8=BB=E9=A2=98?=\r\n" +
		"Date: Tue, 03 Jan 2006 10:00:00 +0800\r\n" +
		"Message-Id: <abc@example.com>\r\n" +
		"In-Reply-To: <parent@example.com>\r\n" +
		"References: <root@example.com> <parent@example.com>\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"BOUND\"\r\n" +
		"\r\n" +
		"--BOUND\r\n" +
		"Content-Type: multipart/alternative; boundary=\"ALT\"\r\n" +
		"\r\n" +
		"--ALT\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"plain body\r\n" +
		"--ALT\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		"<p>html <b>body</b></p>\r\n" +
		"--ALT--\r\n" +
		"--BOUND\r\n" +
		"Content-Type: application/pdf; name=\"report.pdf\"\r\n" +
		"Content-Disposition: attachment; filename=\"report.pdf\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" +
		"JVBERi0xLjQK\r\n" +
		"--BOUND--\r\n"

	uid := srv.appendMessage(t, "INBOX", raw)

	msg, err := s.GetMessage(ctx, "acc_test", uint32(uid), "INBOX")
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if msg.Subject != "中文主题" {
		t.Errorf("subject = %q", msg.Subject)
	}
	if msg.From.Name != "李四" || msg.From.Email != "li@example.com" {
		t.Errorf("from = %+v", msg.From)
	}
	if len(msg.To) != 2 || msg.To[1].Email != "two@example.com" || msg.To[1].Name != "Second" {
		t.Errorf("to = %+v", msg.To)
	}
	if len(msg.Cc) != 1 || msg.Cc[0].Email != "cc@example.com" {
		t.Errorf("cc = %+v", msg.Cc)
	}
	if msg.Text != "plain body" {
		t.Errorf("text = %q", msg.Text)
	}
	if !strings.Contains(msg.HTML, "<b>body</b>") {
		t.Errorf("html = %q", msg.HTML)
	}
	if len(msg.Attachments) != 1 || msg.Attachments[0].Filename != "report.pdf" {
		t.Fatalf("attachments = %+v", msg.Attachments)
	}
	if msg.Attachments[0].Mime != "application/pdf" {
		t.Errorf("attachment mime = %q", msg.Attachments[0].Mime)
	}
	if msg.Attachments[0].Size != 9 { // base64 "JVBERi0xLjQK" decodes to "%PDF-1.4\n"
		t.Errorf("attachment size = %d, want 9", msg.Attachments[0].Size)
	}
	if !msg.HasAttach {
		t.Error("has_attachments = false")
	}
	if msg.InReplyTo != "parent@example.com" {
		t.Errorf("in_reply_to = %q", msg.InReplyTo)
	}
	if len(msg.References) != 2 || msg.References[0] != "root@example.com" {
		t.Errorf("references = %v", msg.References)
	}
	if msg.Seen {
		t.Error("fetched message should not be marked seen")
	}

	// Reading must not set \Seen on the server (BODY.PEEK).
	res, err := s.ListMessages(ctx, ListOptions{AccountID: "acc_test"})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(res.Messages) != 1 || res.Messages[0].Seen {
		t.Fatalf("message was marked seen by GetMessage: %+v", res.Messages)
	}
}

func TestSetFlags(t *testing.T) {
	srv := newTestIMAP(t)
	s := newServiceWithAccount(t, srv)
	ctx := context.Background()

	uid := srv.appendMessage(t, "INBOX", rawMessage("Flags", "a@example.com", "hi"))

	yes, no := true, false
	if err := s.SetFlags(ctx, "acc_test", uint32(uid), SetFlagsRequest{Seen: &yes, Flagged: &yes}); err != nil {
		t.Fatalf("SetFlags seen+flagged: %v", err)
	}
	msg, err := s.GetMessage(ctx, "acc_test", uint32(uid), "INBOX")
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if !msg.Seen || !msg.Flagged {
		t.Fatalf("flags not set: seen=%v flagged=%v", msg.Seen, msg.Flagged)
	}

	if err := s.SetFlags(ctx, "acc_test", uint32(uid), SetFlagsRequest{Flagged: &no}); err != nil {
		t.Fatalf("SetFlags unflag: %v", err)
	}
	msg, err = s.GetMessage(ctx, "acc_test", uint32(uid), "INBOX")
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if msg.Flagged {
		t.Fatal("flag was not cleared")
	}
	if !msg.Seen {
		t.Fatal("seen flag was cleared by an unrelated update")
	}
}

func TestListMessagesAllAccounts(t *testing.T) {
	srvA := newTestIMAP(t)
	srvB := newTestIMAP(t)
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.store.save([]*MailAccount{srvA.testAccount()}); err != nil {
		t.Fatal(err)
	}
	accounts, _ := s.store.load()
	for _, a := range accounts {
		s.accts[a.ID] = a
	}
	b := srvB.testAccount()
	b.ID = "acc_b"
	b.Email = "second@example.com"
	b.Username = "me@example.com"
	s.accts[b.ID] = b

	srvA.appendMessage(t, "INBOX", rawMessage("From A", "a@example.com", "a"))
	srvB.appendMessage(t, "INBOX", rawMessage("From B", "b@example.com", "b"))

	res, err := s.ListMessages(context.Background(), ListOptions{})
	if err != nil {
		t.Fatalf("merged list: %v", err)
	}
	if len(res.Messages) != 2 {
		t.Fatalf("merged list = %v", subjectsOf(res.Messages))
	}
	ids := map[string]bool{}
	for _, m := range res.Messages {
		ids[m.AccountID] = true
	}
	if !ids["acc_test"] || !ids["acc_b"] {
		t.Fatalf("merged accounts = %v", ids)
	}
	// Newest first.
	for i := 1; i < len(res.Messages); i++ {
		if res.Messages[i-1].Date < res.Messages[i].Date {
			t.Fatalf("merged list is not sorted by date desc: %+v", res.Messages)
		}
	}
}

func TestConnReuseAndIdleExpiry(t *testing.T) {
	srv := newTestIMAP(t)
	s := newServiceWithAccount(t, srv)
	ctx := context.Background()

	if _, err := s.ListMessages(ctx, ListOptions{AccountID: "acc_test"}); err != nil {
		t.Fatalf("first list: %v", err)
	}
	s.mu.Lock()
	first := s.conns["acc_test"]
	s.mu.Unlock()
	if first == nil {
		t.Fatal("no cached connection after a list")
	}

	if _, err := s.ListMessages(ctx, ListOptions{AccountID: "acc_test"}); err != nil {
		t.Fatalf("second list: %v", err)
	}
	s.mu.Lock()
	second := s.conns["acc_test"]
	s.mu.Unlock()
	if second != first {
		t.Fatal("connection was not reused")
	}

	// Age the connection past the idle timeout and run one sweep: it must be
	// closed and removed.
	s.mu.Lock()
	first.lastUsed = time.Now().Add(-idleTimeout - time.Minute)
	s.mu.Unlock()
	s.sweepOnce()
	s.mu.Lock()
	_, still := s.conns["acc_test"]
	s.mu.Unlock()
	if still {
		t.Fatal("idle connection was not swept")
	}

	if _, err := s.ListMessages(ctx, ListOptions{AccountID: "acc_test"}); err != nil {
		t.Fatalf("list after sweep: %v", err)
	}
}

func TestReconnectAfterServerRestart(t *testing.T) {
	srv := newTestIMAP(t)
	s := newServiceWithAccount(t, srv)
	ctx := context.Background()

	if _, err := s.ListMessages(ctx, ListOptions{AccountID: "acc_test"}); err != nil {
		t.Fatalf("first list: %v", err)
	}
	// Kill the cached connection underneath the service, simulating a network
	// drop or a server-side timeout.
	s.mu.Lock()
	c := s.conns["acc_test"]
	s.mu.Unlock()
	if c != nil {
		c.c.Close()
	}
	time.Sleep(50 * time.Millisecond)

	if _, err := s.ListMessages(ctx, ListOptions{AccountID: "acc_test"}); err != nil {
		t.Fatalf("list after close: %v", err)
	}
}

func subjectsOf(msgs []MailSummary) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.Subject)
	}
	return out
}
