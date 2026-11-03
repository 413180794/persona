// Package lark reads and sends Feishu / Lark messages through the user's lark-connector instance
// (github.com/Eric-2369/lark-connector) on the hub machine, via its local agent API (/agent/v1, loopback only).
// The agent token comes from hub.json (lark.token), or is read from the configured lark-connector container's
// environment at runtime (lark.container); in that case Persona never stores a copy.
package lark

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
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
	Account   int    `json:"account,omitempty"` // lark-connector account ID; zero selects primary account 1
	BaseURL   string `json:"base_url"`          // default http://127.0.0.1:8095/agent/v1
	Container string `json:"container"`         // docker container to read lark-connector_AGENT_TOKEN from when Token is empty (no default)
	Token     string `json:"token,omitempty"`   // agent token; kept for the client's lifetime (never dropped on a 401)

	// CLI is Feishu's official open-platform CLI (lark-cli) as a second source beside lark-connector (docs/plan-lark-cli.md);
	// nil: not used.
	CLI *CLIConfig `json:"cli,omitempty"`
	// Prefer is the primary source when both are configured: "lark-connector" (default) or "cli".
	Prefer string `json:"prefer,omitempty"`
}

// CLIConfig is how the hub and the assistant run lark-cli. Its app and the user's login stay in lark-cli's own store.
type CLIConfig struct {
	Bin     string `json:"bin,omitempty"`     // command name or absolute path; default lark-cli, looked up in a login shell
	Profile string `json:"profile,omitempty"` // lark-cli --profile; "" is its default profile
}

// lark-connectorConfigured: lark-connector has a token, or a container to read one from.
func (cfg Config) lark-connectorConfigured() bool { return cfg.Token != "" || cfg.Container != "" }

func (cfg Config) Validate() error {
	if cfg.Account < 0 || cfg.Account > 999999999 {
		return errors.New("lark.account must be an integer between 1 and 999999999 (or 0 for the primary account)")
	}
	switch cfg.Prefer {
	case "", "lark-connector", "cli":
	default:
		return errors.New(`lark.prefer must be "lark-connector" or "cli"`)
	}
	return nil
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
	topics      map[string]bool // feed id → topic group or not, from the feed's chatMode or GET /conversations/FEED_ID
}

func New(cfg Config) *Client {
	if cfg.Account == 0 {
		cfg.Account = 1
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://127.0.0.1:8095/agent/v1"
	}
	return &Client{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}, media: &http.Client{}, token: cfg.Token}
}

// Account is the default account for reads, writes, media and realtime events.
func (c *Client) Account() int { return c.cfg.Account }

func (c *Client) authorize(req *http.Request, token string) {
	req.Header.Set("Authorization", "Bearer "+token)
	// Preserve explicit account selection on raw requests, including upstream validation.
	if !req.URL.Query().Has("ol_account") {
		req.Header.Set("x-lark-connector-account", strconv.Itoa(c.Account()))
	}
}

// AgentAccess is the agent API's base (without /agent/v1) and token, for the assistant's own lark-connector helper.
func (c *Client) AgentAccess(ctx context.Context) (base, token string, err error) {
	token, err = c.agentToken(ctx)
	return strings.TrimSuffix(strings.TrimSuffix(c.cfg.BaseURL, "/"), "/agent/v1"), token, err
}

func (c *Client) agentToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" {
		return c.token, nil
	}
	if c.cfg.Container == "" {
		return "", errors.New("lark: lark-connector is not configured (set lark.token or lark.container in hub.json)")
	}
	out, err := containerEnv(ctx, c.cfg.Container)
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

// containerEnv prints a docker container's environment, one VAR=value per line (a variable so tests can stub docker).
var containerEnv = func(ctx context.Context, container string) ([]byte, error) {
	return exec.CommandContext(ctx, "docker", "inspect", container, "--format", "{{range .Config.Env}}{{println .}}{{end}}").Output()
}

// ErrNotLoggedIn is lark-connector answering that it has no live Feishu session (AUTH_SESSION_EXPIRED and the other
// AUTH_* codes, sent with HTTP 401). The agent token is fine; the user has to log in to Feishu in lark-connector again.
var ErrNotLoggedIn = errors.New("lark-connector 未登录飞书")

// upstreamError reads an lark-connector error body ({"error":{"code","message"}}).
func upstreamError(raw []byte) (code, message string) {
	var e struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &e)
	return e.Error.Code, e.Error.Message
}

// doError is a non-2xx answer (other than 401) to c.do, keeping the status and lark-connector's error code.
type doError struct {
	Status  int
	Code    string
	Message string
}

func (e *doError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("lark: %s (%s)", e.Message, e.Code)
	}
	return fmt.Sprintf("lark: HTTP %d", e.Status)
}

// unauthorized handles a 401 from lark-connector. An AUTH_* code means lark-connector itself is logged out of Feishu: the
// token stays and the error says so. Anything else is a rejected agent token: forget a token read from the
// container (it may have been recreated with a new one), but never the one from config.
func (c *Client) unauthorized(raw []byte) error {
	code, msg := upstreamError(raw)
	if strings.HasPrefix(code, "AUTH_") {
		if msg != "" {
			return fmt.Errorf("lark: %w: %s (%s)", ErrNotLoggedIn, msg, code)
		}
		return fmt.Errorf("lark: %w (%s)", ErrNotLoggedIn, code)
	}
	c.mu.Lock()
	c.token = c.cfg.Token
	c.mu.Unlock()
	if msg != "" {
		return fmt.Errorf("lark: agent token rejected: %s (%s)", msg, code)
	}
	return errors.New("lark: agent token rejected (HTTP 401)")
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
		return &doError{Status: resp.StatusCode, Code: code, Message: msg}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

type Message struct {
	ID               string          `json:"id"`
	ClientID         string          `json:"client_id,omitempty"`
	LocalState       string          `json:"local_state,omitempty"`
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
	// ThreadID is the thread (topic) the message is in; ThreadKind is "root", "reply" or "" (a plain message).
	ThreadID   string     `json:"thread_id,omitempty"`
	ThreadKind string     `json:"thread_kind,omitempty"`
	Reactions  []Reaction `json:"reactions,omitempty"`
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
	ID                string          `json:"id"`
	ClientID          string          `json:"clientId"`
	LocalState        string          `json:"localState"`
	Position          string          `json:"position"`
	SenderID          string          `json:"senderId"`
	SenderName        string          `json:"senderName"`
	SenderAvatarURL   string          `json:"senderAvatarUrl"`
	SenderType        string          `json:"senderType"`
	IsMine            bool            `json:"isMine"`
	MessageTypeName   string          `json:"messageTypeName"`
	TextPreview       string          `json:"textPreview"`
	CreatedAt         time.Time       `json:"createdAt"`
	IsRecalled        bool            `json:"isRecalled"`
	IsDeleted         bool            `json:"isDeleted"`
	IsEdited          bool            `json:"isEdited"`
	ReplyCount        int             `json:"replyCount"`
	ThreadReplyCount  int             `json:"threadReplyCount"`
	BadgeCount        *int            `json:"badgeCount"`
	IsAtMe            bool            `json:"isAtMe"`
	IsAtAll           bool            `json:"isAtAll"`
	IsRead            bool            `json:"isRead"`
	Parts             json.RawMessage `json:"parts"`
	ThreadID          string          `json:"threadId"`
	ThreadMessageType string          `json:"threadMessageType"`
	ReplyTo           *struct {
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
		ClientID: m.ClientID, LocalState: m.LocalState,
		SenderAvatar: MediaURL(m.SenderAvatarURL), SenderType: m.SenderType, IsMine: m.IsMine,
		Type: m.MessageTypeName, Text: m.TextPreview, CreatedAt: m.CreatedAt, Recalled: m.IsRecalled, Edited: m.IsEdited,
		ReplyCount: m.ReplyCount, ThreadReplyCount: m.ThreadReplyCount, BadgeCount: m.BadgeCount, AtMe: m.IsAtMe || m.IsAtAll,
		IsRead: m.IsRead, Parts: rewriteMedia(m.Parts), ThreadID: m.ThreadID}
	if m.ThreadMessageType == "root" || m.ThreadMessageType == "reply" {
		out.ThreadKind = m.ThreadMessageType
	}
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
	SyncState   string    `json:"sync_state,omitempty"`
	HasOlder    bool      `json:"has_older"`
	HasNewer    bool      `json:"has_newer"`
	OlderCursor string    `json:"older_cursor,omitempty"`
	NewerCursor string    `json:"newer_cursor,omitempty"`
	// Topic: the chat is a topic group and the page is its threads' roots (open one with Thread).
	Topic bool `json:"topic,omitempty"`
}

// Messages returns a page of a chat. In a topic group (`topic`) the page is the thread roots, each with its
// reply count; topic pages only page backwards.
func (c *Client) Messages(ctx context.Context, feedID string, w Window, limit int, topic bool) (Page, error) {
	if topic {
		p, err := c.page(ctx, "/conversations/"+url.PathEscape(feedID)+"/threads", Window{Before: w.Before}, limit)
		p.Topic = err == nil
		return p, err
	}
	return c.page(ctx, "/conversations/"+url.PathEscape(feedID)+"/messages", w, limit)
}

// ChatPage is Messages for a caller that may not know the chat is a topic group (`topic` false): the client
// checks (IsTopic). A topic group's latest and older pages are its threads, as Messages gives them for topic; a
// page around a message still reads the messages endpoint, which some topic groups fill, and falls back to the
// threads when all it has is system notices (创建了话题群).
func (c *Client) ChatPage(ctx context.Context, feedID string, w Window, limit int, topic bool) (Page, error) {
	if topic || (w.Around == "" && w.After == "" && c.IsTopic(ctx, feedID)) {
		return c.Messages(ctx, feedID, w, limit, true)
	}
	p, err := c.Messages(ctx, feedID, w, limit, false)
	if err != nil || w.Around == "" || !onlyNotices(p.Messages) || !c.IsTopic(ctx, feedID) {
		return p, err
	}
	return c.Messages(ctx, feedID, Window{}, limit, true)
}

func onlyNotices(ms []Message) bool {
	for _, m := range ms {
		if m.Type != "SYSTEM" {
			return false
		}
	}
	return true
}

// IsTopic says whether a chat is a topic group, whose messages endpoint holds only the 创建了话题群 notice: the
// content is in its threads. The answer comes from the feed when the chat was listed, otherwise from one
// GET /conversations/FEED_ID, and is kept (a chat's mode does not change). A failed lookup answers false, uncached.
func (c *Client) IsTopic(ctx context.Context, feedID string) bool {
	c.mu.Lock()
	topic, ok := c.topics[feedID]
	c.mu.Unlock()
	if ok {
		return topic
	}
	var r rawConversation
	if err := c.do(ctx, "GET", "/conversations/"+url.PathEscape(feedID), nil, nil, &r); err != nil {
		return false
	}
	c.rememberMode(feedID, r.ChatMode)
	return r.ChatMode == "topic"
}

func (c *Client) rememberMode(feedID, chatMode string) {
	if feedID == "" || chatMode == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.topics == nil {
		c.topics = map[string]bool{}
	}
	c.topics[feedID] = chatMode == "topic"
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
		SyncState       string       `json:"syncState"`
	}
	if err := c.do(ctx, "GET", path, q, nil, &res); err != nil {
		return Page{}, err
	}
	p := Page{Messages: make([]Message, 0, len(res.Items)), HasOlder: res.HasOlder, HasNewer: res.HasNewer,
		OlderCursor: res.NextOlderCursor, NewerCursor: res.NextNewerCursor, SyncState: res.SyncState}
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
func (c *Client) Media(ctx context.Context, w http.ResponseWriter, path string, query url.Values, inbound http.Header) error {
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
	c.authorize(req, tok)
	copyRangeHeaders(req, inbound)
	resp, err := c.media.Do(req)
	if err != nil {
		return fmt.Errorf("lark: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		if resp.StatusCode == 401 {
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
			return c.unauthorized(raw)
		}
		return fmt.Errorf("lark: media HTTP %d", resp.StatusCode)
	}
	for _, h := range mediaHeaders {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, err = io.Copy(w, resp.Body)
	return err
}

// Headers a media answer keeps, so players can seek (206 + Content-Range) and caches can validate.
var mediaHeaders = []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Content-Disposition", "Cache-Control", "ETag", "Last-Modified"}

// copyRangeHeaders forwards a player's byte-range request (AVPlayer asks for small ranges and refuses a whole
// 100 MB file when it wanted the first two bytes).
func copyRangeHeaders(out *http.Request, in http.Header) {
	for _, h := range []string{"Range", "If-Range", "If-None-Match", "If-Modified-Since"} {
		if v := in.Get(h); v != "" {
			out.Header.Set(h, v)
		}
	}
}

// CardAction presses a callback button (or submits a control) on a card message as the user.
func (c *Client) CardAction(ctx context.Context, messageID, actionID string, params json.RawMessage) error {
	var body any = map[string]any{}
	if len(params) > 0 && string(params) != "null" {
		body = params
	}
	return c.do(ctx, "POST", "/messages/"+url.PathEscape(messageID)+"/card-actions/"+url.PathEscape(actionID), nil, body, nil)
}

// ReplyInThread lets lark-connector resolve the target message's thread or topic.
func (c *Client) ReplyInThread(ctx context.Context, messageID, text, clientID string) error {
	_, err := c.post(ctx, "/messages/"+url.PathEscape(messageID)+"/replies", map[string]any{"text": text, "replyMode": "thread"}, clientID)
	return err
}

// Mention is one person @-mentioned in a sent message; the text must carry "@"+Name literally. UserID "all" with
// Name "所有人" is @all.
type Mention struct {
	UserID string `json:"user_id"`
	Name   string `json:"name"`
}

// MaxMentions is how many mentions one message may carry (lark-connector's cap).
const MaxMentions = 100

// Send posts text to a conversation as the user and returns lark-connector's message id (empty if it gave none).
// clientID is the idempotency key: resending the same one never posts twice. Empty generates a fresh one.
// mentions turn the matching "@name" in text into real @-mentions.
func (c *Client) Send(ctx context.Context, feedID, text, clientID string, mentions ...Mention) (string, error) {
	return c.post(ctx, "/conversations/"+url.PathEscape(feedID)+"/messages", withMentions(map[string]any{"text": text}, mentions), clientID)
}

// Reply answers a message as the user, quoting it; `thread` posts into its thread (lark-connector finds the root; in a
// topic group it lands in the message's topic). `markdown` sends rich text (bold, lists, code, links;
// lark-connector ignores mentions then), else plain text. It returns lark-connector's message id; clientID and mentions work as in Send.
func (c *Client) Reply(ctx context.Context, messageID, text string, thread, markdown bool, clientID string, mentions ...Mention) (string, error) {
	body := withMentions(map[string]any{"text": text}, mentions)
	if thread {
		body["replyMode"] = "thread"
	}
	if markdown {
		body["editorType"] = "markdown"
	}
	return c.post(ctx, "/messages/"+url.PathEscape(messageID)+"/replies", body, clientID)
}

// withMentions adds lark-connector's mentions ([{userId, name}]) to a message body; none leaves the body as it was.
func withMentions(body map[string]any, mentions []Mention) map[string]any {
	if len(mentions) == 0 {
		return body
	}
	out := make([]map[string]string, len(mentions))
	for i, m := range mentions {
		out[i] = map[string]string{"userId": m.UserID, "name": m.Name}
	}
	body["mentions"] = out
	return body
}

// post sends a message body to path with clientID (generated if empty) and returns lark-connector's message id.
// 409 MESSAGE_ALREADY_SENT counts as sent; MESSAGE_DISCARDED stays an error.
func (c *Client) post(ctx context.Context, path string, body map[string]any, clientID string) (string, error) {
	if clientID == "" {
		clientID = NewClientID()
	}
	body["clientId"] = clientID
	var out struct {
		MessageID string `json:"messageId"`
	}
	err := c.do(ctx, "POST", path, nil, body, &out)
	var de *doError
	if errors.As(err, &de) && de.Status == http.StatusConflict && de.Code == "MESSAGE_ALREADY_SENT" {
		return "", nil
	}
	return out.MessageID, err
}

// NewClientID returns a random UUIDv4 for lark-connector's clientId.
func NewClientID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// clientIDNamespace is the UUIDv5 namespace of NamedClientID. Fixed forever: changing it would let a retry of an
// already delivered message post again.
var clientIDNamespace = [16]byte{0xa5, 0x1e, 0x3f, 0x21, 0xe1, 0xd7, 0x4c, 0xcd, 0x89, 0xbf, 0xcb, 0x02, 0x7f, 0x8c, 0xb8, 0xdc}

// NamedClientID is a UUIDv5 clientId derived from name: the same name always gives the same id, so the same
// message sent again (after a timeout, from another device, after a hub restart) posts once.
func NamedClientID(name string) string {
	h := sha1.New()
	h.Write(clientIDNamespace[:])
	h.Write([]byte(name))
	b := h.Sum(nil)[:16]
	b[6], b[8] = b[6]&0x0f|0x50, b[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ValidClientID reports whether s is a UUID lark-connector accepts as a clientId (version 1-8, RFC variant, any case).
func ValidClientID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if ch != '-' {
				return false
			}
		case !('0' <= ch && ch <= '9' || 'a' <= ch && ch <= 'f' || 'A' <= ch && ch <= 'F'):
			return false
		}
	}
	return '1' <= s[14] && s[14] <= '8' && strings.ContainsRune("89abAB", rune(s[19]))
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

func readOnly(method, path string) error {
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
	return nil
}

// Read makes one read-only call to lark-connector's agent API (the routes Raw allows) and returns the JSON answer
// (the assistant's lark_* tools).
func (c *Client) Read(ctx context.Context, method, path string, query url.Values, body any) (json.RawMessage, error) {
	if err := readOnly(method, path); err != nil {
		return nil, err
	}
	return c.raw(ctx, method, path, query, body)
}

// Raw forwards one read-only request to lark-connector's agent API (path below /agent/v1) and writes the answer:
// JSON with its media / API paths pointing at the hub, anything else streamed as is.
func (c *Client) Raw(ctx context.Context, w http.ResponseWriter, method, path string, query url.Values, body io.Reader, inbound http.Header) error {
	if err := readOnly(method, path); err != nil {
		return err
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
	c.authorize(req, tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	copyRangeHeaders(req, inbound)
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
			_ = c.unauthorized(raw) // the caller gets lark-connector's own answer below
		}
		w.Header().Set("Content-Type", ct)
		w.WriteHeader(resp.StatusCode)
		_, err = w.Write([]byte(rawRewriter.Replace(string(raw))))
		return err
	}
	for _, h := range mediaHeaders {
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

// MaxReadStates is how many messages one ReadStates call may ask about (lark-connector's own cap per batch).
const MaxReadStates = 100

// ReadState is a message's read receipt: how many members have read it and how many have not.
type ReadState struct {
	MessageID   string `json:"message_id"`
	ReadCount   int    `json:"read_count"`
	UnreadCount int    `json:"unread_count"`
}

// ReadStates returns the read receipts of messages in one chat. A message lark-connector has no receipt for (still
// sending, unknown, rejected by Feishu) is simply missing from the result. At most MaxReadStates ids.
func (c *Client) ReadStates(ctx context.Context, feedID string, messageIDs []string) ([]ReadState, error) {
	if len(messageIDs) > MaxReadStates {
		return nil, fmt.Errorf("lark: at most %d messages per read-states call", MaxReadStates)
	}
	out := []ReadState{}
	if len(messageIDs) == 0 {
		return out, nil
	}
	pairs := make([]map[string]string, len(messageIDs))
	for i, id := range messageIDs {
		pairs[i] = map[string]string{"feedId": feedID, "messageId": id}
	}
	var res struct {
		Items []struct {
			MessageID   string `json:"messageId"`
			ReadCount   int    `json:"readCount"`
			UnreadCount int    `json:"unreadCount"`
		} `json:"items"`
	}
	if err := c.do(ctx, "POST", "/messages/read-states", nil, map[string]any{"pairs": pairs}, &res); err != nil {
		return nil, err
	}
	for _, it := range res.Items {
		if it.MessageID != "" {
			out = append(out, ReadState{MessageID: it.MessageID, ReadCount: it.ReadCount, UnreadCount: it.UnreadCount})
		}
	}
	return out, nil
}

// Member is one person in a chat, as the @ picker shows them: the group nickname if they have one, else their name.
type Member struct {
	UserID string `json:"user_id"`
	Name   string `json:"name"`
	Avatar string `json:"avatar,omitempty"`
}

// Members lists a chat's members, or with q the members matching it (name, nickname, pinyin). limit defaults to 50
// and is capped at 200.
func (c *Client) Members(ctx context.Context, feedID, q string, limit int) ([]Member, error) {
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, 200)
	path, query := "/conversations/"+url.PathEscape(feedID)+"/members", url.Values{"limit": {strconv.Itoa(limit)}}
	if q = strings.TrimSpace(q); q != "" {
		path += "/search"
		query.Set("q", q)
	}
	var res struct {
		Items []struct {
			UserID        string `json:"userId"`
			DisplayName   string `json:"displayName"`
			GroupNickname string `json:"groupNickname"`
			AvatarURL     string `json:"avatarUrl"`
		} `json:"items"`
	}
	if err := c.do(ctx, "GET", path, query, nil, &res); err != nil {
		return nil, err
	}
	out := []Member{}
	for _, it := range res.Items {
		if it.UserID == "" {
			continue
		}
		name := it.GroupNickname
		if name == "" {
			name = it.DisplayName
		}
		out = append(out, Member{UserID: it.UserID, Name: name, Avatar: MediaURL(it.AvatarURL)})
	}
	return out, nil
}
