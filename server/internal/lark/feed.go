package lark

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Conversation is a chat or group as the app lists it (lark-connector's feed item, flattened).
type Conversation struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Avatar        string    `json:"avatar,omitempty"`
	IsGroup       bool      `json:"is_group"`
	IsTopic       bool      `json:"is_topic,omitempty"` // topic group: the chat is a list of threads
	Unread        int       `json:"unread"`
	MarkedUnread  bool      `json:"marked_unread,omitempty"`
	AtMe          bool      `json:"at_me"`
	AtAll         bool      `json:"at_all,omitempty"`
	Muted         bool      `json:"muted"`
	Pinned        bool      `json:"pinned,omitempty"`
	Flagged       bool      `json:"flagged,omitempty"`
	Done          bool      `json:"done,omitempty"` // set from the scope the item was listed in
	Tags          []string  `json:"tags,omitempty"` // 机器人 / AI / 外部 / 公开 / 会议…
	Preview       string    `json:"preview"`
	PreviewSender string    `json:"preview_sender,omitempty"` // group chats: who wrote the last message
	IsMine        bool      `json:"is_mine"`
	UpdatedAt     time.Time `json:"updated_at"`
	ReadPosition  string    `json:"read_position,omitempty"`
	MentionAt     string    `json:"mention_position,omitempty"` // the unread @me / @all message
	pinRank       string
}

type rawConversation struct {
	FeedID         string   `json:"feedId"`
	Name           string   `json:"name"`
	AvatarURL      string   `json:"avatarUrl"`
	IsGroup        bool     `json:"isGroup"`
	ChatMode       string   `json:"chatMode"`
	UnreadCount    int      `json:"unreadCount"`
	IsMarkedUnread bool     `json:"isMarkedUnread"`
	HasUnreadAtMe  bool     `json:"hasUnreadAtMe"`
	HasUnreadAtAll bool     `json:"hasUnreadAtAll"`
	IsMuted        bool     `json:"isMuted"`
	IsMutedAtAll   bool     `json:"isMutedAtAll"`
	IsHidden       bool     `json:"isHidden"`
	IsFlagged      bool     `json:"isFlagged"`
	IsBot          bool     `json:"isBot"`
	IsAgent        bool     `json:"isAgent"`
	GroupTags      []string `json:"groupTags"`
	OnTopRankAt    string   `json:"onTopRankAt"`
	ReadPosition   string   `json:"readPosition"`
	AutoMute       *struct {
		Reason string `json:"reason"`
	} `json:"autoMute"`
	UnreadMention *struct {
		Position string `json:"position"`
	} `json:"unreadMention"`
	UpdatedAtMs any `json:"updatedAtMs"`
	Preview     *struct {
		DisplayAt string `json:"displayAt"`
		IsMine    bool   `json:"isMine"`
		Sender    *struct {
			DisplayName string `json:"displayName"`
			Alias       string `json:"alias"`
		} `json:"sender"`
		Digest struct {
			Elements []struct {
				Content  string `json:"content"`
				Kind     string `json:"kind"`
				EmojiKey string `json:"emojiKey"`
			} `json:"elements"`
		} `json:"digest"`
	} `json:"preview"`
}

func (r rawConversation) normalize() Conversation {
	c := Conversation{ID: r.FeedID, Name: r.Name, Avatar: MediaURL(r.AvatarURL), IsGroup: r.IsGroup, IsTopic: r.ChatMode == "topic",
		Unread: r.UnreadCount, MarkedUnread: r.IsMarkedUnread, AtMe: r.HasUnreadAtMe, AtAll: r.HasUnreadAtAll && !r.IsMutedAtAll,
		Muted: r.IsMuted || (r.AutoMute != nil && r.AutoMute.Reason == "large_chat"), Pinned: r.OnTopRankAt != "", Flagged: r.IsFlagged,
		ReadPosition: r.ReadPosition, pinRank: r.OnTopRankAt}
	// Bots get an identity tag before the group's own tags, as lark-connector's list does.
	if !r.IsGroup && r.IsAgent {
		c.Tags = append(c.Tags, "AI")
	} else if !r.IsGroup && r.IsBot {
		c.Tags = append(c.Tags, "机器人")
	}
	c.Tags = append(c.Tags, r.GroupTags...)
	if r.UnreadMention != nil && r.UnreadCount > 0 {
		c.MentionAt = r.UnreadMention.Position
	}
	if r.Preview != nil {
		var b strings.Builder
		for _, e := range r.Preview.Digest.Elements {
			if e.Kind == "emoji" && e.Content == "" {
				b.WriteString("[" + e.EmojiKey + "]")
				continue
			}
			b.WriteString(e.Content)
		}
		c.Preview = strings.Join(strings.Fields(b.String()), " ")
		c.IsMine = r.Preview.IsMine
		if r.IsGroup && r.Preview.Sender != nil {
			c.PreviewSender = r.Preview.Sender.Alias
			if c.PreviewSender == "" {
				c.PreviewSender = r.Preview.Sender.DisplayName
			}
		}
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

// Scopes the app may list (lark-connector silently turns anything else into inbox).
var scopes = map[string]bool{"chats": true, "inbox": true, "unread": true, "done": true, "flagged": true, "mentions": true,
	"muted": true, "p2p": true, "group": true, "thread": true, "later": true}

// Conversations returns one page of a feed scope in lark-connector's order (pinned first): "chats" is Feishu's
// 消息 list (no done or folded chats, label display rules applied), "done" is 已完成, "label:<id>" one label.
func (c *Client) Conversations(ctx context.Context, scope, cursor string, limit int) ([]Conversation, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if scope == "" {
		scope = "chats"
	}
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	if id, ok := strings.CutPrefix(scope, "label:"); ok && id != "" {
		q.Set("scope", "group:"+id)
	} else if scopes[scope] {
		q.Set("scope", scope)
	} else {
		return nil, "", errors.New("lark: unknown scope " + scope)
	}
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
		conv := r.normalize()
		conv.Done = scope == "done" // the item's own isDone is not reliable
		out = append(out, conv)
	}
	next := ""
	if res.HasMore {
		next = res.NextCursor
	}
	return out, next, nil
}

// Pinned returns every pinned chat (置顶), highest rank first. lark-connector has no pinned list: pinned chats sort
// first in each scope, and they live in both the inbox and done, so it reads the head of both.
func (c *Client) Pinned(ctx context.Context) ([]Conversation, error) {
	var out []Conversation
	seen := map[string]bool{}
	for _, scope := range []string{"inbox", "done"} {
		cursor := ""
		for page := 0; page < 3; page++ {
			items, next, err := c.Conversations(ctx, scope, cursor, 100)
			if err != nil {
				return nil, err
			}
			more := next != ""
			for _, it := range items {
				if !it.Pinned {
					more = false
					break
				}
				if !seen[it.ID] {
					seen[it.ID] = true
					out = append(out, it)
				}
			}
			if !more {
				break
			}
			cursor = next
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].pinRank > out[j].pinRank })
	return out, nil
}

// Label is one of the user's feed labels (标签).
type Label struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func (c *Client) Labels(ctx context.Context) ([]Label, error) {
	var res struct {
		Items []struct {
			ID                string `json:"id"`
			Name              string `json:"name"`
			ConversationCount int    `json:"conversationCount"`
		} `json:"items"`
	}
	if err := c.do(ctx, "GET", "/conversation-labels", nil, nil, &res); err != nil {
		return nil, err
	}
	out := make([]Label, 0, len(res.Items))
	for _, l := range res.Items {
		out = append(out, Label{ID: l.ID, Name: l.Name, Count: l.ConversationCount})
	}
	return out, nil
}

// UnreadTotal is the 未读 count Feishu shows (muted chats excluded).
func (c *Client) UnreadTotal(ctx context.Context) (int, error) {
	var res struct {
		Count int `json:"count"`
	}
	err := c.do(ctx, "GET", "/conversations/total-unread-count", url.Values{"includeMuted": {"false"}}, nil, &res)
	return res.Count, err
}

// FlaggedMessage is one flagged message in 标记, with the chat it is in.
type FlaggedMessage struct {
	FeedID     string    `json:"feed_id"`
	ChatName   string    `json:"chat_name"`
	ChatAvatar string    `json:"chat_avatar,omitempty"`
	FlaggedAt  time.Time `json:"flagged_at"`
	Message    Message   `json:"message"`
}

// Flags returns 标记: flagged chats and flagged messages, newest flag first.
func (c *Client) Flags(ctx context.Context) ([]Conversation, []FlaggedMessage, error) {
	var res struct {
		Conversations []struct {
			Conversation rawConversation `json:"conversation"`
			FlaggedAt    time.Time       `json:"flaggedAt"`
		} `json:"conversations"`
		Messages []struct {
			FeedID                string     `json:"feedId"`
			ConversationName      string     `json:"conversationName"`
			ConversationAvatarURL string     `json:"conversationAvatarUrl"`
			FlaggedAt             time.Time  `json:"flaggedAt"`
			Message               rawMessage `json:"message"`
		} `json:"messages"`
	}
	if err := c.do(ctx, "GET", "/flags", nil, nil, &res); err != nil {
		return nil, nil, err
	}
	sort.SliceStable(res.Conversations, func(i, j int) bool { return res.Conversations[i].FlaggedAt.After(res.Conversations[j].FlaggedAt) })
	sort.SliceStable(res.Messages, func(i, j int) bool { return res.Messages[i].FlaggedAt.After(res.Messages[j].FlaggedAt) })
	convs := make([]Conversation, 0, len(res.Conversations))
	for _, f := range res.Conversations {
		conv := f.Conversation.normalize()
		conv.Flagged = true
		convs = append(convs, conv)
	}
	msgs := make([]FlaggedMessage, 0, len(res.Messages))
	for _, f := range res.Messages {
		msgs = append(msgs, FlaggedMessage{FeedID: f.FeedID, ChatName: f.ConversationName, ChatAvatar: MediaURL(f.ConversationAvatarURL),
			FlaggedAt: f.FlaggedAt, Message: f.Message.normalize()})
	}
	return convs, msgs, nil
}

// Settings changes one chat as the user: done (已完成), pinned (置顶), flagged (标记), muted (免打扰), labels.
type Settings struct {
	Done     *bool     `json:"done,omitempty"`
	Pinned   *bool     `json:"pinned,omitempty"`
	Flagged  *bool     `json:"flagged,omitempty"`
	Muted    *bool     `json:"muted,omitempty"`
	LabelIDs *[]string `json:"label_ids,omitempty"`
}

func (c *Client) UpdateSettings(ctx context.Context, feedID string, s Settings) error {
	body := map[string]any{}
	if s.Done != nil {
		body["isArchived"] = *s.Done
	}
	if s.Pinned != nil {
		body["isShortcut"] = *s.Pinned
	}
	if s.Flagged != nil {
		body["isFlagged"] = *s.Flagged
	}
	if s.Muted != nil {
		body["isMuted"] = *s.Muted
	}
	if s.LabelIDs != nil {
		body["labelIds"] = *s.LabelIDs
	}
	if len(body) == 0 {
		return errors.New("lark: nothing to change")
	}
	return c.do(ctx, "PATCH", "/conversations/"+url.PathEscape(feedID)+"/settings", nil, body, nil)
}

// ChatLabels returns the labels a chat is in and every label it could be put in.
func (c *Client) ChatLabels(ctx context.Context, feedID string) ([]string, []Label, error) {
	var res struct {
		LabelIDs     []string `json:"labelIds"`
		LabelOptions []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"labelOptions"`
	}
	if err := c.do(ctx, "GET", "/conversations/"+url.PathEscape(feedID)+"/settings", nil, nil, &res); err != nil {
		return nil, nil, err
	}
	opts := make([]Label, 0, len(res.LabelOptions))
	for _, o := range res.LabelOptions {
		opts = append(opts, Label{ID: o.ID, Name: o.Name})
	}
	return res.LabelIDs, opts, nil
}

// SetUnread marks a chat unread (标为未读) or clears its unread state.
func (c *Client) SetUnread(ctx context.Context, feedID string, unread bool) error {
	return c.do(ctx, "PUT", "/conversations/"+url.PathEscape(feedID)+"/unread", nil, map[string]any{"unread": unread}, nil)
}
