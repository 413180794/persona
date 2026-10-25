package mail

import (
	"context"
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

// oauthToken returns the access token of an OAuth account, "" for password accounts.
func (s *Service) oauthToken(ctx context.Context, a *MailAccount) (string, error) {
	if a.Auth != AuthOAuth2 {
		return "", nil
	}
	return s.accessToken(ctx, a.OAuth)
}

// loginIMAP authenticates an IMAP client with the account's credentials (token for OAuth accounts).
func loginIMAP(c *imapclient.Client, a *MailAccount, token string) error {
	switch a.Auth {
	case AuthOAuth2:
		if err := c.Authenticate(newXOAUTH2Client(a.Username, token)); err != nil {
			return fmt.Errorf("imap: XOAUTH2: %w", err)
		}
		return nil
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
func (s *Service) smtpAuth(ctx context.Context, a *MailAccount) (sasl.Client, error) {
	switch a.Auth {
	case AuthOAuth2:
		token, err := s.accessToken(ctx, a.OAuth)
		if err != nil {
			return nil, err
		}
		return newXOAUTH2Client(a.Username, token), nil
	case "", AuthPassword:
		return sasl.NewPlainClient("", a.Username, a.Password), nil
	default:
		return nil, fmt.Errorf("mail: unsupported auth %q", a.Auth)
	}
}
