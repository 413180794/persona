// Package mail implements the Jarvis mail module: encrypted account storage,
// IMAP reading, SMTP sending and the /api/v1/mail HTTP API.
//
// The JSON shapes mirror docs/protocol.md ("邮件 API"); keep them in sync.
package mail

// Address is a mail address with an optional display name.
type Address struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Authentication methods.
const (
	AuthPassword = "password"
)

// Providers.
const (
	ProviderGmail  = "gmail"
	ProviderIMAP   = "imap"
	ProviderFeishu = "feishu" // Feishu / Lark mail on a company domain: fixed larkoffice.com servers
)

// Mailbox roles. The values match the role field in docs/protocol.md.
const (
	RoleInbox   = "inbox"
	RoleSent    = "sent"
	RoleDrafts  = "drafts"
	RoleTrash   = "trash"
	RoleArchive = "archive"
	RoleJunk    = "junk"
)

// MailAccount is the public view of an account (no secrets).
type MailAccount struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Provider string `json:"provider"` // gmail | imap
	Auth     string `json:"auth"`     // password
	Username string `json:"username"`

	IMAPHost string `json:"imap_host"`
	IMAPPort int    `json:"imap_port"`
	// IMAPSecurity is an optional override: tls | starttls | none.
	// When empty it is derived from the port (993 = tls, 143 = starttls,
	// anything else = none, which is only meant for local test servers).
	IMAPSecurity string `json:"imap_security,omitempty"`

	SMTPHost     string `json:"smtp_host"`
	SMTPPort     int    `json:"smtp_port"`
	SMTPSecurity string `json:"smtp_security"` // tls | starttls (| none for tests)

	Status string `json:"status"` // ok | error
	Error  string `json:"error"`

	// Secrets. Never marshalled into API responses.
	Password string `json:"-"`
}

// AddAccountRequest is the POST /api/v1/mail/accounts body. Fields that the
// request leaves empty are filled from the preset for the email domain.
type AddAccountRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Provider string `json:"provider,omitempty"`
	Auth     string `json:"auth,omitempty"`
	Username string `json:"username,omitempty"`

	Password string `json:"password,omitempty"`

	IMAPHost     string `json:"imap_host,omitempty"`
	IMAPPort     int    `json:"imap_port,omitempty"`
	IMAPSecurity string `json:"imap_security,omitempty"`
	SMTPHost     string `json:"smtp_host,omitempty"`
	SMTPPort     int    `json:"smtp_port,omitempty"`
	SMTPSecurity string `json:"smtp_security,omitempty"`
}

// Mailbox is a mailbox with its role and counters.
type Mailbox struct {
	Name   string `json:"name"`
	Role   string `json:"role"`
	Unseen int    `json:"unseen"`
	Total  int    `json:"total"`
}

// MailSummary is one row of a message list.
type MailSummary struct {
	AccountID string    `json:"account_id"`
	Mailbox   string    `json:"mailbox"`
	UID       uint32    `json:"uid"`
	MessageID string    `json:"message_id"`
	Subject   string    `json:"subject"`
	From      Address   `json:"from"`
	To        []Address `json:"to"`
	Date      string    `json:"date"` // RFC3339, UTC
	Snippet   string    `json:"snippet"`
	Seen      bool      `json:"seen"`
	Flagged   bool      `json:"flagged"`
	HasAttach bool      `json:"has_attachments"`
}

// Attachment is attachment metadata; the body is not downloaded.
type Attachment struct {
	Filename string `json:"filename"`
	Mime     string `json:"mime"`
	Size     int64  `json:"size"`
}

// MailMessage is a full message.
type MailMessage struct {
	MailSummary
	Cc          []Address    `json:"cc"`
	Text        string       `json:"text"`
	HTML        string       `json:"html"`
	Attachments []Attachment `json:"attachments"`
	InReplyTo   string       `json:"in_reply_to"`
	References  []string     `json:"references"`
}

// ListOptions selects messages. AccountID == "" merges every account's inbox.
type ListOptions struct {
	AccountID string
	Mailbox   string
	Limit     int
	BeforeUID uint32
	Query     string
}

// ListResult is the message list response.
type ListResult struct {
	Messages []MailSummary `json:"messages"`
	HasMore  bool          `json:"has_more"`
}

// SetFlagsRequest updates the seen/flagged flags of one message.
type SetFlagsRequest struct {
	Mailbox string `json:"mailbox"`
	Seen    *bool  `json:"seen,omitempty"`
	Flagged *bool  `json:"flagged,omitempty"`
}

// SendRequest is a message to send.
type SendRequest struct {
	AccountID  string    `json:"account_id"`
	To         []Address `json:"to"`
	Cc         []Address `json:"cc,omitempty"`
	Bcc        []Address `json:"bcc,omitempty"`
	Subject    string    `json:"subject"`
	Text       string    `json:"text"`
	HTML       string    `json:"html,omitempty"`
	InReplyTo  string    `json:"in_reply_to,omitempty"`
	References string    `json:"references,omitempty"`
}
