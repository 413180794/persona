package mail

import (
	"context"
	"errors"
	"fmt"
	"mime"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
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
			return loginRejected(err)
		}
		return nil
	default:
		return fmt.Errorf("mail: unsupported auth %q", a.Auth)
	}
}

// loginRejected is a failed IMAP LOGIN of a password account. A NO about the credentials (no code,
// AUTHENTICATIONFAILED, AUTHORIZATIONFAILED) says what to fix; another NO (blocked, contact the admin) quotes the
// server; UNAVAILABLE and anything that is not a NO stay server errors. The server's answer stays wrapped, so
// isConnError still sees a protocol reply and does not log in again.
func loginRejected(err error) error {
	var ie *imap.Error
	if errors.As(err, &ie) && ie.Type == imap.StatusResponseTypeNo {
		switch ie.Code {
		case "", imap.ResponseCodeAuthenticationFailed, imap.ResponseCodeAuthorizationFailed:
			return userError{msg: "邮箱服务器拒绝了登录：用户名或密码不对。QQ、163、126、飞书等邮箱要填网页版邮箱设置里生成的授权码，" +
				"不是登录密码（服务器原话：" + serverText(ie.Text) + "）", cause: err}
		case imap.ResponseCodeUnavailable: // the server is busy: not the user's to fix
		default:
			return userError{msg: "邮箱服务器拒绝了登录（服务器原话：" + serverText(ie.Text) + "）", cause: err}
		}
	}
	return fmt.Errorf("imap: login: %w", err)
}

// smtpLoginErr is the connection test's rejected SMTP login of a password account (535, or 534 "use an app
// password") told in words for people (IMAP has passed by then); any other error is wrapped as before.
func smtpLoginErr(a *MailAccount, err error) error {
	var se *smtp.SMTPError
	if (a.Auth == "" || a.Auth == AuthPassword) && errors.As(err, &se) && (se.Code == 535 || se.Code == 534) {
		return userError{msg: "收信登录通过了，发信（SMTP）登录被拒：多半是邮箱设置里没打开 SMTP 服务，或授权码不对" +
			"（服务器原话：" + serverText(se.Message) + "）", cause: err}
	}
	return fmt.Errorf("auth: %w", err)
}

// serverText is a server's reason as shown to the user, "" said as such.
func serverText(s string) string {
	if s == "" {
		return "没有说明"
	}
	return s
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
