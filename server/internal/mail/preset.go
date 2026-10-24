package mail

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// preset describes the well-known servers of a mail provider.
type preset struct {
	Provider     string
	IMAPHost     string
	IMAPPort     int
	SMTPHost     string
	SMTPPort     int
	SMTPSecurity string
}

var presets = map[string]*preset{
	"gmail.com": {
		Provider: ProviderGmail, IMAPHost: "imap.gmail.com", IMAPPort: 993,
		SMTPHost: "smtp.gmail.com", SMTPPort: 465, SMTPSecurity: "tls",
	},
	"googlemail.com": {
		Provider: ProviderGmail, IMAPHost: "imap.gmail.com", IMAPPort: 993,
		SMTPHost: "smtp.gmail.com", SMTPPort: 465, SMTPSecurity: "tls",
	},
	"qq.com": {
		Provider: ProviderIMAP, IMAPHost: "imap.qq.com", IMAPPort: 993,
		SMTPHost: "smtp.qq.com", SMTPPort: 465, SMTPSecurity: "tls",
	},
	"foxmail.com": {
		Provider: ProviderIMAP, IMAPHost: "imap.qq.com", IMAPPort: 993,
		SMTPHost: "smtp.qq.com", SMTPPort: 465, SMTPSecurity: "tls",
	},
	"163.com": {
		Provider: ProviderIMAP, IMAPHost: "imap.163.com", IMAPPort: 993,
		SMTPHost: "smtp.163.com", SMTPPort: 465, SMTPSecurity: "tls",
	},
	"126.com": {
		Provider: ProviderIMAP, IMAPHost: "imap.126.com", IMAPPort: 993,
		SMTPHost: "smtp.126.com", SMTPPort: 465, SMTPSecurity: "tls",
	},
	"outlook.com": {
		Provider: ProviderIMAP, IMAPHost: "outlook.office365.com", IMAPPort: 993,
		SMTPHost: "smtp-mail.outlook.com", SMTPPort: 587, SMTPSecurity: "starttls",
	},
	"hotmail.com": {
		Provider: ProviderIMAP, IMAPHost: "outlook.office365.com", IMAPPort: 993,
		SMTPHost: "smtp-mail.outlook.com", SMTPPort: 587, SMTPSecurity: "starttls",
	},
	"live.com": {
		Provider: ProviderIMAP, IMAPHost: "outlook.office365.com", IMAPPort: 993,
		SMTPHost: "smtp-mail.outlook.com", SMTPPort: 587, SMTPSecurity: "starttls",
	},
	"icloud.com": {
		Provider: ProviderIMAP, IMAPHost: "imap.mail.me.com", IMAPPort: 993,
		SMTPHost: "smtp.mail.me.com", SMTPPort: 587, SMTPSecurity: "starttls",
	},
	"me.com": {
		Provider: ProviderIMAP, IMAPHost: "imap.mail.me.com", IMAPPort: 993,
		SMTPHost: "smtp.mail.me.com", SMTPPort: 587, SMTPSecurity: "starttls",
	},
	"yahoo.com": {
		Provider: ProviderIMAP, IMAPHost: "imap.mail.yahoo.com", IMAPPort: 993,
		SMTPHost: "smtp.mail.yahoo.com", SMTPPort: 465, SMTPSecurity: "tls",
	},
	"yahoo.co.jp": {
		Provider: ProviderIMAP, IMAPHost: "imap.mail.yahoo.co.jp", IMAPPort: 993,
		SMTPHost: "smtp.mail.yahoo.co.jp", SMTPPort: 465, SMTPSecurity: "tls",
	},
}

// providerPresets are chosen explicitly in the app because the address is on the company's own domain.
var providerPresets = map[string]*preset{
	// Feishu / Lark mail: SSL on both, account + password only.
	ProviderFeishu: {
		Provider: ProviderFeishu, IMAPHost: "imap.larkoffice.com", IMAPPort: 993,
		SMTPHost: "smtp.larkoffice.com", SMTPPort: 465, SMTPSecurity: "tls",
	},
}

// presetFor returns the server preset for an email address, or nil when the
// domain is unknown.
func presetFor(email string) *preset {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return nil
	}
	return presets[strings.ToLower(email[at+1:])]
}

// newAccountID returns a fresh account identifier, "acc_" + 12 hex chars.
func newAccountID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return "acc_" + hex.EncodeToString(b[:])
}
