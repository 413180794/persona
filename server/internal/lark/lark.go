// Package lark reads and sends Feishu / Lark messages through the user's lark-connector instance
// (github.com/Eric-2369/lark-connector) on example-machine, via its local agent API (/agent/v1, loopback only).
// The agent token is read from the lark-connector container's environment at runtime; Jarvis never stores a copy.
package lark

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Config struct {
	BaseURL   string `json:"base_url"`  // default http://127.0.0.1:8095/agent/v1
	Container string `json:"container"` // docker container to read lark-connector_AGENT_TOKEN from (default lark-connector-yunpeng)
	Token     string `json:"token,omitempty"`
}

type Client struct {
	cfg  Config
	http *http.Client

	mu    sync.Mutex
	token string
}

func New(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://127.0.0.1:8095/agent/v1"
	}
	if cfg.Container == "" {
		cfg.Container = "lark-connector-yunpeng"
	}
	return &Client{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}, token: cfg.Token}
}

func (c *Client) agentToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" {
		return c.token, nil
	}
	out, err := exec.CommandContext(ctx, "docker", "inspect", c.cfg.Container, "--format", "{{range .Config.Env}}{{println .}}{{end}}").Output()
	if err != nil {
		return "", fmt.Errorf("lark: read lark-connector token from container %s: %w", c.cfg.Container, err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(line, "lark-connector_AGENT_TOKEN="); ok && v != "" {
			c.token = strings.TrimSpace(v)
			return c.token, nil
		}
	}
	return "", errors.New("lark: lark-connector agent API is not enabled (lark-connector_AGENT_TOKEN unset)")
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
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
	req.Header.Set("Authorization", "Bearer "+tok)
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
		var e struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Error.Message != "" {
			if resp.StatusCode == 401 {
				c.mu.Lock()
				c.token = "" // the container may have been recreated with a new token
				c.mu.Unlock()
			}
			return fmt.Errorf("lark: %s (%s)", e.Error.Message, e.Error.Code)
		}
		return fmt.Errorf("lark: HTTP %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// Conversation is a chat or group as the app lists it.
type Conversation struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	IsGroup   bool      `json:"is_group"`
	Unread    int       `json:"unread"`
	AtMe      bool      `json:"at_me"`
	Muted     bool      `json:"muted"`
	Preview   string    `json:"preview"`
	IsMine    bool      `json:"is_mine"`
	UpdatedAt time.Time `json:"updated_at"`
}

type rawConversation struct {
	FeedID        string `json:"feedId"`
	Name          string `json:"name"`
	IsGroup       bool   `json:"isGroup"`
	UnreadCount   int    `json:"unreadCount"`
	HasUnreadAtMe bool   `json:"hasUnreadAtMe"`
	IsMuted       bool   `json:"isMuted"`
	IsHidden      bool   `json:"isHidden"`
	UpdatedAtMs   any    `json:"updatedAtMs"`
	Preview       *struct {
		DisplayAt string `json:"displayAt"`
		IsMine    bool   `json:"isMine"`
		Digest    struct {
			Elements []struct {
				Content string `json:"content"`
				Kind    string `json:"kind"`
			} `json:"elements"`
		} `json:"digest"`
	} `json:"preview"`
}

func (r rawConversation) normalize() Conversation {
	c := Conversation{ID: r.FeedID, Name: r.Name, IsGroup: r.IsGroup, Unread: r.UnreadCount, AtMe: r.HasUnreadAtMe, Muted: r.IsMuted}
	if r.Preview != nil {
		var b strings.Builder
		for _, e := range r.Preview.Digest.Elements {
			b.WriteString(e.Content)
		}
		c.Preview = strings.Join(strings.Fields(b.String()), " ")
		c.IsMine = r.Preview.IsMine
		if t, err := time.Parse(time.RFC3339Nano, r.Preview.DisplayAt); err == nil {
			c.UpdatedAt = t
		}
	}
	if c.UpdatedAt.IsZero() {
		var ms int64
		switch v := r.UpdatedAtMs.(type) {
		case float64:
			ms = int64(v)
		case string:
			ms, _ = strconv.ParseInt(v, 10, 64)
		}
		if ms > 0 {
			c.UpdatedAt = time.UnixMilli(ms).UTC()
		}
	}
	return c
}

// Conversations returns the most recent chats (lark-connector's feed order), skipping hidden ones.
func (c *Client) Conversations(ctx context.Context, cursor string, limit int) ([]Conversation, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	var res struct {
		Items      []rawConversation `json:"items"`
		NextCursor string            `json:"nextCursor"`
		HasMore    bool              `json:"hasMore"`
	}
	if err := c.do(ctx, "GET", "/conversations", q, nil, &res); err != nil {
		return nil, "", err
	}
	out := make([]Conversation, 0, len(res.Items))
	for _, r := range res.Items {
		if r.IsHidden {
			continue
		}
		out = append(out, r.normalize())
	}
	next := ""
	if res.HasMore {
		next = res.NextCursor
	}
	return out, next, nil
}

type Message struct {
	ID         string    `json:"id"`
	Position   string    `json:"position"`
	SenderName string    `json:"sender_name"`
	IsMine     bool      `json:"is_mine"`
	Type       string    `json:"type"` // TEXT, POST, IMAGE, CARD, SYSTEM, FILE…
	Text       string    `json:"text"`
	CreatedAt  time.Time `json:"created_at"`
	Recalled   bool      `json:"recalled"`
	ReplyCount int       `json:"reply_count"`
}

// Messages returns a page of messages, oldest first: the latest page, or the one before position `before`.
func (c *Client) Messages(ctx context.Context, feedID, before string, limit int) ([]Message, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 40
	}
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	if before != "" {
		q.Set("beforePosition", before)
	} else {
		q.Set("tail", "1")
	}
	var res struct {
		Items []struct {
			ID              string    `json:"id"`
			Position        string    `json:"position"`
			SenderName      string    `json:"senderName"`
			IsMine          bool      `json:"isMine"`
			MessageTypeName string    `json:"messageTypeName"`
			TextPreview     string    `json:"textPreview"`
			CreatedAt       time.Time `json:"createdAt"`
			IsRecalled      bool      `json:"isRecalled"`
			IsDeleted       bool      `json:"isDeleted"`
			ReplyCount      int       `json:"replyCount"`
		} `json:"items"`
		HasOlder bool `json:"hasOlder"`
	}
	if err := c.do(ctx, "GET", "/conversations/"+url.PathEscape(feedID)+"/messages", q, nil, &res); err != nil {
		return nil, false, err
	}
	out := make([]Message, 0, len(res.Items))
	for _, m := range res.Items {
		if m.IsDeleted {
			continue
		}
		out = append(out, Message{ID: m.ID, Position: m.Position, SenderName: m.SenderName, IsMine: m.IsMine,
			Type: m.MessageTypeName, Text: m.TextPreview, CreatedAt: m.CreatedAt, Recalled: m.IsRecalled, ReplyCount: m.ReplyCount})
	}
	return out, res.HasOlder, nil
}

func (c *Client) Send(ctx context.Context, feedID, text string) error {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	clientID := fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
	return c.do(ctx, "POST", "/conversations/"+url.PathEscape(feedID)+"/messages", nil,
		map[string]any{"text": text, "clientId": clientID}, nil)
}

// MarkRead marks everything up to position as read.
func (c *Client) MarkRead(ctx context.Context, feedID, position string) error {
	return c.do(ctx, "POST", "/conversations/"+url.PathEscape(feedID)+"/read-position", nil,
		map[string]any{"maxPosition": position}, nil)
}
