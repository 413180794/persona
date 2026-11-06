package mail

import (
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/nwverse/persona/server/internal/i18n"
)

// preset describes the well-known servers of a mail provider.
type preset struct {
	Provider     string
	IMAPHost     string
	IMAPPort     int
	SMTPHost     string
	SMTPPort     int
	SMTPSecurity string
	// Guide is the steps to make the provider's authorization code, the password its mail clients sign in with
	// (PasswordGuide); guideNone when the account's own password works.
	Guide codeGuide
	// Site is the web mail whose settings make that code.
	Site string
}

// codeGuide is which provider's authorization code steps a preset has. The clients' add-mail forms show the same
// steps for the same domains (MailAccountForm on iOS / Android, mailCodeGuide on the web): change them together.
type codeGuide int

const (
	guideNone codeGuide = iota
	guideQQ
	guideNetEase
)

func qqPreset() *preset {
	return &preset{Provider: ProviderIMAP, IMAPHost: "imap.qq.com", IMAPPort: 993,
		SMTPHost: "smtp.qq.com", SMTPPort: 465, SMTPSecurity: "tls", Guide: guideQQ, Site: "mail.qq.com"}
}

// neteasePreset is a NetEase domain's servers, imap.<host> and smtp.<host> on SSL, whose code is made on the web
// mail at site.
func neteasePreset(host, site string) *preset {
	return &preset{Provider: ProviderIMAP, IMAPHost: "imap." + host, IMAPPort: 993,
		SMTPHost: "smtp." + host, SMTPPort: 465, SMTPSecurity: "tls", Guide: guideNetEase, Site: site}
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
	"qq.com":      qqPreset(),
	"foxmail.com": qqPreset(),
	"vip.qq.com":  qqPreset(),
	"163.com":     neteasePreset("163.com", "mail.163.com"),
	"126.com":     neteasePreset("126.com", "mail.126.com"),
	"yeah.net":    neteasePreset("yeah.net", "mail.yeah.net"),
	"vip.163.com": neteasePreset("vip.163.com", "vip.163.com"),
	"vip.126.com": neteasePreset("vip.126.com", "vip.126.com"),
	"188.com":     neteasePreset("188.com", "www.188.com"),
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

// feishuServers are Feishu / Lark mail's two server sets: larkoffice.com resolves
// overseas and is slow or times out from mainland China, feishu.cn is the reverse.
// Adding an account tries both and keeps the one that signs in first
// (pickFeishuServers); the first entry is the default for buildAccount.
var feishuServers = []*preset{
	{
		Provider: ProviderFeishu, IMAPHost: "imap.larkoffice.com", IMAPPort: 993,
		SMTPHost: "smtp.larkoffice.com", SMTPPort: 465, SMTPSecurity: "tls",
	},
	{
		Provider: ProviderFeishu, IMAPHost: "imap.feishu.cn", IMAPPort: 993,
		SMTPHost: "smtp.feishu.cn", SMTPPort: 465, SMTPSecurity: "tls",
	},
}

// providerPresets are chosen explicitly in the app because the address is on the company's own domain.
var providerPresets = map[string]*preset{
	// Feishu / Lark mail: SSL on both, account + password only.
	ProviderFeishu: feishuServers[0],
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

// PasswordGuide is, in l, where the provider of email makes the authorization code its mail clients sign in with;
// "" when the domain has no such steps.
func PasswordGuide(l i18n.Lang, email string) string {
	p := presetFor(email)
	if p == nil {
		return ""
	}
	switch p.Guide {
	case guideQQ:
		return i18n.T(l, "mail-preset.guide_qq", i18n.Args{"site": p.Site})
	case guideNetEase:
		return i18n.T(l, "mail-preset.guide_netease", i18n.Args{"site": p.Site})
	}
	return ""
}

// newAccountID returns a fresh account identifier, "acc_" + 12 hex chars.
func newAccountID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return "acc_" + hex.EncodeToString(b[:])
}
