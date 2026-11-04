package wechat

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/connector"
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
			return codePrompt("填微信里显示的数字"), nil
		}
	}
	c, err := b.authClient()
	if err != nil {
		return connector.Prompt{}, err
	}
	st, err := c.PollQR(ctx, p.qr.Code, code, p.base, qrWait)
	if err != nil {
		if ilink.Retryable(err) {
			return p.prompt(), nil // asked again in a moment
		}
		return connector.Prompt{}, err
	}
	switch st.State {
	case ilink.QRWaiting:
		return p.prompt(), nil
	case ilink.QRScanned:
		b.mu.Lock()
		p.scanned = true
		b.mu.Unlock()
		return p.prompt(), nil
	case ilink.QRRedirect:
		b.mu.Lock()
		p.base = st.RedirectBase
		b.mu.Unlock()
		return p.prompt(), nil
	case ilink.QRNeedCode:
		if step == stepCode {
			return codePrompt("数字不对，再看一下微信里显示的"), nil
		}
		return codePrompt(""), nil
	case ilink.QRExpired:
		if p.renewed >= 3 {
			b.forgetQR(p)
			return connector.Prompt{}, errors.New("二维码过期太多次了，重新开始绑定")
		}
		return b.renewQR(ctx, p)
	case ilink.QRCodeBlocked:
		b.forgetQR(p)
		return connector.Prompt{}, errors.New("验证码错太多次，微信暂时不让绑定了，过一会儿再试")
	case ilink.QRBoundAlready:
		b.forgetQR(p)
		return connector.Prompt{}, errors.New("这个微信已经绑定过这个机器人了：在微信里把它删掉后再扫")
	case ilink.QRConfirmed:
		b.forgetQR(p)
		return b.bound(ctx, st.Credentials), nil
	}
	return p.prompt(), nil
}

func (p *pendingQR) prompt() connector.Prompt {
	hint := "用微信扫一扫这个二维码，在手机上确认后这里会自动继续。在同一部手机上：先保存二维码，再在微信里扫一扫、从相册选它。"
	if p.scanned {
		hint = "已经扫码了，在手机微信上点确认。"
	}
	return connector.Prompt{Step: stepQR, Kind: connector.PromptQRCode, URL: p.qr.Content, Interval: qrInterval, Hint: hint}
}

func codePrompt(errText string) connector.Prompt {
	return connector.Prompt{Step: stepCode, Fields: []connector.Field{{Name: "code", Label: "验证码", Placeholder: "微信里显示的数字"}},
		Hint: "微信要确认是你：把手机微信上显示的数字填在这里。", Error: errText}
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
	return p.prompt(), nil
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
	b.status = connector.Status{State: connector.StateOK, Account: account(&creds), Via: "iLink（微信 AI 机器人）", LastEvent: b.status.LastEvent}
	b.mu.Unlock()
	b.kick()
	return connector.Prompt{Done: true, Account: account(&creds)}
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
	b.setStatus(connector.Status{State: connector.StateNotSignedIn, Error: "还没绑定微信"})
	b.kick()
	return nil
}
