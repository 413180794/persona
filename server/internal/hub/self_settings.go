package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/mail"
	"github.com/nwverse/persona/server/internal/proto"
)

// The hub's own settings jarvis_settings offers (docs/assistant.md「操作 Persona 自己」, 2026-11-01): the tool is the
// assistant's (its own settings are its own: 关于我, its launcher, …), which lists these (app.call settings.list) and
// hands the actions on them to the hub (app.tool jarvis_settings). One registry: each setting says what it is, how to
// read it and how a change is made. Reads are free; every change is a card on the phone that says what changes (before
// → after) and is applied only when approved. What the card needs filled in (a password) is typed on the phone and
// never passes through the model. Some things are never here: the hub's and devices' tokens, pairing, which machines
// the hub reaches, permission modes.

// selfSetting is one thing Persona may change. A value setting has set; a list has add / remove.
type selfSetting struct {
	key, title, about string
	value             string // what set / add take as value, for the model
	get               func(ctx context.Context) (string, error)
	set               func(ctx context.Context, v json.RawMessage) (*selfChange, error)
	add               func(ctx context.Context, v json.RawMessage) (*selfChange, error)
	remove            func(ctx context.Context, id string) (*selfChange, error)
}

// selfChange is a change waiting for the phone: what the card says, what it asks for, and how it is made.
type selfChange struct {
	summary string
	fields  []proto.Field
	apply   func(ctx context.Context, values map[string]string) (string, error)
}

func (s selfSetting) actions() []string {
	a := []string{"get"}
	if s.set != nil {
		a = append(a, "set")
	}
	if s.add != nil {
		a = append(a, "add")
	}
	if s.remove != nil {
		a = append(a, "remove")
	}
	return a
}

// selfSettings is the registry (what this hub has: mail only with mail set up).
func (h *Hub) selfSettings() []selfSetting {
	out := []selfSetting{
		{key: "inbox.rules", title: "收件箱分拣规则",
			about: "the user's own rules for sorting the inbox (需要你 / 知悉即可 / 噪音), in their words; they win over the defaults " +
				"(a source with rules of its own, inbox.rules.<plugin>, uses those instead)",
			value: `a string (the whole rules text; "" clears it and falls back to hub.json)`,
			get:   func(ctx context.Context) (string, error) { return orNone(h.inboxRules(ctx)), nil },
			set: func(ctx context.Context, v json.RawMessage) (*selfChange, error) {
				return h.textSetting(ctx, v, "收件箱分拣规则", h.inboxRules, "inbox.rules")
			}},
		{key: "inbox.agent_rules", title: "AI 分身的规则",
			about: "the user's rules for which 需要你 inbox items the AI 分身 takes on and drafts a reply for",
			value: `a string ("" clears it)`,
			get:   func(ctx context.Context) (string, error) { return orNone(h.inboxAgentRules(ctx)), nil },
			set: func(ctx context.Context, v json.RawMessage) (*selfChange, error) {
				return h.textSetting(ctx, v, "AI 分身的规则", h.inboxAgentRules, "inbox.agent_rules")
			}},
	}
	if h.store != nil {
		out = append(out, h.blockedClaudeDirsSetting())
	}
	out = append(out, h.sourceRulesSettings()...)
	if h.mail != nil {
		out = append(out, selfSetting{key: "mail.accounts", title: "邮箱",
			about: "the user's mail accounts: Persona reads them in the inbox and mail_* use them. add connects (IMAP and SMTP) before saving; " +
				"the password is typed on the phone, never pass one. Gmail can only be added in the app (Google sign-in)",
			value: `add: {"email", "provider": "imap|feishu" (default imap; feishu = Feishu / Lark mail), "name"?, "username"?, "imap_host"?, ` +
				`"imap_port"?, "smtp_host"?, "smtp_port"?, "smtp_security"?: "tls|starttls"}; most providers need only the email. remove: the account id`,
			get: h.getMailAccounts, add: h.addMailAccount, remove: h.removeMailAccount})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

func (h *Hub) toolJarvisSettings(ctx context.Context, m *Machine, sessionID string, args json.RawMessage) (string, error) {
	var a struct {
		Action string          `json:"action"`
		Key    string          `json:"key"`
		Value  json.RawMessage `json:"value"`
		ID     string          `json:"id"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	all := h.selfSettings() // the list is the assistant's (it adds its own: settings.list)
	var s *selfSetting
	for i := range all {
		if all[i].key == a.Key {
			s = &all[i]
		}
	}
	if s == nil {
		return "", fmt.Errorf("no setting %q (action list shows them)", a.Key)
	}
	var change *selfChange
	var err error
	switch {
	case a.Action == "get":
		return s.get(ctx)
	case a.Action == "set" && s.set != nil:
		change, err = s.set(ctx, a.Value)
	case a.Action == "add" && s.add != nil:
		change, err = s.add(ctx, a.Value)
	case a.Action == "remove" && s.remove != nil:
		change, err = s.remove(ctx, a.ID)
	default:
		return "", fmt.Errorf("%s does not take %q (it takes %s)", s.key, a.Action, strings.Join(s.actions(), ", "))
	}
	if err != nil {
		return "", err
	}
	if change.apply == nil {
		return change.summary, nil // nothing to change
	}
	shown, _ := json.Marshal(map[string]string{"setting": s.title, "action": a.Action})
	ix := proto.Interaction{ID: "ix_" + randomHex(6), MachineID: m.cfg.ID, SessionID: sessionID, Kind: proto.IxPermission,
		ToolName: "jarvis_settings", Summary: change.summary, ToolInput: shown, Fields: change.fields, SessionTitle: "Persona",
		CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(15 * time.Minute).UTC()}
	r, err := h.waitInteraction(ctx, ix)
	if err != nil {
		return "", err
	}
	if r.Allow == nil || !*r.Allow {
		return "The user declined on the phone: nothing changed.", nil
	}
	for _, f := range change.fields {
		if !f.Optional && strings.TrimSpace(r.Values[f.Key]) == "" {
			return "", fmt.Errorf("%s was left empty on the phone: nothing changed", f.Label)
		}
	}
	return change.apply(ctx, r.Values)
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "（无）"
	}
	return s
}

// clipped is a value as the card shows it.
func clipped(s string) string { return clip(oneLine(orNone(s)), 120) }

// ---- inbox rules (settings in Postgres win over hub.json) ----

func (h *Hub) settingText(ctx context.Context, key, fallback string) string {
	if h.store != nil {
		var v string
		if ok, err := h.store.Setting(ctx, key, &v); err == nil && ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return strings.TrimSpace(fallback)
}

func (h *Hub) inboxRules(ctx context.Context) string {
	return h.settingText(ctx, "inbox.rules", h.cfg.Inbox.Rules)
}
func (h *Hub) inboxAgentRules(ctx context.Context) string {
	return h.settingText(ctx, "inbox.agent_rules", h.cfg.Inbox.Agent.Rules)
}

// textSetting changes a text setting kept in Postgres under key ("" deletes the override).
func (h *Hub) textSetting(ctx context.Context, v json.RawMessage, title string, now func(context.Context) string, key string) (*selfChange, error) {
	var text string
	if err := json.Unmarshal(v, &text); err != nil {
		return nil, errors.New("value must be a string")
	}
	text = strings.TrimSpace(text)
	before := now(ctx)
	if text == before {
		return &selfChange{summary: title + "没有变化"}, nil
	}
	if h.store == nil {
		return nil, errors.New("no database on this hub")
	}
	return &selfChange{summary: fmt.Sprintf("改%s：%s → %s", title, clipped(before), clipped(text)),
		apply: func(ctx context.Context, _ map[string]string) (string, error) {
			if err := h.store.PutSetting(ctx, key, text); err != nil {
				return "", err
			}
			return title + " updated; it applies from the next items sorted.", nil
		}}, nil
}

// ---- mail accounts ----

func (h *Hub) getMailAccounts(context.Context) (string, error) {
	as, err := h.mail.ListAccounts()
	if err != nil {
		return "", err
	}
	if len(as) == 0 {
		return "（无）", nil
	}
	var b strings.Builder
	for _, a := range as {
		fmt.Fprintf(&b, "- %s (id %s): %s, provider %s\n", a.Name, a.ID, a.Email, a.Provider)
	}
	return b.String(), nil
}

func (h *Hub) addMailAccount(_ context.Context, v json.RawMessage) (*selfChange, error) {
	req, summary, err := mailAccountRequest(v)
	if err != nil {
		return nil, err
	}
	return &selfChange{summary: summary,
		fields: []proto.Field{{Key: "password", Label: "密码 / 授权码", Placeholder: "国内邮箱多半要授权码", Secret: true}},
		apply: func(_ context.Context, values map[string]string) (string, error) {
			req.Password = values["password"]
			a, err := h.mail.AddAccount(req)
			if err != nil {
				return "", fmt.Errorf("could not add %s: %w (nothing saved; a login error usually means the provider wants an app password / 授权码)", req.Email, err)
			}
			h.broadcast(map[string]any{"type": "mail_accounts"})
			return fmt.Sprintf("Added %s (account id %s, provider %s): the inbox reads it from its next collection; mail_* can use it now.",
				a.Email, a.ID, a.Provider), nil
		}}, nil
}

// mailAccountRequest reads add's value into the request and what the card says. Gmail is an error: Google sign-in
// only happens in the app.
func mailAccountRequest(v json.RawMessage) (mail.AddAccountRequest, string, error) {
	var a struct {
		Email        string `json:"email"`
		Provider     string `json:"provider"`
		Name         string `json:"name"`
		Username     string `json:"username"`
		IMAPHost     string `json:"imap_host"`
		IMAPPort     int    `json:"imap_port"`
		SMTPHost     string `json:"smtp_host"`
		SMTPPort     int    `json:"smtp_port"`
		SMTPSecurity string `json:"smtp_security"`
	}
	if err := json.Unmarshal(v, &a); err != nil {
		return mail.AddAccountRequest{}, "", fmt.Errorf("value: %w", err)
	}
	email := strings.ToLower(strings.TrimSpace(a.Email))
	if !strings.Contains(email, "@") {
		return mail.AddAccountRequest{}, "", errors.New("email is required")
	}
	if a.Provider == "gmail" || strings.HasSuffix(email, "@gmail.com") || strings.HasSuffix(email, "@googlemail.com") {
		return mail.AddAccountRequest{}, "", errors.New("Gmail is added with Google sign-in, which only works in the app: " +
			"ask the user to open 设置 › 邮箱 › 添加邮箱, pick Gmail and tap 使用 Google 账号登录")
	}
	provider := a.Provider
	if provider == "" {
		provider = mail.ProviderIMAP
	}
	req := mail.AddAccountRequest{Name: strings.TrimSpace(a.Name), Email: email, Provider: provider, Auth: mail.AuthPassword,
		Username: a.Username, IMAPHost: a.IMAPHost, IMAPPort: a.IMAPPort, SMTPHost: a.SMTPHost, SMTPPort: a.SMTPPort,
		SMTPSecurity: a.SMTPSecurity}
	servers := "服务器自动识别"
	if provider == mail.ProviderFeishu {
		servers = "飞书邮箱的服务器"
	} else if a.IMAPHost != "" || a.SMTPHost != "" {
		servers = fmt.Sprintf("IMAP %s · SMTP %s", hostPort(a.IMAPHost, a.IMAPPort), hostPort(a.SMTPHost, a.SMTPPort))
	}
	return req, fmt.Sprintf("添加邮箱 %s（%s），填上密码后先连一遍再保存", email, servers), nil
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

func (h *Hub) removeMailAccount(_ context.Context, id string) (*selfChange, error) {
	as, err := h.mail.ListAccounts()
	if err != nil {
		return nil, err
	}
	for _, a := range as {
		if a.ID == id || strings.EqualFold(a.Email, id) {
			acct := a
			return &selfChange{summary: fmt.Sprintf("删除邮箱 %s（Persona 不再收它的邮件，邮箱里的邮件不受影响）", acct.Email),
				apply: func(context.Context, map[string]string) (string, error) {
					if err := h.mail.DeleteAccount(acct.ID); err != nil {
						return "", err
					}
					h.broadcast(map[string]any{"type": "mail_accounts"})
					return "Removed " + acct.Email + ".", nil
				}}, nil
		}
	}
	return nil, fmt.Errorf("no mail account %q (get lists them)", id)
}
