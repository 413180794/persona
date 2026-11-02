package mail

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
)

func TestAttachmentDownloadRangeAndIdentity(t *testing.T) {
	srv := newTestIMAPCaps(t, imap.CapSet{imap.CapIMAP4rev1: {}})
	srv.ignoreListReturn(t)
	service := newServiceWithAccount(t, srv)
	raw := "From: sender@example.com\r\nTo: me@example.com\r\nSubject: file\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=parts\r\n\r\n" +
		"--parts\r\nContent-Type: text/plain\r\n\r\nbody\r\n" +
		"--parts\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=test.bin\r\nContent-Transfer-Encoding: base64\r\n\r\nMDEyMzQ1Njc4OQ==\r\n--parts--\r\n"
	uid := uint32(srv.appendMessage(t, "INBOX", raw))
	message, err := service.GetMessage(context.Background(), "acc_test", uid, "INBOX")
	if err != nil {
		t.Fatal(err)
	}
	if len(message.Attachments) != 1 || message.Attachments[0].ID == "" || message.Attachments[0].Size != 10 {
		t.Fatalf("attachments: %+v", message.Attachments)
	}
	path := "/api/v1/mail/messages/acc_test/" + itoa(uid) + "/attachments/" + message.Attachments[0].ID + "?mailbox=INBOX"
	for _, test := range []struct {
		rangeHeader        string
		status             int
		body, contentRange string
	}{
		{"", 200, "0123456789", ""},
		{"bytes=2-5", 206, "2345", "bytes 2-5/10"},
		{"bytes=6-", 206, "6789", "bytes 6-9/10"},
		{"bytes=40-", 416, "", "bytes */10"},
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if test.rangeHeader != "" {
			req.Header.Set("Range", test.rangeHeader)
		}
		response := httptest.NewRecorder()
		service.Handler().ServeHTTP(response, req)
		if response.Code != test.status || test.body != "" && response.Body.String() != test.body || response.Header().Get("Content-Range") != test.contentRange {
			t.Fatalf("range %q: %d %s %s", test.rangeHeader, response.Code, response.Header().Get("Content-Range"), response.Body)
		}
	}
	for _, invalidPath := range []string{
		strings.Replace(path, "acc_test", "missing", 1),
		strings.Replace(path, message.Attachments[0].ID, "1-"+strings.Repeat("0", 64), 1),
	} {
		response := httptest.NewRecorder()
		service.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, invalidPath, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("invalid identity: %d %s", response.Code, response.Body)
		}
	}
	again, err := service.GetMessage(context.Background(), "acc_test", uid, "INBOX")
	if err != nil || again.Seen || again.Attachments[0].ID != message.Attachments[0].ID {
		t.Fatalf("download changed flags or identity: %+v %v", again, err)
	}
}
