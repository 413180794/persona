package lark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf16"
)

var nativeOLKToken = regexp.MustCompile(`^[A-Za-z0-9_-]{43,128}$`)
var nativeOLKAccount = regexp.MustCompile(`^[0-9]{1,32}$`)
var nativeOLKFeed = regexp.MustCompile(`^[0-9]{1,20}$`)

// NativeOLKError has only stable, safe messages; upstream bodies can contain passwords or plaintext.
type NativeOLKError struct {
	Status        int
	Code, Message string
}

func (e *NativeOLKError) Error() string { return e.Message }
func nativeOLKFailure(code string) *NativeOLKError {
	status, message := http.StatusBadGateway, "加密服务暂时不可用，请稍后重试"
	switch code {
	case "OLK_VAULT_NATIVE_NOT_AUTHORIZED":
		status, message = 403, "Hub 尚未获得手机加密授权，请在 Hub 上配置"
	case "OLK_VAULT_ACCOUNT_CHANGED":
		status, message = 409, "飞书账号已变化，请重新打开会话"
	case "OLK_VAULT_LOCKED":
		status, message = 409, "请先解锁密钥库"
	case "OLK_VAULT_NOT_CONFIGURED":
		status, message = 409, "请先在 lark-connector 网页创建密钥库"
	case "OLK_VAULT_INVALID_PASSWORD", "OLK_VAULT_INVALID_LOGIN_PASSWORD":
		status, message = 401, "密码不正确，请重试"
	case "OLK_VAULT_RATE_LIMITED", "OLK_VAULT_LOGIN_RATE_LIMITED":
		status, message = 429, "密码尝试过多，请稍后重试"
	case "OLK_VAULT_LOGIN_PASSWORD_RESET":
		status, message = 409, "登录密码已重置，请先在 lark-connector 网页恢复密钥库"
	case "OLK_VAULT_NO_RECIPIENTS":
		status, message = 409, "会话中还没有其他成员发布加密公钥"
	default:
		code = "lark_encryption_unavailable"
	}
	return &NativeOLKError{Status: status, Code: code, Message: message}
}

type NativeOLKStatus struct {
	AccountID       string `json:"accountId"`
	Configured      bool   `json:"configured"`
	Locked          bool   `json:"locked"`
	UnifiedPassword bool   `json:"unifiedPassword"`
}
type NativeOLKPrepared struct {
	AccountID string                `json:"accountId"`
	FeedID    string                `json:"feedId"`
	Messages  []NativeOLKCiphertext `json:"messages"`
}
type NativeOLKCiphertext struct {
	Text string `json:"text"`
}

func (c *Client) nativeOLK(ctx context.Context, method, path string, body, out any) error {
	if c.cfg.NativeOLKToken == "" {
		return nativeOLKFailure("OLK_VAULT_NATIVE_NOT_AUTHORIZED")
	}
	token, err := c.agentToken(ctx)
	if err != nil {
		return nativeOLKFailure("")
	}
	var data []byte
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return nativeOLKFailure("")
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.cfg.BaseURL, "/")+"/olk/native/"+path, bytes.NewReader(data))
	if err != nil {
		return nativeOLKFailure("")
	}
	c.authorize(req, token)
	req.Header.Set("x-lark-connector-native-olk-token", c.cfg.NativeOLKToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := *c.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return nativeOLKFailure("")
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, (4<<20)+1))
	if err != nil || len(raw) > 4<<20 {
		return nativeOLKFailure("")
	}
	if res.StatusCode/100 != 2 {
		code, _ := upstreamError(raw)
		return nativeOLKFailure(code)
	}
	if err = json.Unmarshal(raw, out); err != nil {
		return nativeOLKFailure("")
	}
	return nil
}

func (c *Client) NativeOLKStatus(ctx context.Context) (NativeOLKStatus, error) {
	var out NativeOLKStatus
	err := c.nativeOLK(ctx, "GET", "status", nil, &out)
	if err == nil && !nativeOLKAccount.MatchString(out.AccountID) {
		err = nativeOLKFailure("")
	}
	return out, err
}
func (c *Client) NativeOLKUnlock(ctx context.Context, account, password string) (NativeOLKStatus, error) {
	var out NativeOLKStatus
	err := c.nativeOLK(ctx, "POST", "unlock", map[string]string{"accountId": account, "password": password}, &out)
	if err == nil && (out.AccountID != account || out.Locked || !out.Configured) {
		err = nativeOLKFailure("OLK_VAULT_ACCOUNT_CHANGED")
	}
	return out, err
}
func (c *Client) NativeOLKCovers(ctx context.Context, account, feed string) ([]string, error) {
	var out struct {
		Lines []string `json:"lines"`
	}
	err := c.nativeOLK(ctx, "POST", "replay-covers", map[string]string{"accountId": account, "feedId": feed}, &out)
	if out.Lines == nil {
		out.Lines = []string{}
	}
	return out.Lines, err
}
func (c *Client) NativeOLKEncrypt(ctx context.Context, account, feed, text, cover string) (NativeOLKPrepared, error) {
	var out NativeOLKPrepared
	err := c.nativeOLK(ctx, "POST", "encrypt", map[string]string{"accountId": account, "feedId": feed, "text": text, "cover": cover}, &out)
	if err != nil {
		return NativeOLKPrepared{}, err
	}
	if out.AccountID != account || out.FeedID != feed {
		return NativeOLKPrepared{}, nativeOLKFailure("OLK_VAULT_ACCOUNT_CHANGED")
	}
	if len(out.Messages) == 0 || len(out.Messages) > 256 {
		return NativeOLKPrepared{}, nativeOLKFailure("")
	}
	for _, message := range out.Messages {
		if !ValidOLKCiphertext(message.Text) || strings.TrimRightFunc(message.Text, func(r rune) bool { return r >= 0xfe00 && r <= 0xfe0f || r >= 0xe0100 && r <= 0xe01ef }) != cover {
			return NativeOLKPrepared{}, nativeOLKFailure("")
		}
	}
	return out, nil
}

// Check the public wire shape, not the cryptography: only the vault prepares and signs envelopes.
func ValidOLKCiphertext(text string) bool {
	if len(utf16.Encode([]rune(text))) > 10000 {
		return false
	}
	payload := []byte{}
	started := false
	cover := false
	for _, char := range text {
		var value int
		switch {
		case char >= 0xfe00 && char <= 0xfe0f:
			value = int(char - 0xfe00)
		case char >= 0xe0100 && char <= 0xe01ef:
			value = int(char - 0xe0100 + 16)
		default:
			if started {
				return false
			}
			cover = true
			continue
		}
		started = true
		payload = append(payload, byte(value))
	}
	return cover && len(payload) > 5 && (string(payload[:5]) == "OLK2." || string(payload[:5]) == "OLK3.")
}

// The account is bound again at lark-connector's write boundary; a preflight alone cannot prevent a login race.
func (c *Client) SendOLKCiphertext(ctx context.Context, account, feed, text, clientID, reply string) (string, error) {
	if !nativeOLKAccount.MatchString(account) || !nativeOLKFeed.MatchString(feed) || !ValidClientID(clientID) || !ValidOLKCiphertext(text) || (reply != "" && !nativeOLKFeed.MatchString(reply)) {
		return "", errors.New("加密消息无效，未发送")
	}
	status, err := c.NativeOLKStatus(ctx)
	if err != nil {
		return "", err
	}
	if status.AccountID != account {
		return "", nativeOLKFailure("OLK_VAULT_ACCOUNT_CHANGED")
	}
	body := map[string]any{"text": text, "expectedSenderId": account}
	path := "/conversations/" + url.PathEscape(feed) + "/messages"
	if reply != "" {
		path = "/messages/" + url.PathEscape(reply) + "/replies"
	}
	return c.post(ctx, path, body, clientID)
}
