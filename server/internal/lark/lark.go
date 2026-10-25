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

	mu          sync.Mutex
	token       string
	emojis      map[string]Emoji
	emojiGroups []EmojiGroup
	emojisAt    time.Time
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
// Groups are the picker's sections (默认表情, 字节范…) with their keys in order.
func (c *Client) Emojis(ctx context.Context) (map[string]Emoji, []EmojiGroup, error) {
	c.mu.Lock()
	if c.emojis != nil && time.Since(c.emojisAt) < 24*time.Hour {
		e, g := c.emojis, c.emojiGroups
		c.mu.Unlock()
		return e, g, nil
	}
	c.mu.Unlock()
	var res struct {
		Groups []EmojiGroup `json:"groups"`
		Items  []struct {
			Key      string   `json:"key"`
			Label    string   `json:"label"`
			ImageURL string   `json:"imageUrl"`
			SkinKeys []string `json:"skinKeys"`
		} `json:"items"`
	}
	if err := c.do(ctx, "GET", "/resources/emojis", nil, nil, &res); err != nil {
		return nil, nil, err
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
	c.emojis, c.emojiGroups, c.emojisAt = out, res.Groups, time.Now()
	c.mu.Unlock()
	return out, res.Groups, nil
}

type Emoji struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type EmojiGroup struct {
	Title string   `json:"title"`
	Keys  []string `json:"keys"`
}

// raw calls lark-connector and hands its JSON back with media URLs pointing at the hub (profiles, previews, posts).
func (c *Client) raw(ctx context.Context, method, path string, query url.Values, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, method, path, query, body, &out); err != nil {
		return nil, err
	}
	return rewriteMedia(out), nil
}

// Profile is a person's profile card (lark-connector UserProfileDetail); feedID adds the group nickname.
func (c *Client) Profile(ctx context.Context, userID, feedID string) (json.RawMessage, error) {
	q := url.Values{}
	if feedID != "" {
		q.Set("feedId", feedID)
	}
	return c.raw(ctx, "GET", "/users/"+url.PathEscape(userID), q, nil)
}

// DirectChat returns the feed id of the 1:1 chat with a user, creating it if needed.
func (c *Client) DirectChat(ctx context.Context, userID string) (string, error) {
	var res struct {
		FeedID string `json:"feedId"`
	}
	if err := c.do(ctx, "POST", "/conversations", nil, map[string]any{"type": "p2p", "userIds": []string{userID}}, &res); err != nil {
		return "", err
	}
	if res.FeedID == "" {
		return "", errors.New("lark: no chat with this user")
	}
	return res.FeedID, nil
}

// React adds (or with remove, takes back) the user's reaction on a message.
func (c *Client) React(ctx context.Context, messageID, key string, remove bool) error {
	if remove {
		return c.do(ctx, "DELETE", "/messages/"+url.PathEscape(messageID)+"/reactions/"+url.PathEscape(key), nil, nil, nil)
	}
	return c.do(ctx, "POST", "/messages/"+url.PathEscape(messageID)+"/reactions", nil, map[string]any{"key": key}, nil)
}

// QuickReactions are the reaction keys the user reaches for (Feishu's quick bar), most used first.
func (c *Client) QuickReactions(ctx context.Context) ([]string, error) {
	var res struct {
		Quick  []string `json:"quick"`
		Recent []string `json:"recent"`
	}
	if err := c.do(ctx, "GET", "/reactions/recent", nil, nil, &res); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, k := range append(append(res.Recent, res.Quick...), "THUMBSUP", "HEART", "SMILE", "LAUGH", "SURPRISED", "CRY", "OK", "DONE") {
		if !seen[strings.ToLower(k)] {
			seen[strings.ToLower(k)] = true
			out = append(out, k)
		}
	}
	return out, nil
}

// URLPreview is the card Feishu shows under a link (removed posts, docs…).
func (c *Client) URLPreview(ctx context.Context, messageID, link string) (json.RawMessage, error) {
	return c.raw(ctx, "GET", "/message-url-preview", url.Values{"messageId": {messageID}, "url": {link}}, nil)
}

// MomentsPost is one removed post: author, text, pictures, counts.
func (c *Client) MomentsPost(ctx context.Context, postID string) (json.RawMessage, error) {
	return c.raw(ctx, "GET", "/moments/posts/"+url.PathEscape(postID), nil, nil)
}

// MomentsComments are a removed post's comments.
func (c *Client) MomentsComments(ctx context.Context, postID string) (json.RawMessage, error) {
	return c.raw(ctx, "GET", "/moments/posts/"+url.PathEscape(postID)+"/comments", nil, nil)
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

// rawRewriter points every lark-connector /api/... path in a JSON body at the hub's read-only proxy.
var rawRewriter = strings.NewReplacer(`"/api/media/`, `"/api/v1/lark/media/`, `"/api/messages/`, `"/api/v1/lark/messages/`, `"/api/`, `"/api/v1/lark/raw/`)

// Read-only POST routes (lark-connector's READ_ONLY_POST_ROUTES subset the app needs): they only POST because the body is large.
var rawPostAllowed = map[string]bool{"/search/page": true, "/search/mention-history": true}

// GETs that act as the user or hand out credentials (lark-connector's NOT_READ_ONLY_GET_ROUTES).
func rawGetDenied(path string) bool {
	return strings.HasSuffix(path, "/qr-code") || strings.HasPrefix(path, "/links/open") || strings.HasPrefix(path, "/maintenance/")
}

// Raw forwards one read-only request to lark-connector's agent API (path below /agent/v1) and writes the answer:
// JSON with its media / API paths pointing at the hub, anything else streamed as is.
func (c *Client) Raw(ctx context.Context, w http.ResponseWriter, method, path string, query url.Values, body io.Reader) error {
	if !strings.HasPrefix(path, "/") || strings.Contains(path, "..") {
		return errors.New("lark: bad path")
	}
	switch method {
	case "GET":
		if rawGetDenied(path) {
			return errors.New("lark: " + path + " is not read-only")
		}
	case "POST":
		if !rawPostAllowed[path] {
			return errors.New("lark: POST " + path + " is not allowed")
		}
	default:
		return errors.New("lark: only reads are allowed")
	}
	tok, err := c.agentToken(ctx)
	if err != nil {
		return err
	}
	u := strings.TrimRight(c.cfg.BaseURL, "/") + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.media.Do(req)
	if err != nil {
		return fmt.Errorf("lark: %w", err)
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "application/json") {
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		if err != nil {
			return err
		}
		if resp.StatusCode == 401 {
			c.mu.Lock()
			c.token = ""
			c.mu.Unlock()
		}
		w.Header().Set("Content-Type", ct)
		w.WriteHeader(resp.StatusCode)
		_, err = w.Write([]byte(rawRewriter.Replace(string(raw))))
		return err
	}
	for _, h := range []string{"Content-Type", "Content-Length", "Content-Disposition", "Cache-Control", "ETag"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, err = io.Copy(w, resp.Body)
	return err
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
