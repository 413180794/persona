package mail

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
)

func TestEncryptedStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	recs := newMemRecords()
	s, err := New(dir, recs)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	// The key file must be created with 0600.
	info, err := os.Stat(filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatalf("stat secret.key: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("secret.key mode = %o, want 600", got)
	}
	if info.Size() != 32 {
		t.Fatalf("secret.key size = %d, want 32", info.Size())
	}

	a := &MailAccount{
		ID:       "acc_1",
		Name:     "Gmail",
		Email:    "me@gmail.com",
		Provider: ProviderGmail,
		Auth:     AuthPassword,
		Username: "me@gmail.com",
		Password: "hunter2-中文",
		IMAPHost: "imap.gmail.com", IMAPPort: 993,
		SMTPHost: "smtp.gmail.com", SMTPPort: 465, SMTPSecurity: "tls",
		Status: "ok",
	}
	if err := s.store.save([]*MailAccount{a}); err != nil {
		t.Fatalf("save: %v", err)
	}

	// What reaches the database must not contain the secrets in clear text.
	raw := recs.dump()
	for _, secret := range []string{"hunter2"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("stored rows leak %q: %s", secret, raw)
		}
	}

	// Reload through a fresh Service: secrets must decrypt back.
	s2, err := New(dir, recs)
	if err != nil {
		t.Fatalf("New again: %v", err)
	}
	defer s2.Close()
	got, err := s2.account("acc_1")
	if err != nil {
		t.Fatalf("account: %v", err)
	}
	if got.Password != a.Password {
		t.Fatalf("password = %q, want %q", got.Password, a.Password)
	}

	// Public views never expose secrets.
	pub := got.public()
	if pub.Password != "" {
		t.Fatalf("public() leaked secrets: %+v", pub)
	}
	b, err := json.Marshal(pub)
	if err != nil {
		t.Fatalf("marshal public: %v", err)
	}
	if strings.Contains(string(b), `"password":`) || strings.Contains(string(b), `"oauth":`) {
		t.Fatalf("public JSON contains secret fields: %s", b)
	}
}

func TestStoreRejectsBadKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "secret.key"), []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openStore(dir, newMemRecords()); err == nil {
		t.Fatal("openStore accepted a 5-byte key")
	}
}

func TestPresets(t *testing.T) {
	cases := []struct {
		email    string
		provider string
		imapHost string
		imapPort int
		smtpHost string
		smtpPort int
		security string
	}{
		{"me@gmail.com", ProviderGmail, "imap.gmail.com", 993, "smtp.gmail.com", 465, "tls"},
		{"ME@GoogleMail.com", ProviderGmail, "imap.gmail.com", 993, "smtp.gmail.com", 465, "tls"},
		{"a@qq.com", ProviderIMAP, "imap.qq.com", 993, "smtp.qq.com", 465, "tls"},
		{"a@163.com", ProviderIMAP, "imap.163.com", 993, "smtp.163.com", 465, "tls"},
		{"a@126.com", ProviderIMAP, "imap.126.com", 993, "smtp.126.com", 465, "tls"},
		{"a@outlook.com", ProviderIMAP, "outlook.office365.com", 993, "smtp-mail.outlook.com", 587, "starttls"},
		{"a@hotmail.com", ProviderIMAP, "outlook.office365.com", 993, "smtp-mail.outlook.com", 587, "starttls"},
		{"a@icloud.com", ProviderIMAP, "imap.mail.me.com", 993, "smtp.mail.me.com", 587, "starttls"},
		{"a@yahoo.com", ProviderIMAP, "imap.mail.yahoo.com", 993, "smtp.mail.yahoo.com", 465, "tls"},
	}
	for _, tc := range cases {
		t.Run(tc.email, func(t *testing.T) {
			a, err := buildAccount(AddAccountRequest{Email: tc.email, Password: "p"})
			if err != nil {
				t.Fatalf("buildAccount: %v", err)
			}
			if a.Provider != tc.provider {
				t.Errorf("provider = %q, want %q", a.Provider, tc.provider)
			}
			if a.IMAPHost != tc.imapHost || a.IMAPPort != tc.imapPort {
				t.Errorf("imap = %s:%d, want %s:%d", a.IMAPHost, a.IMAPPort, tc.imapHost, tc.imapPort)
			}
			if a.SMTPHost != tc.smtpHost || a.SMTPPort != tc.smtpPort {
				t.Errorf("smtp = %s:%d, want %s:%d", a.SMTPHost, a.SMTPPort, tc.smtpHost, tc.smtpPort)
			}
			if a.SMTPSecurity != tc.security {
				t.Errorf("security = %q, want %q", a.SMTPSecurity, tc.security)
			}
		})
	}
}

func TestPresetExplicitFieldsWin(t *testing.T) {
	a, err := buildAccount(AddAccountRequest{
		Email:    "me@gmail.com",
		Password: "p",
		IMAPHost: "imap.internal.example",
		IMAPPort: 1993,
		SMTPHost: "smtp.internal.example",
		SMTPPort: 1587,
	})
	if err != nil {
		t.Fatalf("buildAccount: %v", err)
	}
	if a.IMAPHost != "imap.internal.example" || a.IMAPPort != 1993 {
		t.Errorf("explicit imap overridden: %s:%d", a.IMAPHost, a.IMAPPort)
	}
	if a.SMTPHost != "smtp.internal.example" || a.SMTPPort != 1587 {
		t.Errorf("explicit smtp overridden: %s:%d", a.SMTPHost, a.SMTPPort)
	}
}

func TestBuildAccountValidation(t *testing.T) {
	cases := []struct {
		name string
		req  AddAccountRequest
		want string
	}{
		{"missing email", AddAccountRequest{Password: "p"}, "email is required"},
		{"unknown domain", AddAccountRequest{Email: "me@nowhere.example", Password: "p"}, "unknown mail provider"},
		{"password auth without password", AddAccountRequest{Email: "me@gmail.com", Auth: AuthPassword}, "password is required"},
		{"oauth2 outside Gmail", AddAccountRequest{Email: "me@qq.com", Auth: AuthOAuth2}, "invalid auth"},
		{"bad auth", AddAccountRequest{Email: "me@gmail.com", Auth: "carrier-pigeon", Password: "p"}, "invalid auth"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildAccount(tc.req)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestRoleOf(t *testing.T) {
	cases := []struct {
		name  string
		attrs []string
		want  string
	}{
		{"INBOX", nil, RoleInbox},
		{"inbox", nil, RoleInbox},
		{"[Gmail]/Sent Mail", []string{`\Sent`}, RoleSent},
		{"Drafts", []string{`\Drafts`}, RoleDrafts},
		{"Trash", []string{`\Trash`}, RoleTrash},
		{"Archive", []string{`\Archive`}, RoleArchive},
		{"Junk", []string{`\Junk`}, RoleJunk},
		{"[Gmail]/All Mail", []string{`\All`}, ""},
		{"Sent", nil, RoleSent},
		{"Deleted Items", nil, RoleTrash},
		{"Custom", nil, ""},
	}
	for _, tc := range cases {
		var list []imap.MailboxAttr
		for _, a := range tc.attrs {
			list = append(list, imap.MailboxAttr(a))
		}
		if got := roleOf(tc.name, list); got != tc.want {
			t.Errorf("roleOf(%q, %v) = %q, want %q", tc.name, tc.attrs, got, tc.want)
		}
	}
}

func TestIsConnError(t *testing.T) {
	if isConnError(nil) {
		t.Fatal("nil is not a connection error")
	}
	if !isConnError(errors.New("broken pipe")) {
		t.Fatal("a bare error should trigger a reconnect")
	}
	if isConnError(context.DeadlineExceeded) {
		t.Fatal("a deadline is not a broken connection")
	}
}

func TestFeishuProviderUsesFixedServers(t *testing.T) {
	a, err := buildAccount(AddAccountRequest{Email: "me@example.com", Provider: ProviderFeishu, Password: "pw",
		IMAPHost: "ignored.example.com", IMAPPort: 143})
	if err != nil {
		t.Fatal(err)
	}
	if a.IMAPHost != "imap.larkoffice.com" || a.IMAPPort != 993 || a.SMTPHost != "smtp.larkoffice.com" ||
		a.SMTPPort != 465 || a.SMTPSecurity != "tls" || a.Provider != ProviderFeishu || a.Username != "me@example.com" {
		t.Fatalf("feishu account = %+v", a)
	}
}
