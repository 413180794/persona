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

	media *http.Client // no overall timeout: videos stream for minutes

	mu       sync.Mutex
	token    string
	emojis   map[string]Emoji
	emojisAt time.Time
}

func New(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://127.0.0.1:8095/agent/v1"
	}
	if cfg.Container == "" {
		cfg.Container = "lark-connector-yunpeng"
	}
	return &Client{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}, media: &http.Client{}, token: cfg.Token}
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

type Message struct {
	ID               string          `json:"id"`
	Position         string          `json:"position"`
	SenderID         string          `json:"sender_id,omitempty"`
	SenderName       string          `json:"sender_name"`
	SenderAvatar     string          `json:"sender_avatar,omitempty"`
	SenderType       string          `json:"sender_type,omitempty"` // user, bot…
	IsMine           bool            `json:"is_mine"`
	Type             string          `json:"type"` // TEXT, POST, IMAGE, CARD, SYSTEM, FILE…
	Text             string          `json:"text"`
	CreatedAt        time.Time       `json:"created_at"`
	Recalled         bool            `json:"recalled"`
	Edited           bool            `json:"edited,omitempty"`
	ReplyCount       int             `json:"reply_count"`
	ThreadReplyCount int             `json:"thread_reply_count,omitempty"`
	BadgeCount       *int            `json:"badge_count,omitempty"` // for read reports
	AtMe             bool            `json:"at_me,omitempty"`       // mentions me or @all
	IsRead           bool            `json:"is_read,omitempty"`
	Parts            json.RawMessage `json:"parts,omitempty"` // lark-connector MessagePart[], media URLs rewritten to the hub
	ReplyTo          *ReplyTo        `json:"reply_to,omitempty"`
	Reactions        []Reaction      `json:"reactions,omitempty"`
}

// ReplyTo is the message a reply quotes.
type ReplyTo struct {
	ID         string          `json:"id"`
	SenderName string          `json:"sender_name"`
	Text       string          `json:"text"`
	Recalled   bool            `json:"recalled,omitempty"`
	Parts      json.RawMessage `json:"parts,omitempty"`
}

// Reaction is one emoji under a message; Key resolves through Emojis.
type Reaction struct {
	Key   string   `json:"key"`
	Count int      `json:"count"`
	Mine  bool     `json:"mine,omitempty"`
	Users []string `json:"users,omitempty"`
}

type rawMessage struct {
	ID               string          `json:"id"`
	Position         string          `json:"position"`
	SenderID         string          `json:"senderId"`
	SenderName       string          `json:"senderName"`
	SenderAvatarURL  string          `json:"senderAvatarUrl"`
	SenderType       string          `json:"senderType"`
	IsMine           bool            `json:"isMine"`
	MessageTypeName  string          `json:"messageTypeName"`
	TextPreview      string          `json:"textPreview"`
	CreatedAt        time.Time       `json:"createdAt"`
	IsRecalled       bool            `json:"isRecalled"`
	IsDeleted        bool            `json:"isDeleted"`
	IsEdited         bool            `json:"isEdited"`
	ReplyCount       int             `json:"replyCount"`
	ThreadReplyCount int             `json:"threadReplyCount"`
	BadgeCount       *int            `json:"badgeCount"`
	IsAtMe           bool            `json:"isAtMe"`
	IsAtAll          bool            `json:"isAtAll"`
	IsRead           bool            `json:"isRead"`
	Parts            json.RawMessage `json:"parts"`
	ReplyTo          *struct {
		ID          string          `json:"id"`
		SenderName  string          `json:"senderName"`
		TextPreview string          `json:"textPreview"`
		IsRecalled  bool            `json:"isRecalled"`
		Parts       json.RawMessage `json:"parts"`
	} `json:"replyTo"`
	Reactions []struct {
		Key       string   `json:"key"`
		Count     int      `json:"count"`
		IsMine    bool     `json:"isMine"`
		UserNames []string `json:"userNames"`
	} `json:"reactions"`
}

func (m rawMessage) normalize() Message {
	out := Message{ID: m.ID, Position: m.Position, SenderID: m.SenderID, SenderName: m.SenderName,
		SenderAvatar: MediaURL(m.SenderAvatarURL), SenderType: m.SenderType, IsMine: m.IsMine,
		Type: m.MessageTypeName, Text: m.TextPreview, CreatedAt: m.CreatedAt, Recalled: m.IsRecalled, Edited: m.IsEdited,
		ReplyCount: m.ReplyCount, ThreadReplyCount: m.ThreadReplyCount, BadgeCount: m.BadgeCount, AtMe: m.IsAtMe || m.IsAtAll,
		IsRead: m.IsRead, Parts: rewriteMedia(m.Parts)}
	if r := m.ReplyTo; r != nil {
		out.ReplyTo = &ReplyTo{ID: r.ID, SenderName: r.SenderName, Text: r.TextPreview, Recalled: r.IsRecalled, Parts: rewriteMedia(r.Parts)}
	}
	for _, r := range m.Reactions {
		if r.Count > 0 {
			out.Reactions = append(out.Reactions, Reaction{Key: r.Key, Count: r.Count, Mine: r.IsMine, Users: r.UserNames})
		}
	}
	return out
}

// lark-connector serves media at /api/media/<key> and /api/messages/<id>/media/<key>; the hub proxies both under /api/v1/lark.
var mediaRewriter = strings.NewReplacer(`"/api/media/`, `"/api/v1/lark/media/`, `"/api/messages/`, `"/api/v1/lark/messages/`)

func rewriteMedia(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.RawMessage(mediaRewriter.Replace(string(raw)))
}

// MediaURL maps one lark-connector media URL to the hub's proxy; other URLs pass through.
func MediaURL(u string) string {
	if s, ok := strings.CutPrefix(u, "/api/media/"); ok {
		return "/api/v1/lark/media/" + s
	}
	if s, ok := strings.CutPrefix(u, "/api/messages/"); ok {
		return "/api/v1/lark/messages/" + s
	}
	return u
}

// Window picks which page of a chat to read. Zero value: the latest page.
type Window struct {
	Before string // older than this position
	After  string // newer than this position
	Around string // centred on this position (opening a chat at its first unread message)
}

// Page is a slice of a chat, oldest first, with the cursors to read further either way.
type Page struct {
	Messages    []Message `json:"messages"`
	HasOlder    bool      `json:"has_older"`
	HasNewer    bool      `json:"has_newer"`
	OlderCursor string    `json:"older_cursor,omitempty"`
	NewerCursor string    `json:"newer_cursor,omitempty"`
}

// Messages returns a page of a chat. In a topic group (`topic`) the page is the thread roots, each with its
// reply count; topic pages only page backwards.
func (c *Client) Messages(ctx context.Context, feedID string, w Window, limit int, topic bool) (Page, error) {
	if topic {
		return c.page(ctx, "/conversations/"+url.PathEscape(feedID)+"/threads", Window{Before: w.Before}, limit)
	}
	return c.page(ctx, "/conversations/"+url.PathEscape(feedID)+"/messages", w, limit)
}

// Thread returns the replies in one message's thread, oldest first.
func (c *Client) Thread(ctx context.Context, messageID, before string, limit int) (Page, error) {
	return c.page(ctx, "/messages/"+url.PathEscape(messageID)+"/thread", Window{Before: before}, limit)
}

func (c *Client) page(ctx context.Context, path string, w Window, limit int) (Page, error) {
	if limit <= 0 || limit > 100 {
		limit = 40
	}
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	switch {
	case w.Before != "":
		q.Set("beforePosition", w.Before)
	case w.After != "":
		q.Set("afterPosition", w.After)
	case w.Around != "":
		q.Set("aroundPosition", w.Around)
	default:
		// Without tail lark-connector opens an unread chat at its read position; the app asks for that explicitly.
		q.Set("tail", "1")
	}
	var res struct {
		Items           []rawMessage `json:"items"`
		HasOlder        bool         `json:"hasOlder"`
		HasNewer        bool         `json:"hasNewer"`
		NextOlderCursor string       `json:"nextOlderCursor"`
		NextNewerCursor string       `json:"nextNewerCursor"`
	}
	if err := c.do(ctx, "GET", path, q, nil, &res); err != nil {
		return Page{}, err
	}
	p := Page{Messages: make([]Message, 0, len(res.Items)), HasOlder: res.HasOlder, HasNewer: res.HasNewer,
		OlderCursor: res.NextOlderCursor, NewerCursor: res.NextNewerCursor}
	for _, m := range res.Items {
		if m.IsDeleted {
			continue
		}
		p.Messages = append(p.Messages, m.normalize())
	}
	if p.OlderCursor == "" && len(res.Items) > 0 {
		p.OlderCursor = res.Items[0].Position
	}
	if p.NewerCursor == "" && len(res.Items) > 0 {
		p.NewerCursor = res.Items[len(res.Items)-1].Position
	}
	return p, nil
}

// Emojis maps every emoji key (lower-cased, skin-tone variants included) to its hub image URL.
// The catalogue rarely changes, so it is cached for a day.
func (c *Client) Emojis(ctx context.Context) (map[string]Emoji, error) {
	c.mu.Lock()
	if c.emojis != nil && time.Since(c.emojisAt) < 24*time.Hour {
		e := c.emojis
		c.mu.Unlock()
		return e, nil
	}
	c.mu.Unlock()
	var res struct {
		Items []struct {
			Key      string   `json:"key"`
			Label    string   `json:"label"`
			ImageURL string   `json:"imageUrl"`
			SkinKeys []string `json:"skinKeys"`
		} `json:"items"`
	}
	if err := c.do(ctx, "GET", "/resources/emojis", nil, nil, &res); err != nil {
		return nil, err
	}
	out := make(map[string]Emoji, len(res.Items)*2)
	for _, it := range res.Items {
		if it.ImageURL == "" {
			continue
		}
		e := Emoji{Label: it.Label, URL: MediaURL(it.ImageURL)}
		for _, k := range it.SkinKeys {
			if _, ok := out[strings.ToLower(k)]; !ok {
				out[strings.ToLower(k)] = e
			}
		}
		out[strings.ToLower(it.Key)] = e
	}
	c.mu.Lock()
	c.emojis, c.emojisAt = out, time.Now()
	c.mu.Unlock()
	return out, nil
}

type Emoji struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// Media streams one media resource (path below /agent/v1, e.g. "/media/<key>") to w, keeping its type and caching.
func (c *Client) Media(ctx context.Context, w http.ResponseWriter, path string, query url.Values) error {
	tok, err := c.agentToken(ctx)
	if err != nil {
		return err
	}
	u := strings.TrimRight(c.cfg.BaseURL, "/") + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := c.media.Do(req)
	if err != nil {
		return fmt.Errorf("lark: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		if resp.StatusCode == 401 {
			c.mu.Lock()
			c.token = ""
			c.mu.Unlock()
		}
		return fmt.Errorf("lark: media HTTP %d", resp.StatusCode)
	}
	for _, h := range []string{"Content-Type", "Content-Length", "Cache-Control", "ETag"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

// CardAction presses a callback button (or submits a control) on a card message as the user.
func (c *Client) CardAction(ctx context.Context, messageID, actionID string, params json.RawMessage) error {
	var body any = map[string]any{}
	if len(params) > 0 && string(params) != "null" {
		body = params
	}
	return c.do(ctx, "POST", "/messages/"+url.PathEscape(messageID)+"/card-actions/"+url.PathEscape(actionID), nil, body, nil)
}

func (c *Client) Send(ctx context.Context, feedID, text string) error {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	clientID := fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
	return c.do(ctx, "POST", "/conversations/"+url.PathEscape(feedID)+"/messages", nil,
		map[string]any{"text": text, "clientId": clientID}, nil)
}

// Read is what the reader has actually seen: everything up to Position, plus the unread messages from
// others among them (lark-connector's read report).
type Read struct {
	Position   string   `json:"position"`
	BadgeCount *int     `json:"badge_count,omitempty"`
	MessageIDs []string `json:"message_ids,omitempty"`
}

// MarkRead advances the chat's read position.
func (c *Client) MarkRead(ctx context.Context, feedID string, r Read) error {
	body := map[string]any{"maxPosition": r.Position}
	if r.BadgeCount != nil {
		body["maxPositionBadgeCount"] = *r.BadgeCount
	}
	if len(r.MessageIDs) > 0 {
		body["messageIds"] = r.MessageIDs
	}
	return c.do(ctx, "POST", "/conversations/"+url.PathEscape(feedID)+"/read-position", nil, body, nil)
}
