package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chyroc/jarvis/server/internal/mail"
	"github.com/chyroc/jarvis/server/internal/proto"
)

// mail_account_add: Jarvis adds a mail account for the user from the conversation (it used to be 「AI 一键添加」 in the
// app's add-account page, 2026-11-01). The phone approves it first; the hub then connects and saves only what works.

// mailAccountAdd parses the tool's arguments into the request, what the approval card says and the input it shows
// (without the password). gmail is an error: Google sign-in only happens in the app.
func mailAccountAdd(args json.RawMessage) (mail.AddAccountRequest, string, json.RawMessage, error) {
	var a struct {
		Email        string `json:"email"`
		Password     string `json:"password"`
		Provider     string `json:"provider"`
		Name         string `json:"name"`
		Username     string `json:"username"`
		IMAPHost     string `json:"imap_host"`
		IMAPPort     int    `json:"imap_port"`
		SMTPHost     string `json:"smtp_host"`
		SMTPPort     int    `json:"smtp_port"`
		SMTPSecurity string `json:"smtp_security"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return mail.AddAccountRequest{}, "", nil, fmt.Errorf("invalid arguments: %w", err)
	}
	email := strings.ToLower(strings.TrimSpace(a.Email))
	if !strings.Contains(email, "@") {
		return mail.AddAccountRequest{}, "", nil, errors.New("email is required")
	}
	if a.Provider == "gmail" || strings.HasSuffix(email, "@gmail.com") || strings.HasSuffix(email, "@googlemail.com") {
		return mail.AddAccountRequest{}, "", nil, errors.New("Gmail is added with Google sign-in, which only works in the app: " +
			"ask the user to open 设置 › 邮箱 › 添加邮箱, pick Gmail and tap 使用 Google 账号登录")
	}
	if a.Password == "" {
		return mail.AddAccountRequest{}, "", nil, errors.New("password is required (the login password or the provider's app password / 授权码)")
	}
	provider := a.Provider
	if provider == "" {
		provider = mail.ProviderIMAP
	}
	req := mail.AddAccountRequest{Name: strings.TrimSpace(a.Name), Email: email, Provider: provider, Auth: mail.AuthPassword,
		Username: a.Username, Password: a.Password, IMAPHost: a.IMAPHost, IMAPPort: a.IMAPPort, SMTPHost: a.SMTPHost,
		SMTPPort: a.SMTPPort, SMTPSecurity: a.SMTPSecurity}
	servers := "服务器自动识别"
	if provider == mail.ProviderFeishu {
		servers = "飞书邮箱的服务器"
	} else if a.IMAPHost != "" || a.SMTPHost != "" {
		servers = fmt.Sprintf("IMAP %s · SMTP %s", hostPort(a.IMAPHost, a.IMAPPort), hostPort(a.SMTPHost, a.SMTPPort))
	}
	summary := fmt.Sprintf("添加邮箱 %s（%s）", email, servers)
	shown, _ := json.Marshal(map[string]any{"email": email, "provider": provider, "name": req.Name, "username": a.Username,
		"imap_host": a.IMAPHost, "imap_port": a.IMAPPort, "smtp_host": a.SMTPHost, "smtp_port": a.SMTPPort, "password": "••••••"})
	return req, summary, shown, nil
}

func hostPort(host string, port int) string {
	switch {
	case host == "":
		return "自动"
	case port == 0:
		return host
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func (h *Hub) toolMailAccountAdd(ctx context.Context, m *Machine, sessionID string, args json.RawMessage) (string, error) {
	if h.mail == nil {
		return "", errors.New("mail is not set up on this hub")
	}
	req, summary, shown, err := mailAccountAdd(args)
	if err != nil {
		return "", err
	}
	ix := proto.Interaction{ID: "ix_" + randomHex(6), MachineID: m.cfg.ID, SessionID: sessionID, Kind: proto.IxPermission,
		ToolName: "mail_account_add", Summary: summary, ToolInput: shown, SessionTitle: "Jarvis",
		CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(15 * time.Minute).UTC()}
	r, err := h.waitInteraction(ctx, ix)
	if err != nil {
		return "", err
	}
	if r.Allow == nil || !*r.Allow {
		return "The user declined on the phone: the account was not added.", nil
	}
	a, err := h.mail.AddAccount(req)
	if err != nil {
		return "", fmt.Errorf("could not add %s: %w (the settings were not saved; a login error usually means the provider wants an app password / 授权码)", req.Email, err)
	}
	h.broadcast(map[string]any{"type": "mail_accounts"})
	return fmt.Sprintf("Added %s (account id %s, provider %s). It shows in the app's inbox from the next collection; mail_* tools can use it now.",
		a.Email, a.ID, a.Provider), nil
}
