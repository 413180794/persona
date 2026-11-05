package wechat

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/wechat/ilink"
)

// Binding is the connector's sign-in: a QR code to scan with WeChat (connector.PromptQRCode, asked again every
// interval), WeChat's confirmation on the phone, sometimes a number WeChat shows to type in here.

// pendingQR is the authorization under way; only in memory (a restart starts over with a new code).
type pendingQR struct {
	qr      ilink.QR
	base    string // where to poll: iLink may redirect a scanned code to another origin
	started time.Time
	renewed int
	scanned bool
}

const (
	qrInterval = 2
	// qrWait is how long one ask waits on iLink's long poll: short, the app asks again every qrInterval seconds.
	qrWait   = 8 * time.Second
	qrMaxAge = 5 * time.Minute
	stepQR   = "qr"
	stepCode = "code"
)

var verifyCode = regexp.MustCompile(`^\d{1,12}$`)

// authClient is an iLink client for authorizing (no token).
func (b *Bot) authClient() (*ilink.Client, error) { return b.opt.NewClient(ilink.AuthOrigin, "") }

func (b *Bot) LoginStep(ctx context.Context, step string, values map[string]string) (connector.Prompt, error) {
	if err := b.load(ctx); err != nil {
		return connector.Prompt{}, err
	}
	b.mu.Lock()
	p := b.qr
	b.mu.Unlock()
	if step == "" || p == nil || b.opt.Now().Sub(p.started) > qrMaxAge {
		return b.beginQR(ctx)
	}
	code := ""
	if step == stepCode {
		code = strings.TrimSpace(values["code"])
		if !verifyCode.MatchString(code) {
			return codePrompt(b.lang(), i18n.T(b.lang(), "wechat-login.enter_number")), nil
		}
	}
	c, err := b.authClient()
	if err != nil {
		return connector.Prompt{}, err
	}
	st, err := c.PollQR(ctx, p.qr.Code, code, p.base, qrWait)
	if err != nil {
		if ilink.Retryable(err) {
			return p.prompt(b.lang()), nil // asked again in a moment
		}
		return connector.Prompt{}, err
	}
	switch st.State {
	case ilink.QRWaiting:
		return p.prompt(b.lang()), nil
	case ilink.QRScanned:
		b.mu.Lock()
		p.scanned = true
		b.mu.Unlock()
		return p.prompt(b.lang()), nil
	case ilink.QRRedirect:
		b.mu.Lock()
		p.base = st.RedirectBase
		b.mu.Unlock()
		return p.prompt(b.lang()), nil
	case ilink.QRNeedCode:
		if step == stepCode {
			return codePrompt(b.lang(), i18n.T(b.lang(), "wechat-login.wrong_number")), nil
		}
		return codePrompt(b.lang(), ""), nil
	case ilink.QRExpired:
		if p.renewed >= 3 {
			b.forgetQR(p)
			return connector.Prompt{}, errors.New(i18n.T(b.lang(), "wechat-login.qr_expired_too_often"))
		}
		return b.renewQR(ctx, p)
	case ilink.QRCodeBlocked:
		b.forgetQR(p)
		return connector.Prompt{}, errors.New(i18n.T(b.lang(), "wechat-login.code_blocked"))
	case ilink.QRBoundAlready:
		b.forgetQR(p)
		return connector.Prompt{}, errors.New(i18n.T(b.lang(), "wechat-login.bound_already"))
	case ilink.QRConfirmed:
		b.forgetQR(p)
		return b.bound(ctx, st.Credentials), nil
	}
	return p.prompt(b.lang()), nil
}

func (p *pendingQR) prompt(l i18n.Lang) connector.Prompt {
	hint := i18n.T(l, "wechat-login.scan_hint")
	if p.scanned {
		hint = i18n.T(l, "wechat-login.scanned_hint")
	}
	return connector.Prompt{Step: stepQR, Kind: connector.PromptQRCode, URL: p.qr.Content, Interval: qrInterval, Hint: hint}
}

func codePrompt(l i18n.Lang, errText string) connector.Prompt {
	return connector.Prompt{Step: stepCode, Fields: []connector.Field{{Name: "code", Label: i18n.T(l, "wechat-login.code_label"), Placeholder: i18n.T(l, "wechat-login.code_placeholder")}},
		Hint: i18n.T(l, "wechat-login.code_hint"), Error: errText}
}

func (b *Bot) beginQR(ctx context.Context) (connector.Prompt, error) {
	c, err := b.authClient()
	if err != nil {
		return connector.Prompt{}, err
	}
	b.mu.Lock()
	tokens := append([]string(nil), b.st.Old...)
	if b.st.Creds != nil {
		tokens = rememberToken(tokens, b.st.Creds.Token)
	}
	b.mu.Unlock()
	q, err := c.BeginQR(ctx, tokens)
	if err != nil {
		return connector.Prompt{}, err
	}
	p := &pendingQR{qr: q, started: b.opt.Now()}
	b.mu.Lock()
	b.qr = p
	b.mu.Unlock()
	return p.prompt(b.lang()), nil
}

func (b *Bot) renewQR(ctx context.Context, old *pendingQR) (connector.Prompt, error) {
	pr, err := b.beginQR(ctx)
	if err != nil {
		return pr, err
	}
	b.mu.Lock()
	if b.qr != nil {
		b.qr.renewed = old.renewed + 1
		b.qr.started = old.started
	}
	b.mu.Unlock()
	return pr, nil
}

func (b *Bot) forgetQR(p *pendingQR) {
	b.mu.Lock()
	if b.qr == p {
		b.qr = nil
	}
	b.mu.Unlock()
}

// bound keeps the new credentials and starts serving them. The same bot (bound again) keeps its cursor; another
// starts fresh.
func (b *Bot) bound(ctx context.Context, creds ilink.Credentials) connector.Prompt {
	b.mu.Lock()
	if old := b.st.Creds; old != nil {
		b.st.Old = rememberToken(b.st.Old, old.Token)
		if old.BotID != creds.BotID || old.UserID != creds.UserID {
			b.st.Cursor, b.st.Context = "", ""
		}
	}
	b.st.Creds = &creds
	b.saveLocked(ctx)
	b.status = connector.Status{State: connector.StateOK, Account: account(b.lang(), &creds), Via: i18n.T(b.lang(), "wechat-wechat.via"), LastEvent: b.status.LastEvent}
	b.mu.Unlock()
	b.kick()
	return connector.Prompt{Done: true, Account: account(b.lang(), &creds)}
}

// Logout tells iLink the bot is off line and forgets the token (kept among the old ones, so binding again is known
// to be this bot).
func (b *Bot) Logout(ctx context.Context) error {
	if err := b.load(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	c := b.client
	b.qr = nil
	b.mu.Unlock()
	if c != nil {
		stop, cancel := context.WithTimeout(ctx, 5*time.Second)
		_ = c.NotifyStop(stop)
		cancel()
	}
	b.unbind(ctx, "")
	b.setStatus(connector.Status{State: connector.StateNotSignedIn, Error: i18n.T(b.lang(), "wechat-wechat.not_bound")})
	b.kick()
	return nil
}
