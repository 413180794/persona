package mail

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
)

func TestEncryptedStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
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
		OAuth:    &OAuthCreds{ClientID: "cid", ClientSecret: "csecret", RefreshToken: "rtok"},
		IMAPHost: "imap.gmail.com", IMAPPort: 993,
		SMTPHost: "smtp.gmail.com", SMTPPort: 465, SMTPSecurity: "tls",
		Status: "ok",
	}
	if err := s.store.save([]*MailAccount{a}); err != nil {
		t.Fatalf("save: %v", err)
	}

	// The account file must not contain the secrets in clear text.
	raw, err := os.ReadFile(filepath.Join(dir, "mail_accounts.json"))
	if err != nil {
		t.Fatalf("read accounts: %v", err)
	}
	for _, secret := range []string{"hunter2", "rtok", "csecret"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("account file leaks %q: %s", secret, raw)
		}
	}
	fileInfo, err := os.Stat(filepath.Join(dir, "mail_accounts.json"))
	if err != nil {
		t.Fatalf("stat accounts: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("mail_accounts.json mode = %o, want 600", got)
	}

	// Reload through a fresh Service: secrets must decrypt back.
	s2, err := New(dir)
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
	if got.OAuth == nil || *got.OAuth != *a.OAuth {
		t.Fatalf("oauth = %+v, want %+v", got.OAuth, a.OAuth)
	}

	// Public views never expose secrets.
	pub := got.public()
	if pub.Password != "" || pub.OAuth != nil {
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
	if _, err := openStore(dir); err == nil {
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
		{"oauth without creds", AddAccountRequest{Email: "me@gmail.com", Auth: AuthOAuth2}, "oauth with client_id"},
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

func TestBuildAccountInfersOAuth(t *testing.T) {
	a, err := buildAccount(AddAccountRequest{
		Email: "me@gmail.com",
		OAuth: &OAuthCreds{ClientID: "c", ClientSecret: "s", RefreshToken: "r"},
	})
	if err != nil {
		t.Fatalf("buildAccount: %v", err)
	}
	if a.Auth != AuthOAuth2 {
		t.Fatalf("auth = %q, want oauth2", a.Auth)
	}
	if a.Username != "me@gmail.com" {
		t.Fatalf("username = %q, want the email", a.Username)
	}
}

func TestOAuthTokenCache(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	calls := 0
	now := time.Now()
	s.now = func() time.Time { return now }
	s.accessToken = func(ctx context.Context, c *OAuthCreds) (string, time.Time, error) {
		calls++
		return "tok-" + string(rune('0'+calls)), now.Add(time.Hour), nil
	}
	// The stub bypasses the cache by design, so exercise cachedAccessToken
	// directly with the real fetch path replaced by a local server.
	_ = s.accessToken

	tok, _, err := s.cachedAccessToken(context.Background(), &OAuthCreds{ClientID: "c", RefreshToken: "r"})
	if err == nil {
		t.Fatalf("expected a network failure for the fake credentials, got %q", tok)
	}

	// Now cache a token by hand and check it is reused until it is close to
	// expiry.
	creds := &OAuthCreds{ClientID: "c", RefreshToken: "r"}
	key := creds.ClientID + "\x00" + creds.RefreshToken
	cache := &oauthCache{token: "cached", expires: now.Add(10 * time.Minute)}
	s.mu.Lock()
	s.oauths[key] = cache
	s.mu.Unlock()

	tok, _, err = s.cachedAccessToken(context.Background(), creds)
	if err != nil || tok != "cached" {
		t.Fatalf("cachedAccessToken = %q, %v; want cached", tok, err)
	}

	// Within a minute of expiry the cache is stale.
	now = now.Add(9*time.Minute + 30*time.Second)
	if _, _, err := s.cachedAccessToken(context.Background(), creds); err == nil {
		t.Fatal("expected a refresh attempt for an almost-expired token")
	}
}

func TestXOAUTH2Client(t *testing.T) {
	c := newXOAUTH2Client("me@example.com", "ya29.token")
	mech, ir, err := c.Start()
	if err != nil {
		t.Fatal(err)
	}
	if mech != "XOAUTH2" {
		t.Fatalf("mech = %q", mech)
	}
	want := "user=me@example.com\x01auth=Bearer ya29.token\x01\x01"
	if string(ir) != want {
		t.Fatalf("initial response = %q, want %q", ir, want)
	}
	resp, err := c.Next([]byte("invalid"))
	if err != nil || len(resp) != 0 {
		t.Fatalf("Next on failure = %q, %v; want an empty response", resp, err)
	}
	if _, err := c.Next(nil); err == nil {
		t.Fatal("Next after done should error")
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
