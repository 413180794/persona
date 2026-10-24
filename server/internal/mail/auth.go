package mail

import (
	"fmt"
	"mime"

	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"
	"github.com/emersion/go-sasl"
)

// mailWordDecoder decodes RFC 2047 encoded words in IMAP envelope fields with
// the full charset set registered by go-message/charset.
func mailWordDecoder() *mime.WordDecoder {
	return &mime.WordDecoder{CharsetReader: charset.Reader}
}

// loginIMAP authenticates an IMAP client with the account's credentials.
func loginIMAP(c *imapclient.Client, a *MailAccount) error {
	switch a.Auth {
	case "", AuthPassword:
		if err := c.Login(a.Username, a.Password).Wait(); err != nil {
			return fmt.Errorf("imap: login: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("mail: unsupported auth %q", a.Auth)
	}
}

// smtpAuth returns the SASL client used to authenticate to SMTP.
func (s *Service) smtpAuth(a *MailAccount) (sasl.Client, error) {
	switch a.Auth {
	case "", AuthPassword:
		return sasl.NewPlainClient("", a.Username, a.Password), nil
	default:
		return nil, fmt.Errorf("mail: unsupported auth %q", a.Auth)
	}
}
