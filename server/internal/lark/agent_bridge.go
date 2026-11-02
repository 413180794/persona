package lark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// lark-connector's agent-bridge (personal agents: Cue, Grok Bot over their APIs; Muse, Dots, Claude Duet through the
// server's browser), as the legacy agent API exposes it under /agent/v1/agent-bridge* and /agent/v1/agent-sessions*.
// Shapes follow what the deployment actually returns (see removedagent-bridge-facts.md); unknown fields are ignored and
// unknown block types are kept verbatim. These are dedicated typed calls: Raw / Read stay read-only.

// AgentMaxText is the longest message the bridge accepts (characters, as JavaScript counts them).
const AgentMaxText = 20000

// APIError is a non-2xx answer from lark-connector, with its HTTP status kept so callers can map it (403 → read-only
// token, 404 → unknown session, 409 → account unavailable…).
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("lark: %s (%s, HTTP %d)", e.Message, e.Code, e.Status)
	}
	return fmt.Sprintf("lark: HTTP %d", e.Status)
}

// ErrForbidden marks a 403: typically a read-only agent token used for a write.
var ErrForbidden = errors.New("lark: lark-connector refused (insufficient token permission)")

func (e *APIError) Is(target error) bool {
	return target == ErrForbidden && e.Status == http.StatusForbidden
}

type AgentProvider struct {
	Provider   string `json:"provider"`
	Connection string `json:"connection,omitempty"` // server | browser
	Available  bool   `json:"available"`
}

type AgentAccount struct {
	AccountID         string `json:"accountId"`
	Provider          string `json:"provider"`
	DisplayName       string `json:"displayName,omitempty"`
	Status            string `json:"status,omitempty"` // active, paused, …
	ExternalAccountID string `json:"externalAccountId,omitempty"`
	LastSyncedAt      int64  `json:"lastSyncedAt,omitempty"`
	LastError         string `json:"lastError,omitempty"`
	SessionCount      int    `json:"sessionCount"`
}

type AgentBridgeState struct {
	Enabled      bool            `json:"enabled"`
	MixIntoInbox bool            `json:"mixIntoInbox"`
	Providers    []AgentProvider `json:"providers"`
	Accounts     []AgentAccount  `json:"accounts"`
}

type AgentSession struct {
	SessionKey       string `json:"sessionKey"` // e.g. agent:grok_bot:<accountId>:<uuid>/ or agent:muse:<accountId>:main
	AccountID        string `json:"accountId"`
	Provider         string `json:"provider"`
	Title            string `json:"title"`
	Kind             string `json:"kind"` // main, …
	ParentSessionKey string `json:"parentSessionKey,omitempty"`
	CanSend          bool   `json:"canSend"`
	UnreadCount      int    `json:"unreadCount"`
	RankTimeMs       int64  `json:"rankTimeMs"`
}

// AgentBlock is one block of a message: text, tool-call {name,status,summary,input}, tool-result, or anything
// newer, whose JSON is kept in Raw and written back unchanged.
type AgentBlock struct {
	Type    string          `json:"type"`
	Text    string          `json:"text,omitempty"`
	Name    string          `json:"name,omitempty"`
	Status  string          `json:"status,omitempty"`
	Summary string          `json:"summary,omitempty"`
	Input   json.RawMessage `json:"input,omitempty"`
	Raw     json.RawMessage `json:"-"`
}

func (b *AgentBlock) UnmarshalJSON(data []byte) error {
	type plain AgentBlock
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*b = AgentBlock(p)
	b.Raw = append(json.RawMessage(nil), data...)
	return nil
}

func (b AgentBlock) MarshalJSON() ([]byte, error) {
	if len(b.Raw) > 0 {
		return b.Raw, nil
	}
	type plain AgentBlock
	return json.Marshal(plain(b))
}

type AgentLocal struct {
	ClientID string `json:"clientId"`
	Status   string `json:"status"` // pending | claimed | sent | unconfirmed | failed
}

type AgentMessage struct {
	ExternalID   string       `json:"externalId"`
	SessionKey   string       `json:"sessionKey"`
	Role         string       `json:"role"` // user | assistant | tool (seen); others possible
	Blocks       []AgentBlock `json:"blocks"`
	CreateTimeMs int64        `json:"createTimeMs"`
	Position     int64        `json:"position"`
	Local        *AgentLocal  `json:"local,omitempty"` // only on messages sent through the bridge
}

type AgentMessagePage struct {
	Session    *AgentSession  `json:"session,omitempty"`
	Messages   []AgentMessage `json:"messages"`
	HasMore    bool           `json:"hasMore"`
	NextCursor string         `json:"nextCursor,omitempty"` // opaque; pass back as cursor for older messages
}

type AgentSearchHit struct {
	SessionKey   string `json:"sessionKey"`
	ExternalID   string `json:"externalId"`
	Text         string `json:"text"`
	Role         string `json:"role"`
	CreateTimeMs int64  `json:"createTimeMs"`
	Title        string `json:"title"`
	Provider     string `json:"provider"`
}

// AgentFeed is GET /agent-bridge/feed: sessions shaped as lark-connector feed items (ids prefixed "agent:"); kept raw.
type AgentFeed struct {
	Enabled      bool              `json:"enabled"`
	MixIntoInbox bool              `json:"mixIntoInbox"`
	Items        []json.RawMessage `json:"items"`
}

func agentSessionPath(key string) string { return "/agent-sessions/" + url.PathEscape(key) }

func (c *Client) AgentBridgeStatus(ctx context.Context) (*AgentBridgeState, error) {
	var out AgentBridgeState
	return &out, c.doAgent(ctx, "GET", "/agent-bridge", nil, nil, &out)
}

func (c *Client) AgentBridgeFeed(ctx context.Context) (*AgentFeed, error) {
	var out AgentFeed
	return &out, c.doAgent(ctx, "GET", "/agent-bridge/feed", nil, nil, &out)
}

func (c *Client) AgentSessions(ctx context.Context) ([]AgentSession, error) {
	var out struct {
		Sessions []AgentSession `json:"sessions"`
	}
	err := c.doAgent(ctx, "GET", "/agent-sessions", nil, nil, &out)
	return out.Sessions, err
}

func (c *Client) AgentSessionSearch(ctx context.Context, q string) ([]AgentSearchHit, error) {
	var out struct {
		Items []AgentSearchHit `json:"items"`
	}
	err := c.doAgent(ctx, "GET", "/agent-sessions/search", url.Values{"q": {q}}, nil, &out)
	return out.Items, err
}

// AgentMessages pages a session's messages newest-first by page (each page ordered oldest→newest); cursor "" is the latest.
func (c *Client) AgentMessages(ctx context.Context, key, cursor string, limit int) (*AgentMessagePage, error) {
	q := url.Values{}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var out AgentMessagePage
	if err := c.doAgent(ctx, "GET", agentSessionPath(key)+"/messages", q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AgentSend posts text into a session as the user. clientId makes it idempotent: lark-connector writes its outbox row
// keyed by it first, so a retry with the same clientId never sends twice. Needs the full (not read-only) token.
func (c *Client) AgentSend(ctx context.Context, key, clientID, text string) (json.RawMessage, error) {
	if clientID == "" {
		return nil, errors.New("lark: clientId is required")
	}
	var out json.RawMessage
	err := c.doAgent(ctx, "POST", agentSessionPath(key)+"/messages", nil, map[string]any{"clientId": clientID, "text": text}, &out)
	return out, err
}

func (c *Client) AgentDeleteMessage(ctx context.Context, key, clientID string) error {
	return c.doAgent(ctx, "DELETE", agentSessionPath(key)+"/messages/"+url.PathEscape(clientID), nil, nil, nil)
}

func (c *Client) AgentMarkRead(ctx context.Context, key string) error {
	return c.doAgent(ctx, "POST", agentSessionPath(key)+"/read", nil, map[string]any{}, nil)
}

// doAgent is do with the HTTP status kept in an *APIError (401s still go through unauthorized).
func (c *Client) doAgent(ctx context.Context, method, path string, query url.Values, body, out any) error {
	tok, err := c.agentToken(ctx)
	if err != nil {
		return err
	}
	u := strings.TrimRight(c.cfg.BaseURL, "/") + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	c.authorize(req, tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("lark: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode/100 != 2 {
		if resp.StatusCode == 401 {
			return c.unauthorized(raw)
		}
		code, msg := upstreamError(raw)
		return &APIError{Status: resp.StatusCode, Code: code, Message: msg}
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}
