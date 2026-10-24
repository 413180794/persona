package mail

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// doJSON issues a request against the handler and decodes the JSON reply.
func doJSON(t *testing.T, s *Service, method, path, body string) (int, map[string]json.RawMessage) {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)

	var out map[string]json.RawMessage
	if w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s %s: response is not a JSON object: %v\n%s", method, path, err, w.Body)
		}
	}
	return w.Code, out
}

func TestHTTPAccountsEndpoints(t *testing.T) {
	srv := newTestIMAP(t)
	s := newServiceWithAccount(t, srv)
	_ = s

	code, body := doJSON(t, s, http.MethodGet, "/api/v1/mail/accounts", "")
	if code != http.StatusOK {
		t.Fatalf("GET accounts status = %d, body %v", code, body)
	}
	var accounts []map[string]any
	if err := json.Unmarshal(body["accounts"], &accounts); err != nil {
		t.Fatalf("accounts is not an array: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("accounts = %v", accounts)
	}
	for _, key := range []string{"id", "name", "email", "provider", "auth", "username", "imap_host", "imap_port", "smtp_host", "smtp_port", "smtp_security", "status", "error"} {
		if _, ok := accounts[0][key]; !ok {
			t.Errorf("account is missing %q: %v", key, accounts[0])
		}
	}
	for _, secret := range []string{"password", "oauth"} {
		if _, ok := accounts[0][secret]; ok {
			t.Errorf("account response leaks %q", secret)
		}
	}
}

func TestHTTPMailboxes(t *testing.T) {
	srv := newTestIMAP(t)
	s := newServiceWithAccount(t, srv)
	srv.appendMessage(t, "INBOX", rawMessage("hi", "a@example.com", "body"))

	code, body := doJSON(t, s, http.MethodGet, "/api/v1/mail/accounts/acc_test/mailboxes", "")
	if code != http.StatusOK {
		t.Fatalf("status = %d, body %v", code, body)
	}
	var boxes []map[string]any
	if err := json.Unmarshal(body["mailboxes"], &boxes); err != nil {
		t.Fatalf("mailboxes is not an array: %v", err)
	}
	if len(boxes) == 0 {
		t.Fatal("no mailboxes")
	}
	for _, key := range []string{"name", "role", "unseen", "total"} {
		if _, ok := boxes[0][key]; !ok {
			t.Errorf("mailbox is missing %q: %v", key, boxes[0])
		}
	}
	// Counters must be numbers, not strings.
	if _, ok := boxes[0]["unseen"].(float64); !ok {
		t.Errorf("unseen is not a number: %v (%T)", boxes[0]["unseen"], boxes[0]["unseen"])
	}

	code, body = doJSON(t, s, http.MethodGet, "/api/v1/mail/accounts/acc_nope/mailboxes", "")
	if code != http.StatusNotFound {
		t.Errorf("unknown account status = %d, want 404", code)
	}
	if _, ok := body["error"]; !ok {
		t.Errorf("error response = %v", body)
	}
}

func TestHTTPMessagesShape(t *testing.T) {
	srv := newTestIMAP(t)
	s := newServiceWithAccount(t, srv)
	uid := srv.appendMessage(t, "INBOX", rawMessage("Hello", "张三 <z@example.com>", "the body text"))

	code, body := doJSON(t, s, http.MethodGet, "/api/v1/mail/messages?account_id=acc_test&mailbox=INBOX&limit=10", "")
	if code != http.StatusOK {
		t.Fatalf("status = %d, body %v", code, body)
	}
	if _, ok := body["has_more"]; !ok {
		t.Errorf("response is missing has_more: %v", body)
	}
	var msgs []map[string]any
	if err := json.Unmarshal(body["messages"], &msgs); err != nil {
		t.Fatalf("messages is not an array: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("messages = %v", msgs)
	}
	for _, key := range []string{"account_id", "mailbox", "uid", "message_id", "subject", "from", "to", "date", "snippet", "seen", "flagged", "has_attachments"} {
		if _, ok := msgs[0][key]; !ok {
			t.Errorf("summary is missing %q: %v", key, msgs[0])
		}
	}
	from, ok := msgs[0]["from"].(map[string]any)
	if !ok {
		t.Fatalf("from is not an object: %v", msgs[0]["from"])
	}
	if from["email"] != "z@example.com" || from["name"] != "张三" {
		t.Errorf("from = %v", from)
	}
	if _, ok := msgs[0]["to"].([]any); !ok {
		t.Errorf("to is not an array: %v", msgs[0]["to"])
	}
	if msgs[0]["snippet"] != "the body text" {
		t.Errorf("snippet = %v", msgs[0]["snippet"])
	}

	// Detail endpoint.
	path := "/api/v1/mail/messages/acc_test/" + itoa(uint32(uid)) + "?mailbox=INBOX"
	code, body = doJSON(t, s, http.MethodGet, path, "")
	if code != http.StatusOK {
		t.Fatalf("detail status = %d, body %v", code, body)
	}
	var msg map[string]any
	if err := json.Unmarshal(body["message"], &msg); err != nil {
		t.Fatalf("message is not an object: %v", err)
	}
	for _, key := range []string{"account_id", "uid", "subject", "from", "to", "cc", "text", "html", "attachments", "in_reply_to", "references", "date", "snippet", "seen", "flagged", "has_attachments"} {
		if _, ok := msg[key]; !ok {
			t.Errorf("message is missing %q: %v", key, msg)
		}
	}
	if _, ok := msg["attachments"].([]any); !ok {
		t.Errorf("attachments is not an array: %v", msg["attachments"])
	}
	if _, ok := msg["references"].([]any); !ok {
		t.Errorf("references is not an array: %v", msg["references"])
	}
	if msg["text"] != "the body text" {
		t.Errorf("text = %v", msg["text"])
	}
}

func TestHTTPFlagsAndErrors(t *testing.T) {
	srv := newTestIMAP(t)
	s := newServiceWithAccount(t, srv)
	uid := srv.appendMessage(t, "INBOX", rawMessage("Flags", "a@example.com", "x"))

	path := "/api/v1/mail/messages/acc_test/" + itoa(uint32(uid)) + "/flags"
	code, body := doJSON(t, s, http.MethodPost, path, `{"mailbox":"INBOX","seen":true,"flagged":true}`)
	if code != http.StatusOK {
		t.Fatalf("flags status = %d, body %v", code, body)
	}
	msg, err := s.GetMessage(context.Background(), "acc_test", uint32(uid), "INBOX")
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if !msg.Seen || !msg.Flagged {
		t.Fatalf("flags not applied: %+v", msg.MailSummary)
	}

	// Bad JSON is a 400 with an error field.
	code, body = doJSON(t, s, http.MethodPost, path, `{"seen":`)
	if code != http.StatusBadRequest {
		t.Errorf("bad JSON status = %d, want 400", code)
	}
	if _, ok := body["error"]; !ok {
		t.Errorf("bad JSON body = %v", body)
	}

	// A bad uid is a 400.
	code, _ = doJSON(t, s, http.MethodGet, "/api/v1/mail/messages/acc_test/notanumber", "")
	if code != http.StatusBadRequest {
		t.Errorf("bad uid status = %d, want 400", code)
	}

	// Sending without recipients is a 400.
	code, body = doJSON(t, s, http.MethodPost, "/api/v1/mail/send", `{"account_id":"acc_test","subject":"x","text":"y"}`)
	if code != http.StatusBadRequest {
		t.Errorf("send without recipients status = %d, body %v", code, body)
	}
}

func TestHTTPMethodsAreRestricted(t *testing.T) {
	s := newServiceWithAccount(t, newTestIMAP(t))
	// PUT is not registered on the accounts collection.
	code, _ := doJSON(t, s, http.MethodPut, "/api/v1/mail/accounts", "")
	if code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT accounts status = %d, want 405", code)
	}
}

// itoa is a tiny strconv.Itoa to keep imports focused.
func itoa(n uint32) string {
	if n == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
