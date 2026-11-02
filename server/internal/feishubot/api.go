package feishubot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Domains of the two clouds; a hub.json domain may also be a full URL (a private deployment).
const (
	DomainFeishu = "https://open.feishu.cn"
	DomainLark   = "https://lark-connectorsuite.com"
)

// ResolveDomain turns the configured domain ("", "feishu", "lark" or a URL) into a base URL.
func ResolveDomain(d string) string {
	switch strings.ToLower(strings.TrimSpace(d)) {
	case "", "feishu":
		return DomainFeishu
	case "lark", "larksuite":
		return DomainLark
	}
	return strings.TrimRight(strings.TrimSpace(d), "/")
}

// Client makes the app's REST calls with a tenant access token it keeps fresh.
type Client struct {
	appID, appSecret string
	domain           string
	http             *http.Client

	mu      sync.Mutex
	token   string
	expires time.Time
}

func New(appID, appSecret, domain string) *Client {
	return &Client{appID: appID, appSecret: appSecret, domain: ResolveDomain(domain), http: &http.Client{Timeout: 30 * time.Second}}
}

// WithHTTP replaces the HTTP client (tests).
func (c *Client) WithHTTP(h *http.Client) *Client {
	c.http = h
	return c
}

func (c *Client) AppID() string { return c.appID }

// APIError is Feishu's {code, msg} for a failed call.
type APIError struct {
	Op   string
	Code int
	Msg  string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("feishu %s: %d %s", e.Op, e.Code, e.Msg)
}

// Token-related codes: the token expired or is not valid any more; one fresh token and one retry fix it.
func tokenGone(code int) bool { return code == 99991663 || code == 99991664 || code == 99991661 }

func (c *Client) tenantToken(ctx context.Context, fresh bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !fresh && c.token != "" && time.Now().Before(c.expires) {
		return c.token, nil
	}
	body, _ := json.Marshal(map[string]string{"app_id": c.appID, "app_secret": c.appSecret})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.domain+"/open-apis/auth/v3/tenant_access_token/internal", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Code   int    `json:"code"`
		Msg    string `json:"msg"`
		Token  string `json:"tenant_access_token"`
		Expire int    `json:"expire"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("feishu token: HTTP %d: %w", resp.StatusCode, err)
	}
	if out.Code != 0 || out.Token == "" {
		return "", &APIError{Op: "token", Code: out.Code, Msg: out.Msg}
	}
	c.token = out.Token
	c.expires = time.Now().Add(time.Duration(max(out.Expire-300, 60)) * time.Second)
	return c.token, nil
}

// call sends one JSON request and decodes {code, msg, data} into out (data's fields, or the whole body for calls
// whose payload is not under data).
func (c *Client) call(ctx context.Context, op, method, path string, in, out any) error {
	var payload []byte
	if in != nil {
		payload, _ = json.Marshal(in)
	}
	for attempt := 0; ; attempt++ {
		tok, err := c.tenantToken(ctx, attempt > 0)
		if err != nil {
			return err
		}
		var body io.Reader
		if payload != nil {
			body = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.domain+path, body)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json; charset=utf-8")
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return err
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if err != nil {
			return err
		}
		var env struct {
			Code int             `json:"code"`
			Msg  string          `json:"msg"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			return fmt.Errorf("feishu %s: HTTP %d: %w", op, resp.StatusCode, err)
		}
		if env.Code != 0 {
			if tokenGone(env.Code) && attempt == 0 {
				continue
			}
			return &APIError{Op: op, Code: env.Code, Msg: env.Msg}
		}
		if out == nil {
			return nil
		}
		if len(env.Data) > 0 && string(env.Data) != "null" {
			return json.Unmarshal(env.Data, out)
		}
		return json.Unmarshal(raw, out)
	}
}

// BotInfo is the app's bot: its name and open id (GET /open-apis/bot/v3/info). Asking for it also checks the app's
// id and secret.
type BotInfo struct {
	Name   string `json:"app_name"`
	OpenID string `json:"open_id"`
	Avatar string `json:"avatar_url"`
}

func (c *Client) BotInfo(ctx context.Context) (BotInfo, error) {
	var out struct {
		Bot BotInfo `json:"bot"`
	}
	err := c.call(ctx, "bot info", http.MethodGet, "/open-apis/bot/v3/info", nil, &out)
	return out.Bot, err
}

// Message types the bot sends.
const (
	TypeText        = "text"
	TypePost        = "post"
	TypeInteractive = "interactive"
)

type sent struct {
	MessageID string `json:"message_id"`
}

// Send posts a message to a user (idType "open_id") or into a chat ("chat_id"); uuid (≤ 50 chars, may be "") makes
// a retried send land once.
func (c *Client) Send(ctx context.Context, idType, id, msgType, content, uuid string) (string, error) {
	var out sent
	in := map[string]any{"receive_id": id, "msg_type": msgType, "content": content}
	if uuid != "" {
		in["uuid"] = uuid
	}
	err := c.call(ctx, "send", http.MethodPost, "/open-apis/im/v1/messages?receive_id_type="+url.QueryEscape(idType), in, &out)
	return out.MessageID, err
}

// Reply answers a message (quoting it); inThread puts the answer in the message's thread.
func (c *Client) Reply(ctx context.Context, messageID, msgType, content, uuid string, inThread bool) (string, error) {
	var out sent
	in := map[string]any{"msg_type": msgType, "content": content}
	if uuid != "" {
		in["uuid"] = uuid
	}
	if inThread {
		in["reply_in_thread"] = true
	}
	err := c.call(ctx, "reply", http.MethodPost, "/open-apis/im/v1/messages/"+url.PathEscape(messageID)+"/reply", in, &out)
	return out.MessageID, err
}

// React puts an emoji under a message (emoji_type: OK, DONE, OnIt…) and returns the reaction's id.
func (c *Client) React(ctx context.Context, messageID, emoji string) (string, error) {
	var out struct {
		ReactionID string `json:"reaction_id"`
	}
	err := c.call(ctx, "react", http.MethodPost, "/open-apis/im/v1/messages/"+url.PathEscape(messageID)+"/reactions",
		map[string]any{"reaction_type": map[string]string{"emoji_type": emoji}}, &out)
	return out.ReactionID, err
}

// Unreact takes a reaction away.
func (c *Client) Unreact(ctx context.Context, messageID, reactionID string) error {
	return c.call(ctx, "unreact", http.MethodDelete, "/open-apis/im/v1/messages/"+url.PathEscape(messageID)+"/reactions/"+url.PathEscape(reactionID), nil, nil)
}

// UpdateCard replaces a card the bot sent (shared cards: update_multi).
func (c *Client) UpdateCard(ctx context.Context, messageID, card string) error {
	return c.call(ctx, "update card", http.MethodPatch, "/open-apis/im/v1/messages/"+url.PathEscape(messageID), map[string]any{"content": card}, nil)
}

// Item is a message read back (GET /open-apis/im/v1/messages/:id): a quoted message, or a merged forward and the
// messages in it (those have UpperMessageID set).
type Item struct {
	MessageID      string `json:"message_id"`
	ParentID       string `json:"parent_id"`
	UpperMessageID string `json:"upper_message_id"`
	MsgType        string `json:"msg_type"`
	CreateTime     string `json:"create_time"` // Unix milliseconds
	Deleted        bool   `json:"deleted"`
	Sender         struct {
		ID         string `json:"id"`
		IDType     string `json:"id_type"`
		SenderType string `json:"sender_type"` // user | app
	} `json:"sender"`
	Body struct {
		Content string `json:"content"`
	} `json:"body"`
	Mentions []struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	} `json:"mentions"`
}

// At is when the message was sent.
func (it Item) At() time.Time { return msTime(it.CreateTime) }

func (c *Client) Message(ctx context.Context, messageID string) ([]Item, error) {
	var out struct {
		Items []Item `json:"items"`
	}
	err := c.call(ctx, "read message", http.MethodGet, "/open-apis/im/v1/messages/"+url.PathEscape(messageID)+"?user_id_type=open_id", nil, &out)
	return out.Items, err
}

// UserName is a user's name (contact API; needs a contact permission, so "" with an error when the app lacks it).
func (c *Client) UserName(ctx context.Context, openID string) (string, error) {
	var out struct {
		User struct {
			Name string `json:"name"`
		} `json:"user"`
	}
	err := c.call(ctx, "user", http.MethodGet, "/open-apis/contact/v3/users/"+url.PathEscape(openID)+"?user_id_type=open_id", nil, &out)
	return out.User.Name, err
}

// Resource downloads an image or file of a message the bot received (kind "image" or "file"); at most limit bytes.
func (c *Client) Resource(ctx context.Context, messageID, key, kind string, limit int64) ([]byte, string, error) {
	for attempt := 0; ; attempt++ {
		tok, err := c.tenantToken(ctx, attempt > 0)
		if err != nil {
			return nil, "", err
		}
		u := c.domain + "/open-apis/im/v1/messages/" + url.PathEscape(messageID) + "/resources/" + url.PathEscape(key) + "?type=" + url.QueryEscape(kind)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, "", err
		}
		ct := resp.Header.Get("Content-Type")
		if resp.StatusCode != http.StatusOK || strings.HasPrefix(ct, "application/json") {
			var env struct {
				Code int    `json:"code"`
				Msg  string `json:"msg"`
			}
			_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&env)
			resp.Body.Close()
			if tokenGone(env.Code) && attempt == 0 {
				continue
			}
			if env.Code == 0 {
				env.Code, env.Msg = resp.StatusCode, http.StatusText(resp.StatusCode)
			}
			return nil, "", &APIError{Op: "download", Code: env.Code, Msg: env.Msg}
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		resp.Body.Close()
		if err != nil {
			return nil, "", err
		}
		if int64(len(b)) > limit {
			return nil, "", fmt.Errorf("feishu download: larger than %d MB", limit>>20)
		}
		return b, ct, nil
	}
}

// CardRejected: Feishu refused a card's content (a field it does not take, Markdown it cannot read, too large); the
// hub sends the text as rich text instead.
func CardRejected(err error) bool {
	var e *APIError
	if !errors.As(err, &e) {
		return false
	}
	switch e.Code {
	case 230099, 230025, 200621, 200830:
		return true
	}
	return strings.Contains(strings.ToLower(e.Msg), "card")
}
